// LocalConfiguration handles offline configuration from YAML files
// Responsibilities:
// - Local YAML configuration loading
// - Agent key generation for offline mode
// - TLS certificate management for offline mode
package configuration

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/fsnotify/fsnotify"
	"gopkg.in/yaml.v2"
	"senhub-agent.go/internal/agent/cliArgs"
	"senhub-agent.go/internal/agent/services/logger"
)

// LocalConfigurationData represents the YAML configuration structure
type LocalConfigurationData struct {
	Agent      LocalAgentConfig  `yaml:"agent"`
	Storage    []StorageConfig   `yaml:"storage"`
	Probes     []ProbeConfig     `yaml:"probes"`
	AutoUpdate *AutoUpdateConfig `yaml:"auto_update,omitempty"`
	Cache      *CacheConfig      `yaml:"cache,omitempty"`
}

// LocalAgentConfig represents agent-specific configuration
type LocalAgentConfig struct {
	Key       string `yaml:"key"`
	Mode      string `yaml:"mode"`
	Generated bool   `yaml:"generated"`
}

// TLSConfig represents TLS/HTTPS configuration
type TLSConfig struct {
	Enabled       bool     `yaml:"enabled"`
	MinTlsVersion string   `yaml:"min_tls_version"`
	CipherSuites  []string `yaml:"cipher_suites"`
}

// AutoUpdateConfig represents auto-update configuration
type AutoUpdateConfig struct {
	Enabled bool   `yaml:"enabled"`
	URL     string `yaml:"url"`
}

// CacheConfig represents cache configuration
type CacheConfig struct {
	RetentionMinutes int `yaml:"retention_minutes"`
}

// LocalConfiguration manages offline configuration
type LocalConfiguration struct {
	data          LocalConfigurationData
	logger        *logger.ModuleLogger
	configPath    string
	args          *cliArgs.ParsedArgs
	eventNotifier *EventNotifier
	watcher       *fsnotify.Watcher
	quitChannel   chan struct{}
}

// NewLocalConfiguration creates a new LocalConfiguration instance
func NewLocalConfiguration(
	args *cliArgs.ParsedArgs,
	baseLogger *logger.Logger,
) *LocalConfiguration {
	// Create module-specific logger for local configuration
	moduleLogger := logger.NewModuleLogger(baseLogger, "configuration.local")
	moduleLogger.Debug().Msg("Creating new LocalConfiguration instance")

	lc := &LocalConfiguration{
		logger:        moduleLogger,
		configPath:    args.ConfigPath,
		args:          args,
		data:          LocalConfigurationData{},
		eventNotifier: NewEventNotifier(moduleLogger.Logger),
	}

	// Try to load existing configuration immediately
	if _, err := os.Stat(lc.configPath); err == nil {
		// File exists, load it
		if err := lc.loadConfiguration(); err != nil {
			moduleLogger.Warn().Err(err).Msg("Failed to load existing configuration, will use defaults")
		}
	}

	moduleLogger.Debug().Msg("LocalConfiguration instance created successfully")
	return lc
}

// GetName returns the service name
func (lc *LocalConfiguration) GetName() string {
	return "LocalConfiguration"
}

// GetAgentKey returns the agent key from the local configuration
func (lc *LocalConfiguration) GetAgentKey() string {
	return lc.data.Agent.Key
}

// GetAuthenticationKey implements AgentConfiguration interface
func (lc *LocalConfiguration) GetAuthenticationKey() string {
	return lc.data.Agent.Key
}

// GetServerUrl implements AgentConfiguration interface
func (lc *LocalConfiguration) GetServerUrl() string {
	// In offline mode, we don't have a server URL
	return ""
}

// GetAutoUpdateConfig returns the auto-update configuration
func (lc *LocalConfiguration) GetAutoUpdateConfig() *AutoUpdateConfig {
	if lc.data.AutoUpdate == nil {
		// Return default configuration
		return &AutoUpdateConfig{
			Enabled: false,
			URL:     "https://eu-west-1.intake.senhub.io/releases",
		}
	}
	return lc.data.AutoUpdate
}

// GetCacheConfig returns the cache configuration
func (lc *LocalConfiguration) GetCacheConfig() *CacheConfig {
	if lc.data.Cache == nil {
		lc.logger.Warn().Msg("Cache configuration is nil in YAML, using default (5 minutes)")
		// Return default configuration
		return &CacheConfig{
			RetentionMinutes: 5,
		}
	}
	lc.logger.Info().
		Int("retention_minutes", lc.data.Cache.RetentionMinutes).
		Msg("Cache configuration loaded from YAML")
	return lc.data.Cache
}

