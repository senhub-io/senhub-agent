// senhub-agent/internal/agent/services/configuration/localConfiguration_watcher.go
package configuration

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/fsnotify/fsnotify"
)

// File Watching and YAML Generation

func (lc *LocalConfiguration) generateConfigYAML(config *LocalConfigurationData) ([]byte, error) {
	// This is a simplified version - in production you'd want a proper YAML generator with comments
	yamlTemplate := `# Agent configuration
agent:
  key: "%s"
  mode: offline
  generated: %t

# Auto-update configuration (disabled by default in offline mode)
auto_update:
  enabled: %t      # Enable/disable automatic updates
  url: "%s"        # Update server URL

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
probes:
  # ===== ACTIVE PROBES =====
  
  # CPU monitoring - 30s interval
  - name: cpu
    params:
      interval: 30
      
  # Memory monitoring - 30s interval  
  - name: memory
    params:
      interval: 30
      
  # Network monitoring - 60s interval (less frequent)
  - name: network
    params:
      interval: 60
      
  # Disk monitoring - 30s interval
  - name: logicaldisk
    params:
      interval: 30

# ===== CONFIGURATION EXAMPLES (COMMENTED) =====

# # Network connectivity
# - name: ping_gateway
#   params: {}  # Auto-detects gateway
#
# - name: ping_webapp  
#   params:
#     url: "https://example.com"  # REQUIRED
#
# - name: load_webapp
#   params:
#     url: "https://example.com"  # REQUIRED
#     timeout: 30                 # Optional, 1-300s, default: 30s

# # WiFi signal strength (auto-detects if WiFi available)
# - name: wifi_signal_strength
#   params: {}

# # Server hardware via Redfish (iDRAC, iLO, etc.)
# - name: redfish
#   params:
#     endpoint: "https://idrac.example.com"  # REQUIRED
#     username: "admin"                      # REQUIRED  
#     password: "password123"                # REQUIRED
#     interval: 300                          # Optional, default: 300s (5min)
#     verify_ssl: true                       # Optional, default: true
#     collections:                           # Optional, default: all
#       - system     # General system info
#       - thermal    # Temperatures, fans
#       - power      # Power supply, consumption
#       - processor  # CPU hardware
#       - memory     # RAM hardware  
#       - storage    # RAID, disks
#       - drives     # Individual drives
#       - networkadapter  # Network cards

# # Citrix Virtual Apps and Desktops monitoring
# - name: citrix
#   params:
#     base_url: "https://citrix-director.company.com"  # REQUIRED (API path added automatically)
#     
#     # Optional: Delivery Controller for site filtering (NEW)
#     delivery_controller:
#       url: "https://citrix-ddc.company.com"
#       fallback_urls:
#         - "https://citrix-ddc-backup.company.com"
#       site_filter: "SITE-NAME"  # Only monitor this site
#     
#     # environment parameter removed - was not used in metrics generation
#     interval: 120               # Optional, default: 120s (2min)
#     
#     auth:
#       # Authentication methods are automatic: NTLM for Director, Basic for DDC
#       username: "DOMAIN\\user"  # REQUIRED
#       password: "password"      # REQUIRED
#     
#     tls:
#       verify_ssl: true          # Optional, default: true
#     
#     timeout: 30                 # Optional, default: 30s
#     retry:
#       max_attempts: 3           # Optional, default: 3
#       backoff_factor: 2.0       # Optional, default: 2.0

# # Syslog event collection
# - name: syslog
#   params:
#     port: 514        # Optional, default: 514, range: 1-65535
#     protocol: "udp"  # Optional, default: "udp", values: "tcp"/"udp"

# # Custom events endpoint (POST /event)
# - name: event
#   params:
#     address: "127.0.0.1"  # Optional, default: "127.0.0.1" 
#     port: 5656            # Optional, default: 5656, range: 1-65535
#     protocol: "tcp"       # Optional, default: "tcp", values: "tcp"/"udp"

# # OpenTelemetry collector
# - name: otel
#   params:
#     endpoint: "http://localhost:4318"  # REQUIRED
#     name: "otel"                       # Optional, default: "otel"
#     interval: 60                       # Optional, default: 60s
#     protocol: "http"                   # Optional, auto-detected ("http"/"grpc")
#     telemetry_types:                   # Optional, default: all
#       - metrics
#       - traces
#       - logs
#     headers:                           # Optional, HTTP only
#       Authorization: "Bearer token123"
#     insecure: false                    # Optional, gRPC only

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

	return []byte(fmt.Sprintf(yamlTemplate,
		config.Agent.Key,
		config.Agent.Generated,
		config.AutoUpdate.Enabled,
		config.AutoUpdate.URL,
		config.Cache.RetentionMinutes,
		port,
		bindAddress,
		endpointsStr,
		tlsSection,
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
	// Compare storage configuration
	if len(old.Storage) != len(new.Storage) {
		return true
	}
	for i, storage := range old.Storage {
		if i >= len(new.Storage) || storage.Name != new.Storage[i].Name {
			return true
		}
		// Deep comparison of parameters would be more thorough
		// but for now we assume any storage section change matters
	}

	// Compare probes configuration
	if len(old.Probes) != len(new.Probes) {
		return true
	}
	for i, probe := range old.Probes {
		if i >= len(new.Probes) || probe.Name != new.Probes[i].Name {
			return true
		}
		// Similar to storage, we could do deeper parameter comparison
	}

	// For now, if we reach here, consider configuration unchanged
	// A more sophisticated comparison could be implemented later
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
