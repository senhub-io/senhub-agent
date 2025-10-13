// Package configuration handles configuration migration between versions
package configuration

import (
	"fmt"
	"os"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
	"senhub-agent.go/internal/agent/cliArgs"
	"senhub-agent.go/internal/agent/services/logger"
)

// ConfigVersion tracks configuration format version (for backups only)
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
		cm.logger.Debug().Msg("Configuration already in version 2 format (has 'type' field)")
		return nil
	}

	// Migration needed!
	cm.logger.Info().Msg("Configuration needs migration from version 1 to 2")

	// Create backup
	if err := cm.createBackup(); err != nil {
		return fmt.Errorf("failed to create backup: %w", err)
	}

	// Perform migration
	if err := cm.migrateFrom1To2(rawConfig); err != nil {
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
# This backup was created before automatic migration to version 2 format

`, time.Now().Format("2006-01-02 15:04:05 MST"), agentVersion)

	backupData := append([]byte(backupHeader), data...)

	// Write backup
	if err := os.WriteFile(backupPath, backupData, 0600); err != nil {
		return fmt.Errorf("failed to write backup: %w", err)
	}

	cm.logger.Info().Str("backup_path", backupPath).Msg("Backup created successfully")
	return nil
}

// migrateFrom1To2 migrates configuration from version 1 (name only) to version 2 (name + type)
func (cm *ConfigMigrator) migrateFrom1To2(config map[string]interface{}) error {
	cm.logger.Info().Msg("Performing version 1 → 2 migration")

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
// Uses yaml.v3 to preserve field order: name → type → params
func (cm *ConfigMigrator) generateMigratedYAML(config map[string]interface{}) ([]byte, error) {
	// Get agent version
	agentVersion := cliArgs.Version
	if agentVersion == "" {
		agentVersion = "unknown"
	}

	// Add config_version field
	config["config_version"] = CurrentConfigVersion

	// Create migration header
	header := fmt.Sprintf(`# Configuration automatically migrated to version %d format on %s
# Original agent version: %s
# Migration: Added 'type' field to all probes (copied from 'name')
#            Added 'config_version' field for version tracking
#
# In version 2 format:
#   - 'name': Display name (free choice, used for UI identification)
#   - 'type': Probe type (technical identifier: cpu, citrix, redfish, etc.)
#
# Example:
#   - name: My Production Citrix    # Display name (free text)
#     type: citrix                   # Probe type (fixed identifier)
#     params:
#       base_url: "https://director.example.com"

`, CurrentConfigVersion, time.Now().Format("2006-01-02 15:04:05 MST"), agentVersion)

	// Marshal config to YAML using v3 to get proper node structure
	var rootNode yaml.Node
	if err := rootNode.Encode(config); err != nil {
		return nil, fmt.Errorf("failed to encode config to yaml.v3: %w", err)
	}

	// Reorder probe fields to: name, type, params, ...
	if err := cm.reorderProbeFields(&rootNode); err != nil {
		return nil, fmt.Errorf("failed to reorder probe fields: %w", err)
	}

	// Marshal with yaml.v3 (preserves order)
	yamlData, err := yaml.Marshal(&rootNode)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal config with yaml.v3: %w", err)
	}

	// Combine header + YAML
	fullData := append([]byte(header), yamlData...)

	return fullData, nil
}

// reorderProbeFields reorders probe fields to: name, type, params, ...
func (cm *ConfigMigrator) reorderProbeFields(node *yaml.Node) error {
	// Navigate to document root
	if node.Kind != yaml.DocumentNode || len(node.Content) == 0 {
		return nil
	}

	rootMap := node.Content[0]
	if rootMap.Kind != yaml.MappingNode {
		return nil
	}

	// Find "probes" key in root map
	for i := 0; i < len(rootMap.Content); i += 2 {
		keyNode := rootMap.Content[i]
		valueNode := rootMap.Content[i+1]

		if keyNode.Value == "probes" && valueNode.Kind == yaml.SequenceNode {
			// Found probes array, process each probe
			for _, probeNode := range valueNode.Content {
				if probeNode.Kind == yaml.MappingNode {
					cm.reorderProbeMap(probeNode)
				}
			}
			break
		}
	}

	return nil
}

// reorderProbeMap reorders a single probe map to: name, type, params, ...
func (cm *ConfigMigrator) reorderProbeMap(probeNode *yaml.Node) {
	if probeNode.Kind != yaml.MappingNode {
		return
	}

	// Extract all key-value pairs
	type kvPair struct {
		key   *yaml.Node
		value *yaml.Node
	}
	var pairs []kvPair
	nameIdx := -1
	typeIdx := -1
	paramsIdx := -1

	for i := 0; i < len(probeNode.Content); i += 2 {
		key := probeNode.Content[i]
		value := probeNode.Content[i+1]

		pairs = append(pairs, kvPair{key: key, value: value})

		switch key.Value {
		case "name":
			nameIdx = len(pairs) - 1
		case "type":
			typeIdx = len(pairs) - 1
		case "params":
			paramsIdx = len(pairs) - 1
		}
	}

	// If no reordering needed, return
	if nameIdx == -1 || typeIdx == -1 {
		return
	}

	// Build new ordered pairs: name, type, params, then rest
	var orderedPairs []kvPair

	// 1. name first
	orderedPairs = append(orderedPairs, pairs[nameIdx])

	// 2. type second
	orderedPairs = append(orderedPairs, pairs[typeIdx])

	// 3. params third (if exists)
	if paramsIdx != -1 {
		orderedPairs = append(orderedPairs, pairs[paramsIdx])
	}

	// 4. Add remaining fields
	for i, pair := range pairs {
		if i != nameIdx && i != typeIdx && i != paramsIdx {
			orderedPairs = append(orderedPairs, pair)
		}
	}

	// Rebuild Content array with ordered pairs
	newContent := make([]*yaml.Node, 0, len(orderedPairs)*2)
	for _, pair := range orderedPairs {
		newContent = append(newContent, pair.key, pair.value)
	}

	probeNode.Content = newContent
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