// GetConfiguration returns the configuration data in RemoteConfigurationData format
func (lc *LocalConfiguration) GetConfiguration() RemoteConfigurationData {
	// Get auto-update configuration
	autoUpdate := lc.GetAutoUpdateConfig()

	// Convert auto-update interval based on enabled status
	var updateInterval int
	if autoUpdate.Enabled {
		updateInterval = 3600 // 1 hour in seconds
	} else {
		updateInterval = 0 // Disabled
	}

	// Convert local config format to remote config format
	return RemoteConfigurationData{
		StorageConfig: lc.data.Storage,
		Probes:        lc.data.Probes,
		Agent: AgentConfig{
			RegistryUrl:         autoUpdate.URL,
			Version:             "",
			UpdateCheckInterval: updateInterval,
		},
	}
}

// OnConfigChanged registers a callback for configuration changes
func (lc *LocalConfiguration) OnConfigChanged(callback func(string)) {
	lc.logger.Info().Msg("Registering new configuration change callback")
	lc.eventNotifier.RegisterObserver(callback)
}

// Start initializes the local configuration and begins file watching
func (lc *LocalConfiguration) Start(quitChannel chan struct{}) error {
	lc.logger.Info().Msg("Starting LocalConfiguration with file watching")
	lc.quitChannel = quitChannel

	// Load or create configuration
	if err := lc.loadOrCreateConfiguration(); err != nil {
		return fmt.Errorf("failed to load configuration: %w", err)
	}

	// Initialize file watcher
	var err error
	lc.watcher, err = fsnotify.NewWatcher()
	if err != nil {
		return fmt.Errorf("failed to create file watcher: %w", err)
	}

	// Add config file to watcher
	err = lc.watcher.Add(lc.configPath)
	if err != nil {
		_ = lc.watcher.Close()
		return fmt.Errorf("failed to watch config file %s: %w", lc.configPath, err)
	}

	lc.logger.Info().Str("config_path", lc.configPath).Msg("Started watching configuration file")

	// Start watching goroutine
	go lc.watchConfigFile()

	return nil
}

// Shutdown performs cleanup and stops file watching
func (lc *LocalConfiguration) Shutdown(ctx context.Context) error {
	lc.logger.Info().Msg("Shutting down LocalConfiguration")

	if lc.watcher != nil {
		if err := lc.watcher.Close(); err != nil {
			lc.logger.Warn().Err(err).Msg("Error closing file watcher")
		}
	}

	return nil
}

// loadOrCreateConfiguration loads existing config or creates a new one
func (lc *LocalConfiguration) loadOrCreateConfiguration() error {
	if _, err := os.Stat(lc.configPath); os.IsNotExist(err) {
		lc.logger.Info().Msgf("Configuration file not found, creating default: %s", lc.configPath)
		return lc.createDefaultConfiguration()
	}

	lc.logger.Info().Msgf("Loading configuration from: %s", lc.configPath)
	return lc.loadConfiguration()
}

// loadConfiguration loads configuration from YAML file
func (lc *LocalConfiguration) loadConfiguration() error {
	data, err := os.ReadFile(lc.configPath)
	if err != nil {
		return fmt.Errorf("failed to read config file: %w", err)
	}

	var config LocalConfigurationData
	if err := yaml.Unmarshal(data, &config); err != nil {
		return fmt.Errorf("failed to parse YAML config: %w", err)
	}

	// Fix YAML interface conversion issues
	config = lc.fixYAMLTypes(config)

	// Validate configuration
	if err := lc.validateConfiguration(&config); err != nil {
		return fmt.Errorf("invalid configuration: %w", err)
	}

	lc.data = config
	lc.logger.Info().Msg("Configuration loaded successfully")
	return nil
}

// createDefaultConfiguration creates a default configuration file
func (lc *LocalConfiguration) createDefaultConfiguration() error {
	// Generate agent key if not provided
	agentKey := lc.args.AuthenticationKey
	if agentKey == "" {
		var err error
		agentKey, err = lc.generateOfflineAgentKey()
		if err != nil {
			return fmt.Errorf("failed to generate agent key: %w", err)
		}
	}

	// Create default configuration
	config := LocalConfigurationData{
		Agent: LocalAgentConfig{
			Key:       agentKey,
			Mode:      "offline",
			Generated: lc.args.AuthenticationKey == "", // Mark as generated if we created it
		},
		Storage:    lc.createDefaultStorageConfig(),
		Probes:     lc.createDefaultProbesConfig(),
		AutoUpdate: lc.createDefaultAutoUpdateConfig(),
		Cache:      lc.createDefaultCacheConfig(),
	}

	// Create directory if it doesn't exist
	configDir := filepath.Dir(lc.configPath)
	if err := os.MkdirAll(configDir, 0750); err != nil {
		return fmt.Errorf("failed to create config directory: %w", err)
	}

	// Generate YAML with comments
	yamlData, err := lc.generateConfigYAML(&config)
	if err != nil {
		return fmt.Errorf("failed to generate YAML: %w", err)
	}

	// Write configuration file
	if err := os.WriteFile(lc.configPath, yamlData, 0600); err != nil {
		return fmt.Errorf("failed to write config file: %w", err)
	}

	lc.data = config
	lc.logger.Info().Msgf("Default configuration created: %s", lc.configPath)

	// Generate TLS certificates if HTTPS is enabled
	if lc.args.EnableHttps {
		if err := lc.generateTLSCertificates(); err != nil {
			lc.logger.Warn().Err(err).Msg("Failed to generate TLS certificates")
		}
	}

	return nil
}

