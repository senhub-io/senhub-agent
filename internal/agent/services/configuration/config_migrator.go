// Package configuration handles configuration migration between versions
package configuration

import (
	"fmt"
	"os"
	"strings"
	"time"

	"gopkg.in/yaml.v2"
	"senhub-agent.go/internal/agent/cliArgs"
	"senhub-agent.go/internal/agent/services/logger"
)

// Current configuration version
const CurrentConfigVersion = 2

// ConfigVersion tracks configuration format version
type ConfigVersion struct {
	Version   int    `yaml:"config_version"`
	Migrated  string `yaml:"migrated_at,omitempty"`
	AgentVer  string `yaml:"agent_version,omitempty"`
}

// ConfigMigrator handles automatic configuration migration
type ConfigMigrator struct {
	logger     *logger.ModuleLogger
	configPath string
}

// NewConfigMigrator creates a new configuration migrator
func NewConfigMigrator(configPath string, baseLogger *logger.Logger) *ConfigMigrator {
	moduleLogger := logger.NewModuleLogger(baseLogger, "configuration.migrator")

	return &ConfigMigrator{
		logger:     moduleLogger,
		configPath: configPath,
	}
}

// MigrateIfNeeded checks if configuration needs migration and performs it automatically
func (cm *ConfigMigrator) MigrateIfNeeded() error {
	cm.logger.Debug().Str("config_path", cm.configPath).Msg("Checking if migration is needed")

	// Check if file exists
	if _, err := os.Stat(cm.configPath); os.IsNotExist(err) {
		cm.logger.Debug().Msg("Configuration file does not exist, no migration needed")
		return nil
	}

	// Read current configuration
	data, err := os.ReadFile(cm.configPath)
	if err != nil {
		return fmt.Errorf("failed to read config file: %w", err)
	}

	// Parse YAML to check structure
	var rawConfig map[string]interface{}
	if err := yaml.Unmarshal(data, &rawConfig); err != nil {
		return fmt.Errorf("failed to parse config YAML: %w", err)
	}

	// Check if probes section exists
	probesRaw, hasProbes := rawConfig["probes"]
	if !hasProbes {
		cm.logger.Debug().Msg("No probes section found, no migration needed")
		return nil
	}

	probesList, ok := probesRaw.([]interface{})
	if !ok || len(probesList) == 0 {
		cm.logger.Debug().Msg("Probes section empty or invalid, no migration needed")
		return nil
	}

	// Check if first probe has 'type' field (v2 format)
	firstProbe, ok := probesList[0].(map[interface{}]interface{})
	if !ok {
		cm.logger.Warn().Msg("Invalid probe format")
		return nil
	}

	if _, hasType := firstProbe["type"]; hasType {
		cm.logger.Debug().Msg("Configuration already in v2 format (has 'type' field)")
		return nil
	}

	// Migration needed!
	cm.logger.Info().Msg("Configuration needs migration from v1 to v2")

	// Create backup
	if err := cm.createBackup(); err != nil {
		return fmt.Errorf("failed to create backup: %w", err)
	}

	// Perform migration
	if err := cm.migrateV1ToV2(rawConfig); err != nil {
		return fmt.Errorf("failed to migrate configuration: %w", err)
	}

	cm.logger.Info().Msg("Configuration migrated successfully")
	return nil
}

// createBackup creates a timestamped backup of the current configuration
func (cm *ConfigMigrator) createBackup() error {
	timestamp := time.Now().Format("20060102-150405")
	backupPath := fmt.Sprintf("%s.backup.%s", cm.configPath, timestamp)

	cm.logger.Info().
		Str("backup_path", backupPath).
		Msg("Creating configuration backup")

	// Read current config
	data, err := os.ReadFile(cm.configPath)
	if err != nil {
		return fmt.Errorf("failed to read config: %w", err)
	}

	// Get agent version
	agentVersion := cliArgs.Version
	if agentVersion == "" {
		agentVersion = "unknown"
	}

	// Prepend backup header with version info
	backupHeader := fmt.Sprintf(`# Configuration backup created: %s
# Original agent version: %s
# This backup was created before automatic migration to v2 format

`, time.Now().Format("2006-01-02 15:04:05 MST"), agentVersion)

	backupData := append([]byte(backupHeader), data...)

	// Write backup
	if err := os.WriteFile(backupPath, backupData, 0600); err != nil {
		return fmt.Errorf("failed to write backup: %w", err)
	}

	cm.logger.Info().Str("backup_path", backupPath).Msg("Backup created successfully")
	return nil
}

