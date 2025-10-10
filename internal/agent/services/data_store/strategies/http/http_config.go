// senhub-agent/internal/agent/services/data_store/http_config.go
package http

import (
	"fmt"
	"strconv"
	"sync"

	"senhub-agent.go/internal/agent/services/configuration"
	"senhub-agent.go/internal/agent/services/logger"
)

// ConfigurationManager handles all configuration management for HTTP strategy
type ConfigurationManager struct {
	logger           *logger.ModuleLogger
	agentConfig      configuration.AgentConfiguration
	params           map[string]interface{}
	enabledEndpoints map[string]bool
	nagiosConfig     *NagiosConfig
	nagiosConfigMu   sync.RWMutex
	tlsEnabled       bool
	tlsMinVersion    string
	tlsCertFile      string
	tlsKeyFile       string
	port             int
	bindAddress      string
}

// NewConfigurationManager creates a new configuration manager
func NewConfigurationManager(agentConfig configuration.AgentConfiguration, params map[string]interface{}, logger *logger.ModuleLogger) *ConfigurationManager {
	cm := &ConfigurationManager{
		logger:           logger,
		agentConfig:      agentConfig,
		params:           params,
		enabledEndpoints: make(map[string]bool),
		port:             8080,      // Default port
		bindAddress:      "0.0.0.0", // Default to all interfaces
		tlsMinVersion:    "1.2",     // Default TLS version
	}

	// Initialize configuration from params
	cm.loadConfiguration()

	return cm
}

// Configuration Loading and Validation

// loadConfiguration loads configuration from params
func (cm *ConfigurationManager) loadConfiguration() {
	// Override port if specified in params
	if portValue, exists := cm.params["port"]; exists {
		switch v := portValue.(type) {
		case float64:
			cm.port = int(v)
		case int:
			cm.port = v
		case int64:
			cm.port = int(v)
		}
	}

	// Override bind address if specified in params
	if bindValue, exists := cm.params["bind_address"]; exists {
		if bindAddr, ok := bindValue.(string); ok {
			cm.bindAddress = bindAddr
		}
	}

	// Load endpoints configuration
	if endpointsParam, exists := cm.params["endpoints"]; exists {
		if endpointsList, ok := endpointsParam.([]interface{}); ok {
			for _, endpoint := range endpointsList {
				if endpointStr, ok := endpoint.(string); ok {
					cm.enabledEndpoints[endpointStr] = true
				}
			}
		}
	}

	// If no endpoints specified, default to no endpoints enabled
	// User must explicitly configure endpoints

	// Parse TLS configuration
	if tlsParam, exists := cm.params["tls"]; exists {
		if tlsConfig, ok := tlsParam.(map[string]interface{}); ok {
			// TLS enabled
			if enabled, exists := tlsConfig["enabled"]; exists {
				if enabledBool, ok := enabled.(bool); ok {
					cm.tlsEnabled = enabledBool
				}
			}

			// Min TLS version (with default)
			if minVersion, exists := tlsConfig["min_tls_version"]; exists {
				if minVersionStr, ok := minVersion.(string); ok {
					cm.tlsMinVersion = minVersionStr
				}
			} else if cm.tlsEnabled {
				cm.tlsMinVersion = "1.2"
			}

			// Certificate file paths
			if certFile, exists := tlsConfig["cert_file"]; exists {
				if certFileStr, ok := certFile.(string); ok {
					cm.tlsCertFile = certFileStr
				}
			}
			if keyFile, exists := tlsConfig["key_file"]; exists {
				if keyFileStr, ok := keyFile.(string); ok {
					cm.tlsKeyFile = keyFileStr
				}
			}
		}
	}
}