// generateOfflineAgentKey creates a unique agent key for offline mode
func (lc *LocalConfiguration) generateOfflineAgentKey() (string, error) {
	// Generate a UUID v4
	uuid := make([]byte, 16)
	if _, err := rand.Read(uuid); err != nil {
		return "", fmt.Errorf("failed to generate random bytes for UUID: %w", err)
	}

	// Set version (4) and variant bits
	uuid[6] = (uuid[6] & 0x0f) | 0x40 // Version 4
	uuid[8] = (uuid[8] & 0x3f) | 0x80 // Variant bits

	// Format as UUID string
	key := fmt.Sprintf("%08x-%04x-%04x-%04x-%012x",
		uuid[0:4], uuid[4:6], uuid[6:8], uuid[8:10], uuid[10:16])

	lc.logger.Info().Msg("Generated offline agent key (UUID)")
	return key, nil
}

// createDefaultStorageConfig creates default storage configuration
func (lc *LocalConfiguration) createDefaultStorageConfig() []StorageConfig {
	httpParams := map[string]interface{}{
		"port":         8080,
		"bind_address": "127.0.0.1",
		"endpoints":    []string{"prtg", "web", "nagios"},
	}

	// Add TLS configuration if HTTPS is enabled
	if lc.args.EnableHttps {
		httpParams["port"] = lc.args.HttpsPort
		httpParams["bind_address"] = "0.0.0.0" // Accept external connections with HTTPS

		tlsConfig := TLSConfig{
			Enabled:       true,
			MinTlsVersion: lc.args.MinTlsVersion,
		}

		httpParams["tls"] = tlsConfig
	}

	return []StorageConfig{
		{
			Name:   "http",
			Params: httpParams,
		},
	}
}

// createDefaultProbesConfig creates default probes configuration
func (lc *LocalConfiguration) createDefaultProbesConfig() []ProbeConfig {
	return []ProbeConfig{
		{
			Name:   "cpu",
			Params: map[string]interface{}{"interval": 30},
		},
		{
			Name:   "memory",
			Params: map[string]interface{}{"interval": 30},
		},
		{
			Name:   "network",
			Params: map[string]interface{}{"interval": 60},
		},
		{
			Name:   "logicaldisk",
			Params: map[string]interface{}{"interval": 30},
		},
	}
}

// createDefaultAutoUpdateConfig creates default auto-update configuration
func (lc *LocalConfiguration) createDefaultAutoUpdateConfig() *AutoUpdateConfig {
	return &AutoUpdateConfig{
		Enabled: false, // Disabled by default in offline mode
		URL:     "https://eu-west-1.intake.senhub.io/releases",
	}
}

// createDefaultCacheConfig creates default cache configuration
func (lc *LocalConfiguration) createDefaultCacheConfig() *CacheConfig {
	return &CacheConfig{
		RetentionMinutes: 5, // Default 5 minutes retention
	}
}

// fixYAMLTypes converts map[interface{}]interface{} to map[string]interface{}
// This is needed because yaml.v2 unmarshals into map[interface{}]interface{}
// but Go JSON expects map[string]interface{}
func (lc *LocalConfiguration) fixYAMLTypes(config LocalConfigurationData) LocalConfigurationData {
	// Fix storage configs
	for i, storage := range config.Storage {
		if converted := lc.convertMapTypes(storage.Params); converted != nil {
			if convertedMap, ok := converted.(map[string]interface{}); ok {
				config.Storage[i].Params = convertedMap
			}
		}
	}

	// Fix probe configs
	for i, probe := range config.Probes {
		if converted := lc.convertMapTypes(probe.Params); converted != nil {
			if convertedMap, ok := converted.(map[string]interface{}); ok {
				config.Probes[i].Params = convertedMap
			}
		}
	}

	return config
}

