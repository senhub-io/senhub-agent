// senhub-agent/internal/agent/services/configuration/localConfiguration_watcher.go
package configuration

import (
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"time"

	"github.com/fsnotify/fsnotify"
)

// File Watching and YAML Generation

func (lc *LocalConfiguration) generateConfigYAML(config *LocalConfigurationData) ([]byte, error) {
	// This is a simplified version - in production you'd want a proper YAML generator with comments
	yamlTemplate := `# SenHub Agent Configuration
# Configuration Version: %d (automatically managed)
# Agent Version: %s
# Generated: %s
#
# DO NOT modify config_version manually - it is managed by the agent

config_version: %d

# Agent configuration
agent:
  key: "%s"
  mode: offline
  # license: ""  # Uncomment and add your license token here
%s

# Auto-update configuration
auto_update:
  enabled: %t           # Enable/disable automatic updates
  include_beta: %t      # Include beta versions in update checks
  url: "%s"             # Update server URL

# Cache configuration
cache:
  retention_minutes: %d  # Cache retention time in minutes

# Local storage with web interface
storage:
  - name: http
    params:
      port: %d
      bind_address: "%s"
      endpoints: [%s]
%s

# Active probes (default system monitoring)
# Note: 'name' is the display name (free choice), 'type' is the probe type (technical identifier)
probes:
  # ===== ACTIVE PROBES =====

  # CPU monitoring - 30s interval
  - name: cpu              # Display name (you can change this)
    type: cpu              # Probe type (must match registered probe)
    params:
      interval: 30

  # Memory monitoring - 30s interval
  - name: memory           # Display name
    type: memory           # Probe type
    params:
      interval: 30

  # Network monitoring - 60s interval (less frequent)
  - name: network          # Display name
    type: network          # Probe type
    params:
      interval: 60

  # Disk monitoring - 30s interval
  - name: logicaldisk      # Display name
    type: logicaldisk      # Probe type
    params:
      interval: 30
` + ProbeExamplesTemplate + `
`

	// Extract storage config values
	httpStorage := config.Storage[0]
	port := httpStorage.Params["port"].(int)
	bindAddress := httpStorage.Params["bind_address"].(string)

	endpoints := httpStorage.Params["endpoints"].([]string)
	endpointsStr := `"` + strings.Join(endpoints, `", "`) + `"`

	// TLS configuration section
	tlsSection := ""
	if lc.args.EnableHttps {
		// Get absolute paths for certificates
		currentDir, err := os.Getwd()
		if err != nil {
			return nil, fmt.Errorf("failed to get current working directory for TLS config: %w", err)
		}
		certPath := filepath.Join(currentDir, "certs", "agent-cert.pem")
		keyPath := filepath.Join(currentDir, "certs", "agent-key.pem")

		// Escape backslashes for Windows paths in YAML
		certPathYAML := strings.ReplaceAll(certPath, "\\", "\\\\")
		keyPathYAML := strings.ReplaceAll(keyPath, "\\", "\\\\")

		tlsSection = `      tls:
        enabled: true
        min_tls_version: "` + lc.args.MinTlsVersion + `"
        cert_file: "` + certPathYAML + `"
        key_file: "` + keyPathYAML + `"`
	}

	// Get agent version for header
	agentVersion := "unknown"
	if lc.args != nil && lc.args.Version != "" {
		agentVersion = lc.args.Version
	}

	// Get timestamp for header
	timestamp := time.Now().Format("2006-01-02 15:04:05 MST")

	return []byte(fmt.Sprintf(yamlTemplate,
		config.ConfigVersion,          // Header comment: config version
		agentVersion,                  // Header comment: agent version
		timestamp,                     // Header comment: timestamp
		config.ConfigVersion,          // YAML field: config_version
		config.Agent.Key,              // agent.key
		LicenseDocumentationTemplate,  // agent.license (documentation)
		config.AutoUpdate.Enabled,     // auto_update.enabled
		config.AutoUpdate.IncludeBeta, // auto_update.include_beta
		config.AutoUpdate.URL,         // auto_update.url
		config.Cache.RetentionMinutes, // cache.retention_minutes
		port,                          // storage port
		bindAddress,                   // storage bind_address
		endpointsStr,                  // storage endpoints
		tlsSection,                    // storage TLS section (optional)
	)), nil
}

