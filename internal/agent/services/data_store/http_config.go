// senhub-agent/internal/agent/services/data_store/http_config.go
package data_store

import (
	"fmt"
	"io/ioutil"
	"net/http"
	"strconv"
	"strings"
	"sync"

	"gopkg.in/yaml.v2"
	"senhub-agent.go/internal/agent/services/configuration"
	"senhub-agent.go/internal/agent/services/logger"
)

// ConfigurationManager handles all configuration management for HTTP strategy
type ConfigurationManager struct {
	logger             *logger.ModuleLogger
	agentConfig        configuration.AgentConfiguration
	params             map[string]interface{}
	enabledEndpoints   map[string]bool
	nagiosConfig       *NagiosConfig
	nagiosConfigMu     sync.RWMutex
	tlsEnabled         bool
	tlsMinVersion      string
	port               int
	bindAddress        string
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

// Nagios Configuration Types

// NagiosConfig represents the main Nagios configuration structure
type NagiosConfig struct {
	Version     string        `yaml:"version"`
	Description string        `yaml:"description"`
	Checks      []NagiosCheck `yaml:"checks"`
}

// NagiosCheck represents a single Nagios check definition
type NagiosCheck struct {
	Name        string            `yaml:"name"`
	Description string            `yaml:"description"`
	ProbeFilter string            `yaml:"probe_filter,omitempty"`
	TagFilters  []NagiosTagFilter `yaml:"tag_filters,omitempty"`
	Metrics     []NagiosMetric    `yaml:"metrics"`
}

// NagiosTagFilter represents tag filtering criteria
type NagiosTagFilter struct {
	Key      string   `yaml:"key"`
	Values   []string `yaml:"values,omitempty"`
	Operator string   `yaml:"operator"` // "in", "not_in", "equals", "not_equals", "exists"
}

// NagiosMetric represents a metric definition within a check
type NagiosMetric struct {
	Channel               string                `yaml:"channel"`
	Aggregation          string                `yaml:"aggregation,omitempty"` // "average", "max", "min", "sum", "none"
	Warning              string                `yaml:"warning"`
	Critical             string                `yaml:"critical"`
	Unit                 string                `yaml:"unit,omitempty"`
	Invert               bool                  `yaml:"invert,omitempty"`
	TagContext           string                `yaml:"tag_context,omitempty"`
	TagSpecificThresholds []NagiosTagThreshold `yaml:"tag_specific_thresholds,omitempty"`
	Description          string                `yaml:"description,omitempty"`
}

// NagiosTagThreshold represents tag-specific threshold overrides
type NagiosTagThreshold struct {
	Tags     map[string]string `yaml:"tags"`
	Warning  string            `yaml:"warning"`
	Critical string            `yaml:"critical"`
}

// Nagios Request/Response Types

// NagiosResponse represents the response structure for Nagios checks
type NagiosResponse struct {
	Status     int    `json:"status"`      // 0=OK, 1=WARNING, 2=CRITICAL, 3=UNKNOWN
	StatusText string `json:"status_text"` // "OK", "WARNING", "CRITICAL", "UNKNOWN"
	Message    string `json:"message"`     // Human readable message
	PerfData   string `json:"perfdata"`    // Performance data string
}

// NagiosRequest represents incoming Nagios check requests
type NagiosRequest struct {
	CheckName string                 `json:"check_name,omitempty"`
	Probe     string                 `json:"probe,omitempty"`
	Config    map[string]interface{} `json:"config,omitempty"`
	Overrides NagiosOverrides        `json:"overrides,omitempty"`
}

// NagiosOverrides represents runtime threshold and filter overrides
type NagiosOverrides struct {
	Warning    string            `json:"warning,omitempty"`
	Critical   string            `json:"critical,omitempty"`
	TagFilters map[string]string `json:"tag_filters,omitempty"`
}

// Nagios Discovery Types

// NagiosCheckInfo represents check information for discovery endpoints
type NagiosCheckInfo struct {
	Name        string             `json:"name"`
	Description string             `json:"description"`
	ProbeFilter string             `json:"probe_filter,omitempty"`
	MetricCount int                `json:"metric_count"`
	TagFilters  []NagiosTagFilter  `json:"tag_filters,omitempty"`
	Metrics     []NagiosMetricInfo `json:"metrics"`
}

// NagiosMetricInfo represents metric information for discovery
type NagiosMetricInfo struct {
	Channel     string `json:"channel"`
	Aggregation string `json:"aggregation,omitempty"`
	Warning     string `json:"warning"`
	Critical    string `json:"critical"`
	Unit        string `json:"unit,omitempty"`
	Invert      bool   `json:"invert,omitempty"`
	TagContext  string `json:"tag_context,omitempty"`
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

	// If no endpoints specified, default to senhub only (raw format)
	if len(cm.enabledEndpoints) == 0 {
		cm.enabledEndpoints["senhub"] = true
	}

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
				"prtg": true, "senhub": true, "nagios": true, 
				"zabbix": true, "prometheus": true, "web": true,
			}
			
			for _, endpoint := range endpointsList {
				if endpointStr, ok := endpoint.(string); ok {
					if !validEndpoints[endpointStr] {
						return fmt.Errorf("invalid endpoint: %s. Valid endpoints: prtg, senhub, nagios, zabbix, prometheus, web", endpointStr)
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

// GetAgentConfig returns the agent configuration
func (cm *ConfigurationManager) GetAgentConfig() configuration.AgentConfiguration {
	return cm.agentConfig
}

// Nagios Configuration Management

// LoadNagiosConfig loads the Nagios configuration from YAML file with thread safety
func (cm *ConfigurationManager) LoadNagiosConfig() *NagiosConfig {
	cm.nagiosConfigMu.RLock()
	if cm.nagiosConfig != nil {
		cm.nagiosConfigMu.RUnlock()
		return cm.nagiosConfig
	}
	cm.nagiosConfigMu.RUnlock()

	cm.nagiosConfigMu.Lock()
	defer cm.nagiosConfigMu.Unlock()

	// Double-check pattern
	if cm.nagiosConfig != nil {
		return cm.nagiosConfig
	}

	// Try to load from file first
	configPath := "config/nagios.yaml"
	if config, err := cm.loadNagiosConfigFromFile(configPath); err == nil {
		cm.logger.Info().Str("path", configPath).Msg("Loaded Nagios configuration from file")
		cm.nagiosConfig = config
		return config
	}

	// Fall back to default configuration
	cm.logger.Info().Msg("Using fallback Nagios configuration")
	cm.nagiosConfig = cm.createFallbackNagiosConfig()
	return cm.nagiosConfig
}

// loadNagiosConfigFromFile loads Nagios configuration from a YAML file
func (cm *ConfigurationManager) loadNagiosConfigFromFile(configPath string) (*NagiosConfig, error) {
	data, err := ioutil.ReadFile(configPath)
	if err != nil {
		return nil, err
	}

	var config NagiosConfig
	if err := yaml.Unmarshal(data, &config); err != nil {
		return nil, fmt.Errorf("failed to parse Nagios YAML: %w", err)
	}

	// Validate configuration
	if err := cm.validateNagiosConfig(&config); err != nil {
		return nil, fmt.Errorf("invalid Nagios configuration: %w", err)
	}

	return &config, nil
}

// validateNagiosConfig validates the loaded Nagios configuration
func (cm *ConfigurationManager) validateNagiosConfig(config *NagiosConfig) error {
	if config.Version == "" {
		return fmt.Errorf("version is required")
	}

	if len(config.Checks) == 0 {
		return fmt.Errorf("at least one check must be defined")
	}

	for i, check := range config.Checks {
		if check.Name == "" {
			return fmt.Errorf("check %d: name is required", i)
		}

		if len(check.Metrics) == 0 {
			return fmt.Errorf("check %s: at least one metric must be defined", check.Name)
		}

		for j, metric := range check.Metrics {
			if metric.Channel == "" {
				return fmt.Errorf("check %s, metric %d: channel is required", check.Name, j)
			}
			if metric.Warning == "" {
				return fmt.Errorf("check %s, metric %s: warning threshold is required", check.Name, metric.Channel)
			}
			if metric.Critical == "" {
				return fmt.Errorf("check %s, metric %s: critical threshold is required", check.Name, metric.Channel)
			}
		}
	}

	return nil
}

// createFallbackNagiosConfig creates a basic fallback configuration
func (cm *ConfigurationManager) createFallbackNagiosConfig() *NagiosConfig {
	return &NagiosConfig{
		Version:     "1.0.0",
		Description: "Fallback Nagios configuration",
		Checks: []NagiosCheck{
			{
				Name:        "system_health",
				Description: "Basic system health check",
				Metrics: []NagiosMetric{
					{
						Channel:     "cpu_usage_percent",
						Aggregation: "average",
						Warning:     "80",
						Critical:    "90",
						Unit:        "%",
					},
					{
						Channel:     "memory_used_percent",
						Aggregation: "average",
						Warning:     "85",
						Critical:    "95",
						Unit:        "%",
					},
				},
			},
			{
				Name:        "network_health",
				Description: "Network interface health check",
				Metrics: []NagiosMetric{
					{
						Channel:     "network_bytes_total",
						Aggregation: "sum",
						Warning:     "1000000000",  // 1GB
						Critical:    "10000000000", // 10GB
						Unit:        "bytes",
					},
				},
			},
		},
	}
}

// FindNagiosCheck finds a check by name in the configuration
func (cm *ConfigurationManager) FindNagiosCheck(config *NagiosConfig, checkName string) *NagiosCheck {
	for _, check := range config.Checks {
		if check.Name == checkName {
			return &check
		}
	}
	return nil
}

// Nagios Request Processing

// ParseNagiosOverrides parses query parameters for Nagios threshold overrides
func (cm *ConfigurationManager) ParseNagiosOverrides(r *http.Request) NagiosOverrides {
	query := r.URL.Query()
	
	overrides := NagiosOverrides{
		TagFilters: make(map[string]string),
	}

	if warning := query.Get("warning"); warning != "" {
		overrides.Warning = warning
	}
	
	if critical := query.Get("critical"); critical != "" {
		overrides.Critical = critical
	}
	
	// Parse tag filters from query parameters
	for key, values := range query {
		if strings.HasPrefix(key, "tag_") {
			tagName := strings.TrimPrefix(key, "tag_")
			if len(values) > 0 {
				overrides.TagFilters[tagName] = values[0]
			}
		}
	}

	return overrides
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

// ReloadNagiosConfig forces a reload of the Nagios configuration
func (cm *ConfigurationManager) ReloadNagiosConfig() error {
	cm.nagiosConfigMu.Lock()
	defer cm.nagiosConfigMu.Unlock()

	// Clear cached config to force reload
	cm.nagiosConfig = nil
	
	cm.logger.Info().Msg("Nagios configuration cache cleared, will reload on next access")
	return nil
}

// GetConfigurationSummary returns a summary of the current configuration
func (cm *ConfigurationManager) GetConfigurationSummary() map[string]interface{} {
	summary := map[string]interface{}{
		"port":                cm.port,
		"bind_address":        cm.bindAddress,
		"enabled_endpoints":   cm.enabledEndpoints,
		"tls_enabled":         cm.tlsEnabled,
		"tls_min_version":     cm.tlsMinVersion,
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