// ValidateConfigParams validates the provided configuration parameters
func (cm *ConfigurationManager) ValidateConfigParams(params configuration.StorageConfigParams) error {
	// Validate port if provided
	if portValue, exists := params["port"]; exists {
		var port int
		switch v := portValue.(type) {
		case int:
			port = v
		case int64:
			port = int(v)
		case float64:
			// Accept float64 only if it's a whole number (for JSON compatibility)
			if v == float64(int(v)) {
				port = int(v)
			} else {
				return fmt.Errorf("port must be an integer, got: %v", v)
			}
		default:
			return fmt.Errorf("port must be an integer, got: %T", v)
		}

		if port < 1 || port > 65535 {
			return fmt.Errorf("port must be between 1 and 65535, got: %d", port)
		}
	}

	// Validate bind_address if provided
	if bindValue, exists := params["bind_address"]; exists {
		if _, ok := bindValue.(string); !ok {
			return fmt.Errorf("bind_address must be a string, got: %T", bindValue)
		}
	}

	// Validate endpoints if provided
	if endpointsValue, exists := params["endpoints"]; exists {
		if endpointsList, ok := endpointsValue.([]interface{}); ok {
			validEndpoints := map[string]bool{
				"prtg": true, "nagios": true,
				"zabbix": true, "prometheus": true, "web": true,
			}

			for _, endpoint := range endpointsList {
				if endpointStr, ok := endpoint.(string); ok {
					if !validEndpoints[endpointStr] {
						return fmt.Errorf("invalid endpoint: %s. Valid endpoints: prtg, nagios, zabbix, prometheus, web", endpointStr)
					}
				} else {
					return fmt.Errorf("endpoint must be a string, got: %T", endpoint)
				}
			}
		} else {
			return fmt.Errorf("endpoints must be an array, got: %T", endpointsValue)
		}
	}

	return nil
}

// Getters

// GetPort returns the configured HTTP port
func (cm *ConfigurationManager) GetPort() int {
	return cm.port
}

// GetBindAddress returns the configured bind address
func (cm *ConfigurationManager) GetBindAddress() string {
	return cm.bindAddress
}

// GetEnabledEndpoints returns the map of enabled endpoints
func (cm *ConfigurationManager) GetEnabledEndpoints() map[string]bool {
	return cm.enabledEndpoints
}

// IsTLSEnabled returns whether TLS is enabled
func (cm *ConfigurationManager) IsTLSEnabled() bool {
	return cm.tlsEnabled
}

// GetTLSMinVersion returns the minimum TLS version
func (cm *ConfigurationManager) GetTLSMinVersion() string {
	return cm.tlsMinVersion
}

// GetTLSCertFile returns the TLS certificate file path
func (cm *ConfigurationManager) GetTLSCertFile() string {
	return cm.tlsCertFile
}

// GetTLSKeyFile returns the TLS private key file path
func (cm *ConfigurationManager) GetTLSKeyFile() string {
	return cm.tlsKeyFile
}

// GetAgentConfig returns the agent configuration
func (cm *ConfigurationManager) GetAgentConfig() configuration.AgentConfiguration {
	return cm.agentConfig
}

// Configuration Updates

// UpdateConfiguration updates the configuration with new parameters
func (cm *ConfigurationManager) UpdateConfiguration(newParams map[string]interface{}) error {
	// Validate new parameters first
	if err := cm.ValidateConfigParams(newParams); err != nil {
		return err
	}

	// Update parameters
	for key, value := range newParams {
		cm.params[key] = value
	}

	// Reload configuration
	cm.loadConfiguration()

	cm.logger.Info().Msg("Configuration updated successfully")
	return nil
}

// GetConfigurationSummary returns a summary of the current configuration
func (cm *ConfigurationManager) GetConfigurationSummary() map[string]interface{} {
	summary := map[string]interface{}{
		"port":                 cm.port,
		"bind_address":         cm.bindAddress,
		"enabled_endpoints":    cm.enabledEndpoints,
		"tls_enabled":          cm.tlsEnabled,
		"tls_min_version":      cm.tlsMinVersion,
		"nagios_config_loaded": cm.nagiosConfig != nil,
	}

	if cm.nagiosConfig != nil {
		summary["nagios_checks_count"] = len(cm.nagiosConfig.Checks)
		summary["nagios_version"] = cm.nagiosConfig.Version
	}

	return summary
}

// Configuration Utilities

// IsEndpointEnabled checks if a specific endpoint is enabled
func (cm *ConfigurationManager) IsEndpointEnabled(endpoint string) bool {
	return cm.enabledEndpoints[endpoint]
}

// GetPortAsString returns the port as a string for server configuration
func (cm *ConfigurationManager) GetPortAsString() string {
	return strconv.Itoa(cm.port)
}

// GetEnabledEndpointsList returns a list of enabled endpoint names
func (cm *ConfigurationManager) GetEnabledEndpointsList() []string {
	var endpoints []string
	for endpoint, enabled := range cm.enabledEndpoints {
		if enabled {
			endpoints = append(endpoints, endpoint)
		}
	}
	return endpoints
}
