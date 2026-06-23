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
	"strings"
	"time"

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

// loadConfiguration loads configuration from disk.
//
// Since Sprint A this delegates to LoadFromDisk which transparently
// handles both the legacy monolithic agent-config.yaml AND the new
// multi-file layout (agent.yaml + probes.d/ + strategies.d/),
// applying ${env:..} / ${file:..} substitution on the merged result.
// Backward compatibility for existing single-file installs is
// preserved by content-based detection inside LoadFromDisk — no
// operator action needed to keep an older config working.
func (lc *LocalConfiguration) loadConfiguration() error {
	config, err := LoadFromDisk(lc.configPath, lc.logger)
	if err != nil {
		return err // already wrapped with the offending path
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

	lc.storeData(config)
	lc.logger.Info().Msg("Configuration loaded successfully")
	return nil
}

// createDefaultConfiguration writes a fresh multi-file configuration
// layout: `agent.yaml` (globals only) next to the configured config
// path, plus sibling `probes.d/00-host.yaml` (default host probes)
// and `strategies.d/00-http.yaml` (default HTTP strategy).
//
// Pre-0.2.x the install path wrote a monolithic agent-config.yaml
// with everything inline. The multi-file layout is the supported
// default from 0.2.x onward — operators can drop fragments into the
// `.d/` directories without editing the central file. Legacy
// monolithic configs are still LOADED transparently by loader.go's
// auto-detection; only the install-time *writer* has switched.
//
// The agent key is always freshly generated (UUID v4). Pre-0.2.0
// the caller could seed it via --authentication-key; that flag was
// removed with the legacy remote-config loader.
func (lc *LocalConfiguration) createDefaultConfiguration() error {
	agentKey, err := lc.generateAgentKey()
	if err != nil {
		return fmt.Errorf("failed to generate agent key: %w", err)
	}

	configDir := filepath.Dir(lc.configPath)
	probesDir := filepath.Join(configDir, "probes.d")
	strategiesDir := filepath.Join(configDir, "strategies.d")

	for _, dir := range []string{configDir, probesDir, strategiesDir} {
		if err := os.MkdirAll(dir, 0750); err != nil {
			return fmt.Errorf("failed to create directory %s: %w", dir, err)
		}
	}

	// 1. agent.yaml — globals only.
	agentYAML, err := lc.generateAgentYAML(agentKey)
	if err != nil {
		return fmt.Errorf("failed to generate agent.yaml: %w", err)
	}
	if err := os.WriteFile(lc.configPath, agentYAML, 0600); err != nil {
		return fmt.Errorf("failed to write agent.yaml: %w", err)
	}

	// 2. probes.d/00-host.yaml — default host probes.
	probesPath := filepath.Join(probesDir, "00-host.yaml")
	if err := os.WriteFile(probesPath, []byte(HostProbesFragmentTemplate), 0600); err != nil {
		return fmt.Errorf("failed to write %s: %w", probesPath, err)
	}

	// 3. strategies.d/00-http.yaml — default HTTP strategy.
	httpPath := filepath.Join(strategiesDir, "00-http.yaml")
	httpYAML := lc.generateHTTPStrategyFragment()
	if err := os.WriteFile(httpPath, []byte(httpYAML), 0600); err != nil {
		return fmt.Errorf("failed to write %s: %w", httpPath, err)
	}

	// Populate the in-memory data so the rest of Start() works without
	// re-reading the files we just wrote.
	lc.storeData(LocalConfigurationData{
		ConfigVersion: CurrentConfigVersion,
		Agent: LocalAgentConfig{
			Key: agentKey,
		},
		Storage:    lc.createDefaultStorageConfig(),
		Probes:     lc.createDefaultProbesConfig(),
		AutoUpdate: lc.createDefaultAutoUpdateConfig(),
		Cache:      lc.createDefaultCacheConfig(),
	})
	lc.logger.Info().
		Str("agent_yaml", lc.configPath).
		Str("probes_d", probesDir).
		Str("strategies_d", strategiesDir).
		Msg("Default multi-file configuration created")

	if lc.args.EnableHttps {
		if err := lc.generateTLSCertificates(); err != nil {
			lc.logger.Warn().Err(err).Msg("Failed to generate TLS certificates")
		}
	}

	return nil
}

// generateAgentYAML renders the globals-only top-level file for a
// fresh install. The TLS knobs go into the HTTP strategy fragment
// (generateHTTPStrategyFragment), not here — agent.yaml is strictly
// globals.
func (lc *LocalConfiguration) generateAgentYAML(agentKey string) ([]byte, error) {
	autoUpdate := lc.createDefaultAutoUpdateConfig()
	cache := lc.createDefaultCacheConfig()

	agentVersion := "unknown"
	if lc.args != nil && lc.args.Version != "" {
		agentVersion = lc.args.Version
	}
	timestamp := time.Now().Format("2006-01-02 15:04:05 MST")

	out := fmt.Sprintf(AgentYAMLTemplate,
		CurrentConfigVersion,
		agentVersion,
		timestamp,
		CurrentConfigVersion,
		agentKey,
		LicenseDocumentationTemplate,
		autoUpdate.Enabled,
		autoUpdate.IncludeBeta,
		autoUpdate.URL,
		cache.RetentionMinutes,
	)
	return []byte(out), nil
}

// generateHTTPStrategyFragment renders the default HTTP strategy
// fragment (strategies.d/00-http.yaml). When --enable-https is set,
// the strategy gets a TLS block + flips to the HTTPS port and binds
// to 0.0.0.0; otherwise it stays on 127.0.0.1:8080 with PRTG / Web /
// Nagios endpoints.
func (lc *LocalConfiguration) generateHTTPStrategyFragment() string {
	port := 8080
	bindAddress := "127.0.0.1"
	tlsSection := ""

	if lc.args != nil && lc.args.EnableHttps {
		port = lc.args.HttpsPort
		bindAddress = "0.0.0.0"

		// HTTPS certificates are written next to the configuration by
		// generateTLSCertificates; reference them as absolute paths so
		// the strategy keeps working whatever the daemon's cwd is.
		certsDir := filepath.Join(filepath.Dir(lc.configPath), "certs")
		certPath := filepath.Join(certsDir, "agent-cert.pem")
		keyPath := filepath.Join(certsDir, "agent-key.pem")
		// Escape backslashes for Windows paths.
		certPathYAML := strings.ReplaceAll(certPath, "\\", "\\\\")
		keyPathYAML := strings.ReplaceAll(keyPath, "\\", "\\\\")

		tlsSection = "  tls:\n" +
			"    enabled: true\n" +
			"    min_tls_version: \"" + lc.args.MinTlsVersion + "\"\n" +
			"    cert_file: \"" + certPathYAML + "\"\n" +
			"    key_file: \"" + keyPathYAML + "\"\n"
	}

	endpointsCSV := `"prtg", "web", "nagios"`
	return fmt.Sprintf(HTTPStrategyFragmentTemplate, port, bindAddress, endpointsCSV, tlsSection)
}

// generateAgentKey creates a unique agent key
func (lc *LocalConfiguration) generateAgentKey() (string, error) {
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

	lc.logger.Info().Msg("Generated agent key (UUID)")
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
			Name:   "cpu", // Display name
			Type:   "cpu", // Probe type
			Params: map[string]interface{}{"interval": 30},
		},
		{
			Name:   "memory", // Display name
			Type:   "memory", // Probe type
			Params: map[string]interface{}{"interval": 30},
		},
		{
			Name:   "network", // Display name
			Type:   "network", // Probe type
			Params: map[string]interface{}{"interval": 60},
		},
		{
			Name:   "logicaldisk", // Display name
			Type:   "logicaldisk", // Probe type
			Params: map[string]interface{}{"interval": 30},
		},
	}
}

// createDefaultAutoUpdateConfig creates default auto-update configuration
func (lc *LocalConfiguration) createDefaultAutoUpdateConfig() *AutoUpdateConfig {
	return &AutoUpdateConfig{
		Enabled:     false,
		IncludeBeta: false,
		URL:         "https://eu-west-1.intake.senhub.io/releases",
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

	// Certs live next to the config, not under the install-time cwd:
	// a hardened (non-root) unit runs with ProtectHome=true, and certs
	// generated under /root or a user home are unreadable by the
	// service user — the HTTPS strategy then fails at start (#280
	// review finding). The config directory is the one place the
	// service user is guaranteed to read.
	certsDir := filepath.Join(filepath.Dir(lc.configPath), "certs")
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
