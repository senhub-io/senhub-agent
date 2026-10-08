// Package configuration handles configuration migration between versions
package configuration

import (
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
	"senhub-agent.go/internal/agent/cliArgs"
	"senhub-agent.go/internal/agent/services/logger"
)

// ConfigVersion tracks configuration format version (for backups only)
type ConfigVersion struct {
	Version  int    `yaml:"config_version"`
	Migrated string `yaml:"migrated_at,omitempty"`
	AgentVer string `yaml:"agent_version,omitempty"`
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

// MigrateIfNeeded checks if configuration needs migration and performs it automatically.
//
// The v1 to v2 step is a node-level edit run through rewriteFile: the file is
// parsed into a yaml.Node tree, `type` keys and `config_version` are added to
// it, and the tree is written back, so the operator's comments, key order and
// formatting survive. If the operator saves the file while the migration is
// being computed, the edit is redone on their version; after bounded retries
// nothing is written and the migration is retried at the next start.
func (cm *ConfigMigrator) MigrateIfNeeded() error {
	cm.logger.Debug().Str("config_path", cm.configPath).Msg("Checking if migration is needed")

	if _, err := os.Stat(cm.configPath); os.IsNotExist(err) {
		cm.logger.Debug().Msg("Configuration file does not exist, no migration needed")
		return nil
	}

	migrated, err := rewriteFile(cm.configPath, cm.migrateV1ToV2)
	if err != nil {
		if errors.Is(err, ErrConfigChangedConcurrently) {
			cm.logger.Warn().Err(err).Msg("Configuration kept changing during migration; nothing was written, it is retried at the next start")
		}
		return fmt.Errorf("failed to migrate configuration: %w", err)
	}
	if migrated {
		cm.logger.Info().Str("config_path", cm.configPath).Msg("Configuration migrated from version 1 to 2")
	}
	return nil
}

// migrateV1ToV2 is the edit handed to rewriteFile. It returns nil when the
// file needs no migration. It may run several times and derives everything
// from the bytes it is given.
func (cm *ConfigMigrator) migrateV1ToV2(data []byte) ([]byte, error) {
	var doc yaml.Node
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return nil, fmt.Errorf("failed to parse config YAML: %w", err)
	}
	if len(doc.Content) == 0 || doc.Content[0].Kind != yaml.MappingNode {
		return nil, nil
	}
	root := doc.Content[0]

	probes := mapValue(root, "probes")
	if probes == nil || probes.Kind != yaml.SequenceNode || len(probes.Content) == 0 {
		cm.logger.Debug().Msg("No probes to migrate")
		return nil, nil
	}
	first := probes.Content[0]
	if first.Kind != yaml.MappingNode {
		cm.logger.Warn().Msg("Invalid probe format")
		return nil, nil
	}
	if mapValue(first, "type") != nil {
		// Version 2 already. Param renames are deliberately not migrated
		// (see migrateParamRenamesInPlace).
		return nil, nil
	}

	if err := cm.createBackup(data); err != nil {
		return nil, fmt.Errorf("failed to create backup: %w", err)
	}

	for i, probe := range probes.Content {
		if probe.Kind != yaml.MappingNode {
			cm.logger.Warn().Int("index", i).Msg("Skipping invalid probe")
			continue
		}
		if mapValue(probe, "type") != nil {
			continue
		}
		addTypeAfterName(probe, cm.logger, i)
	}

	// This is specifically the v1 to v2 migration, so it stamps 2, not
	// CurrentConfigVersion (the v2 to v3 secret seal is a separate pass that
	// stamps 3 only when it actually seals a secret). A newer stamp is never
	// lowered.
	const migratedToVersion = 2
	if cv := mapValue(root, "config_version"); cv != nil {
		if existing, err := strconv.Atoi(strings.TrimSpace(cv.Value)); err != nil || existing < migratedToVersion {
			cv.Kind, cv.Tag, cv.Style, cv.Value = yaml.ScalarNode, "!!int", 0, strconv.Itoa(migratedToVersion)
		}
	} else {
		root.Content = append(root.Content,
			&yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: "config_version"},
			&yaml.Node{Kind: yaml.ScalarNode, Tag: "!!int", Value: strconv.Itoa(migratedToVersion)},
		)
	}

	body, err := marshalNode(&doc)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal migrated config: %w", err)
	}
	return append([]byte(migrationHeader(migratedToVersion)), body...), nil
}

// addTypeAfterName inserts `type: <name>` right after the probe's name key,
// keeping the name, type, params order of the v2 format.
func addTypeAfterName(probe *yaml.Node, log *logger.ModuleLogger, index int) {
	for i := 0; i+1 < len(probe.Content); i += 2 {
		if probe.Content[i].Value != "name" {
			continue
		}
		nameNode := probe.Content[i+1]
		if nameNode.Kind != yaml.ScalarNode {
			log.Warn().Int("index", index).Msg("Probe name is not a string")
			return
		}
		typeKey := &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: "type"}
		typeVal := &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: nameNode.Value, Style: nameNode.Style}
		content := append([]*yaml.Node{}, probe.Content[:i+2]...)
		content = append(content, typeKey, typeVal)
		probe.Content = append(content, probe.Content[i+2:]...)
		log.Debug().Int("index", index).Str("name", nameNode.Value).Msg("Migrated probe: added 'type' field")
		return
	}
	log.Warn().Int("index", index).Msg("Probe missing name field")
}

func migrationHeader(version int) string {
	agentVersion := cliArgs.Version
	if agentVersion == "" {
		agentVersion = "unknown"
	}
	return fmt.Sprintf(`# Configuration automatically migrated to version %d format on %s
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

`, version, time.Now().Format("2006-01-02 15:04:05 MST"), agentVersion)
}

// createBackup writes a timestamped backup of the bytes about to be
// migrated.
func (cm *ConfigMigrator) createBackup(data []byte) error {
	timestamp := time.Now().Format("20060102-150405")
	backupPath := fmt.Sprintf("%s.backup.%s", cm.configPath, timestamp)

	agentVersion := cliArgs.Version
	if agentVersion == "" {
		agentVersion = "unknown"
	}
	backupHeader := fmt.Sprintf(`# Configuration backup created: %s
# Original agent version: %s
# This backup was created before automatic migration to version 2 format

`, time.Now().Format("2006-01-02 15:04:05 MST"), agentVersion)

	if err := os.WriteFile(backupPath, append([]byte(backupHeader), data...), 0600); err != nil {
		return fmt.Errorf("failed to write backup: %w", err)
	}
	cm.logger.Info().Str("backup_path", backupPath).Msg("Backup created successfully")
	return nil
}

// migrateParamRenamesInPlace is a no-op.
// Auto-rename of base_url→director_url was removed because text replacement
// cannot distinguish between probe types (would break netscaler's base_url).
// The old format (base_url/director_url) is handled at runtime with backward compat.
func (cm *ConfigMigrator) migrateParamRenamesInPlace() error {
	return nil
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
		// yaml.v3 returns map[string]interface{} when unmarshaling into map[string]interface{}
		probe, ok := probeRaw.(map[string]interface{})
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