// watchConfigFile monitors the configuration file for changes
func (lc *LocalConfiguration) watchConfigFile() {
	lc.logger.Debug().Msg("Started configuration file watching goroutine")

	for {
		select {
		case <-lc.quitChannel:
			lc.logger.Debug().Msg("Configuration file watching stopped")
			return

		case event, ok := <-lc.watcher.Events:
			if !ok {
				lc.logger.Debug().Msg("File watcher events channel closed")
				return
			}

			lc.logger.Debug().
				Str("event", event.String()).
				Msg("Configuration file event received")

			// Handle various file change events
			if event.Has(fsnotify.Write) || event.Has(fsnotify.Create) || event.Has(fsnotify.Chmod) {
				lc.logger.Info().
					Str("config_path", lc.configPath).
					Str("event_type", event.Op.String()).
					Msg("Configuration file changed, reloading...")

				// Small delay to ensure file write is complete
				time.Sleep(200 * time.Millisecond)

				// Check if file still exists (some editors delete/recreate)
				if _, err := os.Stat(lc.configPath); os.IsNotExist(err) {
					lc.logger.Warn().Msg("Configuration file was deleted, skipping reload")
					continue
				}

				if err := lc.reloadConfiguration(); err != nil {
					lc.logger.Error().
						Err(err).
						Msg("Failed to reload configuration")
				} else {
					lc.logger.Info().Msg("Configuration reloaded successfully")
				}
			} else if event.Has(fsnotify.Remove) || event.Has(fsnotify.Rename) {
				lc.logger.Warn().
					Str("event_type", event.Op.String()).
					Msg("Configuration file was removed or renamed, attempting to re-watch...")

				// Try to re-add the file to the watcher after a delay
				// This handles editors that delete/recreate files
				go lc.attemptRewatch()
			}

		case err, ok := <-lc.watcher.Errors:
			if !ok {
				lc.logger.Debug().Msg("File watcher errors channel closed")
				return
			}
			lc.logger.Warn().
				Err(err).
				Msg("Configuration file watcher error")
		}
	}
}

// reloadConfiguration reloads the configuration and notifies observers
func (lc *LocalConfiguration) reloadConfiguration() error {
	// Store previous configuration for comparison
	previousData := lc.data

	// Load new configuration
	if err := lc.loadConfiguration(); err != nil {
		return fmt.Errorf("failed to load configuration: %w", err)
	}

	// Check if configuration actually changed
	if lc.hasConfigurationChanged(previousData, lc.data) {
		lc.logger.Info().
			Any("old_storage", previousData.Storage).
			Any("new_storage", lc.data.Storage).
			Any("old_probes", previousData.Probes).
			Any("new_probes", lc.data.Probes).
			Msg("Configuration changes detected, notifying observers")

		// Notify all observers about the configuration change
		lc.eventNotifier.NotifyObservers("Configuration file changed")
	} else {
		lc.logger.Info().Msg("Configuration file changed but content is identical")
	}

	return nil
}

// hasConfigurationChanged compares two configurations for differences
func (lc *LocalConfiguration) hasConfigurationChanged(old, new LocalConfigurationData) bool {
	// Use reflect.DeepEqual for comprehensive comparison
	// This detects ALL changes including:
	// - Added/removed probes or storage
	// - Modified parameters (interval, url, etc.)
	// - Modified probe types
	// - Modified storage configuration
	// - Cache configuration changes

	if !reflect.DeepEqual(old.Storage, new.Storage) {
		lc.logger.Debug().Msg("Storage configuration changed")
		return true
	}

	if !reflect.DeepEqual(old.Probes, new.Probes) {
		lc.logger.Debug().Msg("Probes configuration changed")
		return true
	}

	if !reflect.DeepEqual(old.Cache, new.Cache) {
		lc.logger.Debug().Msg("Cache configuration changed")
		return true
	}

	// Auto-update config changes don't require reload of probes/storage
	// but we could handle them separately if needed

	return false
}

// attemptRewatch tries to re-add the configuration file to the watcher
// This handles editors that delete and recreate files during save operations
func (lc *LocalConfiguration) attemptRewatch() {
	maxRetries := 5
	retryDelay := 500 * time.Millisecond

	for i := 0; i < maxRetries; i++ {
		// Wait a bit for the file to be recreated
		time.Sleep(retryDelay)

		// Check if file exists
		if _, err := os.Stat(lc.configPath); err == nil {
			// File exists, try to add it back to watcher
			if err := lc.watcher.Add(lc.configPath); err != nil {
				lc.logger.Warn().
					Err(err).
					Int("attempt", i+1).
					Msg("Failed to re-watch configuration file")
			} else {
				lc.logger.Info().
					Int("attempt", i+1).
					Msg("Successfully re-added configuration file to watcher")

				// File is back, try to reload configuration
				if err := lc.reloadConfiguration(); err != nil {
					lc.logger.Error().
						Err(err).
						Msg("Failed to reload configuration after re-watch")
				} else {
					lc.logger.Info().Msg("Configuration reloaded successfully after re-watch")
				}
				return
			}
		} else {
			lc.logger.Debug().
				Int("attempt", i+1).
				Msg("Configuration file not yet recreated, retrying...")
		}

		// Increase delay for next retry
		retryDelay *= 2
	}

	lc.logger.Error().
		Int("max_retries", maxRetries).
		Msg("Failed to re-watch configuration file after maximum retries")
}
