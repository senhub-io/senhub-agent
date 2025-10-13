// senhub-agent/internal/agent/services/configuration/localConfiguration_manager.go
package configuration

import (
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
	"time"

	"gopkg.in/yaml.v2"
	"senhub-agent.go/internal/agent/cliArgs"
)

// Configuration Management - Loading, creation, validation, and TLS generation

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

	// Check config version
	configVersion := config.ConfigVersion
	if configVersion == 0 {
		// No version field = version 1 format (legacy)
		lc.logger.Warn().Msg("Configuration has no version field, assuming version 1 (legacy format)")
		configVersion = 1
		config.ConfigVersion = 1
	}

	// Validate configuration version compatibility
	if err := ValidateConfigVersion(configVersion); err != nil {
		// Generate compatibility report
		report := CheckCompatibility(configVersion)
		lc.logger.Error().
			Int("config_version", configVersion).
			Int("expected_version", CurrentConfigVersion).
			Str("agent_version", cliArgs.Version).
			Msg(FormatCompatibilityReport(report))
		return fmt.Errorf("incompatible configuration version: %w", err)
	}

	// Log version info
	lc.logger.Info().
		Int("config_version", configVersion).
		Int("current_version", CurrentConfigVersion).
		Bool("needs_migration", NeedsMigration(configVersion)).
		Msg("Configuration version check completed")

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
		ConfigVersion: CurrentConfigVersion, // Use current version for new configs
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
	// In config version 2: 'name' is display name, 'type' is technical identifier
	// For default config, we use the same value for both
	return []ProbeConfig{
		{
			Name:   "cpu",           // Display name
			Type:   "cpu",           // Probe type
			Params: map[string]interface{}{"interval": 30},
		},
		{
			Name:   "memory",        // Display name
			Type:   "memory",        // Probe type
			Params: map[string]interface{}{"interval": 30},
		},
		{
			Name:   "network",       // Display name
			Type:   "network",       // Probe type
			Params: map[string]interface{}{"interval": 60},
		},
		{
			Name:   "logicaldisk",   // Display name
			Type:   "logicaldisk",   // Probe type
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

	// Validate probes config (version 2 format requires both 'name' and 'type')
	for i, probe := range config.Probes {
		if probe.Name == "" {
			return fmt.Errorf("probe %d: name cannot be empty", i)
		}
		if probe.Type == "" {
			return fmt.Errorf("probe %d (%s): type cannot be empty - configuration migration may be needed", i, probe.Name)
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
