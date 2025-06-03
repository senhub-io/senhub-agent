// senhub-agent/internal/agent/services/data_store/http_utils.go
package data_store

import (
	"fmt"
	"net/http"
	"strings"
	"time"

	"senhub-agent.go/internal/agent/cliArgs"
	"senhub-agent.go/internal/agent/services/logger"
)

// UtilsManager handles utility functions and helper methods
type UtilsManager struct {
	logger   *logger.ModuleLogger
	strategy *HTTPSyncStrategy // Reference to parent strategy for access to other modules
}

// NewUtilsManager creates a new utilities manager
func NewUtilsManager(strategy *HTTPSyncStrategy, logger *logger.ModuleLogger) *UtilsManager {
	return &UtilsManager{
		logger:   logger,
		strategy: strategy,
	}
}

// Utility Functions for HTTP Strategy

// getTagDescription provides human-readable descriptions for common tags
func (u *UtilsManager) getTagDescription(tagKey string) string {
	descriptions := map[string]string{
		"core":       "CPU core identifier",
		"instance":   "CPU instance identifier (Windows)",
		"interface":  "Network interface name",
		"adapter":    "Network adapter name (Windows)",
		"device":     "Device identifier",
		"drive":      "Drive identifier",
		"controller": "Controller identifier",
		"slot":       "Physical slot number",
		"channel":    "Channel number",
		"host":       "Hostname",
		"os":         "Operating system",
		"platform":   "Platform identifier",
		"probe_name": "Source probe name",
	}
	
	if desc, exists := descriptions[tagKey]; exists {
		return desc
	}
	return "No description available"
}

// Version and Build Info

// VersionInfo holds parsed version and commit information
type VersionInfo struct {
	Version string
	Commit  string
}

// parseVersionInfo parses version and commit information from cliArgs
func (u *UtilsManager) parseVersionInfo() VersionInfo {
	version := cliArgs.Version
	commit := ""
	
	// If we have a commit hash from git describe, parse it
	if cliArgs.CommitHash != "" {
		fullVersion := cliArgs.CommitHash
		
		// Try to extract version and commit info
		if fullVersion != "" {
			// If it's just a tag (no commit info), use it as version
			if !strings.Contains(fullVersion, "-g") {
				version = fullVersion
			} else {
				// Parse format: "version-commits-ghash-dirty"
				parts := strings.Split(fullVersion, "-")
				if len(parts) >= 3 {
					// Find the version part (everything before the commit count)
					for i, part := range parts {
						if strings.HasPrefix(part, "g") && i > 0 {
							// This is the git hash, version is everything before previous part
							version = strings.Join(parts[:i-1], "-")
							commit = strings.Join(parts[i-1:], "-")
							break
						}
					}
				}
			}
		}
	}
	
	// Fallback: if version is empty, use commit hash
	if version == "" && cliArgs.CommitHash != "" {
		version = cliArgs.CommitHash
	}
	
	// Fallback: if still empty, use default
	if version == "" {
		version = "development"
	}
	
	return VersionInfo{
		Version: version,
		Commit:  commit,
	}
}

// formatDuration formats a duration in a human-readable format
func (u *UtilsManager) formatDuration(d time.Duration) string {
	days := int(d.Hours()) / 24
	hours := int(d.Hours()) % 24
	minutes := int(d.Minutes()) % 60
	
	if days > 0 {
		return fmt.Sprintf("%dd %dh %dm", days, hours, minutes)
	} else if hours > 0 {
		return fmt.Sprintf("%dh %dm", hours, minutes)
	} else {
		return fmt.Sprintf("%dm", minutes)
	}
}

// CPU Measurement for system monitoring

// getCPUUsage calculates CPU usage percentage for the current process
func (u *UtilsManager) getCPUUsage() float64 {
	// For now, return 0.0 as placeholder
	// TODO: Implement actual CPU usage measurement
	return 0.0
}

// Monitoring Format Handlers (Future Expansion)

// handleZabbixMetricsGET handles GET requests for Zabbix format metrics (placeholder)
func (u *UtilsManager) handleZabbixMetricsGET(w http.ResponseWriter, r *http.Request) {
	_, authenticated := u.strategy.authManager.AuthenticateAndExtract(w, r)
	if !authenticated {
		return
	}

	u.logger.Info().Msg("🔄 Zabbix endpoint - Request received")

	// TODO: Implement Zabbix format conversion
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusNotImplemented)
	w.Write([]byte(`{"error": "Zabbix format endpoint not yet implemented"}`))
}

// handlePrometheusMetricsGET handles GET requests for Prometheus format metrics (placeholder)
func (u *UtilsManager) handlePrometheusMetricsGET(w http.ResponseWriter, r *http.Request) {
	_, authenticated := u.strategy.authManager.AuthenticateAndExtract(w, r)
	if !authenticated {
		return
	}

	u.logger.Info().Msg("🔄 Prometheus endpoint - Request received")

	// TODO: Implement Prometheus format conversion
	w.Header().Set("Content-Type", "text/plain")
	w.WriteHeader(http.StatusNotImplemented)
	w.Write([]byte("# Prometheus format endpoint not yet implemented\n"))
}