// Package configuration handles configuration versioning and compatibility
package configuration

import (
	"fmt"

	"senhub-agent.go/internal/agent/cliArgs"
)

// Configuration Version History
// =============================
// 1: Initial format (name only for probes)
//    - Probes have only 'name' field (used for both display and type)
//    - No explicit version field
//    - Agent versions: 0.1.0 - 0.1.63
//
// 2: Name/Type separation
//    - Probes have 'name' (display) and 'type' (technical ID)
//    - Explicit config_version field
//    - Automatic migration from version 1
//    - Agent versions: 0.1.65+
//
// 3: Secret references (current)
//    - Inline plaintext secrets are sealed into the OS-native store and
//      replaced by ${secret:<instance>.<field>} references
//    - The bump happens only on a config that actually had a secret sealed;
//      a secret-free v2 config stays v2 and loads unchanged
//    - An older agent (max v2) refuses a v3 config rather than passing an
//      unresolved ${secret:} literal to a probe

// Current configuration version that this agent expects
const CurrentConfigVersion = 3

// Minimum configuration version that this agent can work with
// (older versions will trigger automatic migration)
const MinimumConfigVersion = 1

// ConfigVersionInfo describes a configuration version
type ConfigVersionInfo struct {
	Version          int
	Name             string
	MinAgentVersion  string
	MaxAgentVersion  string
	Description      string
	MigrationFromPrv bool // Can migrate from previous version
}

// GetConfigVersionHistory returns the full version history
func GetConfigVersionHistory() []ConfigVersionInfo {
	return []ConfigVersionInfo{
		{
			Version:          1,
			Name:             "Initial Format",
			MinAgentVersion:  "0.1.0",
			MaxAgentVersion:  "0.1.63",
			Description:      "Probes use 'name' field for both display and type identification",
			MigrationFromPrv: false,
		},
		{
			Version:          2,
			Name:             "Name/Type Separation",
			MinAgentVersion:  "0.1.65",
			MaxAgentVersion:  "",
			Description:      "Probes have separate 'name' (display) and 'type' (technical ID) fields",
			MigrationFromPrv: true,
		},
		{
			Version:          3,
			Name:             "Secret References",
			MinAgentVersion:  "0.5.0",
			MaxAgentVersion:  "", // Current version, no max
			Description:      "Inline secrets sealed into the OS-native store, referenced as ${secret:...}",
			MigrationFromPrv: true,
		},
	}
}

// GetCurrentVersionInfo returns info about the current config version
func GetCurrentVersionInfo() ConfigVersionInfo {
	history := GetConfigVersionHistory()
	for _, info := range history {
		if info.Version == CurrentConfigVersion {
			return info
		}
	}
	// Fallback (should never happen)
	return ConfigVersionInfo{
		Version:     CurrentConfigVersion,
		Name:        "Unknown",
		Description: "Current configuration version",
	}
}

// ValidateConfigVersion checks if a config version is compatible with this agent
func ValidateConfigVersion(configVersion int) error {
	if configVersion < MinimumConfigVersion {
		return fmt.Errorf("configuration version %d is too old (minimum: %d)", configVersion, MinimumConfigVersion)
	}

	if configVersion > CurrentConfigVersion {
		return fmt.Errorf("configuration version %d is too new for this agent (current: %d, agent: %s)",
			configVersion, CurrentConfigVersion, cliArgs.Version)
	}

	return nil
}

// NeedsMigration returns true if the config version needs migration
func NeedsMigration(configVersion int) bool {
	return configVersion < CurrentConfigVersion
}

// GetMigrationPath returns the list of versions to migrate through
func GetMigrationPath(fromVersion int) []int {
	if fromVersion >= CurrentConfigVersion {
		return []int{}
	}

	path := []int{}
	for v := fromVersion + 1; v <= CurrentConfigVersion; v++ {
		path = append(path, v)
	}
	return path
}

// GetVersionDescription returns a human-readable description of a version
func GetVersionDescription(version int) string {
	history := GetConfigVersionHistory()
	for _, info := range history {
		if info.Version == version {
			return fmt.Sprintf("%d: %s - %s", info.Version, info.Name, info.Description)
		}
	}
	return fmt.Sprintf("%d: Unknown version", version)
}

// ConfigCompatibilityReport provides compatibility information
type ConfigCompatibilityReport struct {
	ConfigVersion  int
	AgentVersion   string
	CurrentVersion int
	Compatible     bool
	NeedsMigration bool
	MigrationPath  []int
	Warnings       []string
	Errors         []string
}

// CheckCompatibility performs a full compatibility check
func CheckCompatibility(configVersion int) ConfigCompatibilityReport {
	report := ConfigCompatibilityReport{
		ConfigVersion:  configVersion,
		AgentVersion:   cliArgs.Version,
		CurrentVersion: CurrentConfigVersion,
		MigrationPath:  []int{},
		Warnings:       []string{},
		Errors:         []string{},
	}

	// Check basic compatibility
	if err := ValidateConfigVersion(configVersion); err != nil {
		report.Compatible = false
		report.Errors = append(report.Errors, err.Error())

		// Too new config
		if configVersion > CurrentConfigVersion {
			report.Errors = append(report.Errors,
				fmt.Sprintf("Please update the agent to a newer version that supports config version %d", configVersion))
		}

		return report
	}

	// Compatible
	report.Compatible = true

	// Check if migration needed
	if NeedsMigration(configVersion) {
		report.NeedsMigration = true
		report.MigrationPath = GetMigrationPath(configVersion)
		report.Warnings = append(report.Warnings,
			fmt.Sprintf("Configuration will be automatically migrated from version %d to %d", configVersion, CurrentConfigVersion))
	}

	return report
}

// FormatCompatibilityReport formats a compatibility report for display
func FormatCompatibilityReport(report ConfigCompatibilityReport) string {
	msg := "Configuration Compatibility Check\n"
	msg += "=====================================\n"
	msg += fmt.Sprintf("Config Version: %d\n", report.ConfigVersion)
	msg += fmt.Sprintf("Agent Version: %s\n", report.AgentVersion)
	msg += fmt.Sprintf("Expected Config Version: %d\n", report.CurrentVersion)
	msg += "\n"

	if !report.Compatible {
		msg += "❌ INCOMPATIBLE\n\n"
		msg += "Errors:\n"
		for _, err := range report.Errors {
			msg += fmt.Sprintf("  - %s\n", err)
		}
	} else if report.NeedsMigration {
		msg += "MIGRATION REQUIRED\n\n"
		msg += fmt.Sprintf("Migration Path: %d", report.ConfigVersion)
		for _, v := range report.MigrationPath {
			msg += fmt.Sprintf(" → %d", v)
		}
		msg += "\n\n"
		msg += "Warnings:\n"
		for _, warn := range report.Warnings {
			msg += fmt.Sprintf("  - %s\n", warn)
		}
	} else {
		msg += "COMPATIBLE\n"
	}

	return msg
}