// migrateV1ToV2 migrates configuration from v1 (name only) to v2 (name + type)
func (cm *ConfigMigrator) migrateV1ToV2(config map[string]interface{}) error {
	cm.logger.Info().Msg("Performing v1 → v2 migration")

	// Process probes section
	probesRaw, hasProbes := config["probes"]
	if !hasProbes {
		return fmt.Errorf("no probes section found")
	}

	probesList, ok := probesRaw.([]interface{})
	if !ok {
		return fmt.Errorf("invalid probes format")
	}

	// Migrate each probe
	for i, probeRaw := range probesList {
		probe, ok := probeRaw.(map[interface{}]interface{})
		if !ok {
			cm.logger.Warn().Int("index", i).Msg("Skipping invalid probe")
			continue
		}

		// Get current name
		nameRaw, hasName := probe["name"]
		if !hasName {
			cm.logger.Warn().Int("index", i).Msg("Probe missing name field")
			continue
		}

		name, ok := nameRaw.(string)
		if !ok {
			cm.logger.Warn().Int("index", i).Msg("Probe name is not a string")
			continue
		}

		// Add 'type' field with same value as 'name' (default migration)
		probe["type"] = name

		cm.logger.Debug().
			Int("index", i).
			Str("name", name).
			Str("type", name).
			Msg("Migrated probe: added 'type' field")
	}

	// Generate migrated YAML
	migratedData, err := cm.generateMigratedYAML(config)
	if err != nil {
		return fmt.Errorf("failed to generate migrated YAML: %w", err)
	}

	// Write migrated configuration
	if err := os.WriteFile(cm.configPath, migratedData, 0600); err != nil {
		return fmt.Errorf("failed to write migrated config: %w", err)
	}

	cm.logger.Info().Str("config_path", cm.configPath).Msg("Migrated configuration written")
	return nil
}

// generateMigratedYAML generates YAML with migration header and comments
func (cm *ConfigMigrator) generateMigratedYAML(config map[string]interface{}) ([]byte, error) {
	// Get agent version
	agentVersion := cliArgs.Version
	if agentVersion == "" {
		agentVersion = "unknown"
	}

	// Create migration header
	header := fmt.Sprintf(`# Configuration automatically migrated to v2 format on %s
# Original agent version: %s
# Migration: Added 'type' field to all probes (copied from 'name')
#
# In v2 format:
#   - 'name': Display name (free choice, used for UI identification)
#   - 'type': Probe type (technical identifier: cpu, citrix, redfish, etc.)
#
# Example:
#   - name: My Production Citrix    # Display name (free text)
#     type: citrix                   # Probe type (fixed identifier)
#     params:
#       base_url: "https://director.example.com"

`, time.Now().Format("2006-01-02 15:04:05 MST"), agentVersion)

	// Marshal config to YAML
	yamlData, err := yaml.Marshal(config)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal config: %w", err)
	}

	// Combine header + YAML
	fullData := append([]byte(header), yamlData...)

	return fullData, nil
}

// AddCommentsForNewParameters adds commented examples of new optional parameters
// This helps users discover new features without breaking existing configs
func (cm *ConfigMigrator) AddCommentsForNewParameters(probeType string) []string {
	// Map of probe types to their new optional parameters (examples)
	newParams := map[string][]string{
		"citrix": {
			"# Optional new parameters:",
			"# display_filter: \"\"           # Filter for specific resources",
			"# cache_duration: 300            # Cache duration in seconds",
		},
		"redfish": {
			"# Optional new parameters:",
			"# collectors: [\"system\", \"thermal\"]  # Specific collectors to enable",
			"# timeout: 30                    # Request timeout in seconds",
		},
		// Add more probe types as needed
	}

	if params, exists := newParams[probeType]; exists {
		return params
	}

	return []string{}
}

// ValidateMigratedConfig checks if migrated configuration is valid
func (cm *ConfigMigrator) ValidateMigratedConfig() error {
	cm.logger.Debug().Msg("Validating migrated configuration")

	data, err := os.ReadFile(cm.configPath)
	if err != nil {
		return fmt.Errorf("failed to read config: %w", err)
	}

	var config map[string]interface{}
	if err := yaml.Unmarshal(data, &config); err != nil {
		return fmt.Errorf("invalid YAML after migration: %w", err)
	}

	// Check probes have both 'name' and 'type'
	probesRaw, hasProbes := config["probes"]
	if !hasProbes {
		return nil // No probes is valid
	}

	probesList, ok := probesRaw.([]interface{})
	if !ok {
		return fmt.Errorf("probes section is not a list")
	}

	for i, probeRaw := range probesList {
		probe, ok := probeRaw.(map[interface{}]interface{})
		if !ok {
			return fmt.Errorf("probe %d is not a map", i)
		}

		name, hasName := probe["name"]
		if !hasName {
			return fmt.Errorf("probe %d missing 'name' field", i)
		}

		probeType, hasType := probe["type"]
		if !hasType {
			return fmt.Errorf("probe %d missing 'type' field", i)
		}

		nameStr, nameOk := name.(string)
		typeStr, typeOk := probeType.(string)

		if !nameOk || !typeOk {
			return fmt.Errorf("probe %d has invalid name or type", i)
		}

		if strings.TrimSpace(nameStr) == "" || strings.TrimSpace(typeStr) == "" {
			return fmt.Errorf("probe %d has empty name or type", i)
		}
	}

	cm.logger.Info().Msg("Migrated configuration validated successfully")
	return nil
}