// convertMapTypes recursively converts map[interface{}]interface{} to map[string]interface{}
func (lc *LocalConfiguration) convertMapTypes(input interface{}) interface{} {
	switch v := input.(type) {
	case map[interface{}]interface{}:
		result := make(map[string]interface{})
		for key, value := range v {
			if keyStr, ok := key.(string); ok {
				result[keyStr] = lc.convertMapTypes(value)
			}
		}
		return result
	case map[string]interface{}:
		result := make(map[string]interface{})
		for key, value := range v {
			result[key] = lc.convertMapTypes(value)
		}
		return result
	case []interface{}:
		result := make([]interface{}, len(v))
		for i, item := range v {
			result[i] = lc.convertMapTypes(item)
		}
		return result
	default:
		return v
	}
}

// validateConfiguration validates the local configuration
func (lc *LocalConfiguration) validateConfiguration(config *LocalConfigurationData) error {
	if config == nil {
		return fmt.Errorf("configuration cannot be nil")
	}

	// Validate agent config
	if config.Agent.Key == "" {
		return fmt.Errorf("agent key cannot be empty")
	}

	// Validate storage config
	if len(config.Storage) == 0 {
		return fmt.Errorf("at least one storage strategy is required")
	}

	for _, storage := range config.Storage {
		if storage.Name == "" {
			return fmt.Errorf("storage strategy name cannot be empty")
		}
	}

	// Validate probes config
	for _, probe := range config.Probes {
		if probe.Name == "" {
			return fmt.Errorf("probe name cannot be empty")
		}
	}

	return nil
}

// generateTLSCertificates generates certificates for HTTPS (always when HTTPS enabled)
func (lc *LocalConfiguration) generateTLSCertificates() error {
	if !lc.args.EnableHttps {
		return nil // Skip if HTTPS disabled
	}

	lc.logger.Info().Msg("Generating TLS certificates (replace with your own if needed)")

	// Create certs directory with absolute path
	currentDir, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("failed to get current working directory: %w", err)
	}
	certsDir := filepath.Join(currentDir, "certs")
	if err := os.MkdirAll(certsDir, 0750); err != nil {
		return fmt.Errorf("failed to create certs directory: %w", err)
	}

	// Generate certificate
	certPEM, keyPEM, err := lc.generateSelfSignedCert()
	if err != nil {
		return fmt.Errorf("failed to generate certificate: %w", err)
	}

	// Write certificate files
	certPath := filepath.Join(certsDir, "agent-cert.pem")
	keyPath := filepath.Join(certsDir, "agent-key.pem")

	if err := os.WriteFile(certPath, certPEM, 0600); err != nil {
		return fmt.Errorf("failed to write certificate: %w", err)
	}

	if err := os.WriteFile(keyPath, keyPEM, 0600); err != nil {
		return fmt.Errorf("failed to write private key: %w", err)
	}

	lc.logger.Info().Msgf("TLS certificates generated: %s, %s", certPath, keyPath)
	lc.logger.Info().Msg("To use your own certificates, replace these files with your own")
	return nil
}

// generateSelfSignedCert generates a self-signed certificate
func (lc *LocalConfiguration) generateSelfSignedCert() ([]byte, []byte, error) {
	// Generate RSA private key
	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to generate private key: %w", err)
	}

	// Create certificate template
	template := x509.Certificate{
		SerialNumber: big.NewInt(time.Now().Unix()),
		Subject: pkix.Name{
			Organization:  []string{"SenHub Agent"},
			Country:       []string{"US"},
			Province:      []string{""},
			Locality:      []string{""},
			StreetAddress: []string{""},
			PostalCode:    []string{""},
			CommonName:    "localhost",
		},
		NotBefore:             time.Now(),
		NotAfter:              time.Now().Add(365 * 24 * time.Hour),
		KeyUsage:              x509.KeyUsageKeyEncipherment | x509.KeyUsageDigitalSignature,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
	}

	// Add Subject Alternative Names
	for _, host := range lc.args.HttpsHosts {
		if ip := net.ParseIP(host); ip != nil {
			template.IPAddresses = append(template.IPAddresses, ip)
		} else {
			template.DNSNames = append(template.DNSNames, host)
		}
	}

	// Generate certificate
	certDER, err := x509.CreateCertificate(rand.Reader, &template, &template, &privateKey.PublicKey, privateKey)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to create certificate: %w", err)
	}

	// Encode to PEM format
	certPEM := pem.EncodeToMemory(&pem.Block{
		Type:  "CERTIFICATE",
		Bytes: certDER,
	})

	keyPEM := pem.EncodeToMemory(&pem.Block{
		Type:  "RSA PRIVATE KEY",
		Bytes: x509.MarshalPKCS1PrivateKey(privateKey),
	})

	return certPEM, keyPEM, nil
}

// generateConfigYAML generates YAML configuration with comments
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
