// senhub-agent/internal/agent/services/data_store/http_utils.go
package http

import (
	"fmt"
	"net/http"
	"strings"

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
	// All descriptions removed for uniform tag display
	descriptions := map[string]string{
		// Tags display as "tag_name (X values)" without additional descriptions
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
	commit := cliArgs.CommitHash

	// Linux/Makefile case: Version is properly set, use it directly
	if version != "" {
		return VersionInfo{
			Version: version,
			Commit:  formatCommitHash(commit),
		}
	}

	// Windows/no-Makefile case: Version is empty, try to extract from CommitHash
	var extractedCommitHash string
	if commit != "" {
		// Parse git describe format: "tag-commits-ghash-dirty"
		if strings.Contains(commit, "-g") {
			parts := strings.Split(commit, "-")
			for i, part := range parts {
				if strings.HasPrefix(part, "g") && i > 0 {
					// Version is everything before the commit count
					version = strings.Join(parts[:i-1], "-")
					// Extract commit hash (part starting with 'g')
					extractedCommitHash = part
					break
				}
			}
		} else {
			// If no -g, it might be just a tag or commit hash
			version = commit
			extractedCommitHash = commit
		}
	}

	// Final fallback
	if version == "" {
		version = "development"
	}

	// Format commit hash for display - use extracted hash, not full commit string
	var formattedCommit string
	if extractedCommitHash != "" {
		formattedCommit = formatCommitHash(extractedCommitHash)
	} else {
		formattedCommit = formatCommitHash(commit)
	}

	return VersionInfo{
		Version: version,
		Commit:  formattedCommit,
	}
}

// formatCommitHash formats a commit hash for human-readable display
func formatCommitHash(commit string) string {
	if commit == "" {
		return ""
	}

	// Handle single hash part with 'g' prefix (e.g., "g302b166")
	if strings.HasPrefix(commit, "g") && len(commit) > 1 {
		// Extract short hash (first 7 chars after 'g', or all if shorter)
		hashPart := commit[1:]
		if len(hashPart) >= 7 {
			return hashPart[:7]
		}
		return hashPart
	}

	// Handle git describe format: "tag-commits-ghash-dirty"
	if strings.Contains(commit, "-g") {
		parts := strings.Split(commit, "-")
		for i, part := range parts {
			if strings.HasPrefix(part, "g") && len(part) > 1 {
				// Extract short hash (first 7 chars after 'g')
				hashPart := part[1:]
				if len(hashPart) >= 7 {
					hashPart = hashPart[:7]
				}
				// Check if it's dirty
				isDirty := len(parts) > i+1 && parts[i+1] == "dirty"
				if isDirty {
					return fmt.Sprintf("%s (modified)", hashPart)
				}
				return hashPart
			}
		}
	}

	// Handle plain commit hash - take first 7 characters
	if len(commit) >= 7 {
		return commit[:7]
	}

	return commit
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
	if _, err := w.Write([]byte(`{"error": "Zabbix format endpoint not yet implemented"}`)); err != nil {
		u.logger.Error().Err(err).Msg("Failed to write Zabbix error response")
	}
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
	if _, err := w.Write([]byte("# Prometheus format endpoint not yet implemented\n")); err != nil {
		u.logger.Error().Err(err).Msg("Failed to write Prometheus error response")
	}
}
