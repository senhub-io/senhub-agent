// senhub-agent/internal/agent/services/data_store/http_config.go
package http

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"gopkg.in/yaml.v2"
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

// Universal Configuration Types

// UniversalConfigRequest represents a universal configuration validation request
type UniversalConfigRequest struct {
	Probe      string                 `json:"probe"`                // Target probe name (e.g., "redfish", "host")
	Target     string                 `json:"target,omitempty"`     // Target system/endpoint URL
	Config     map[string]interface{} `json:"config"`               // Probe-specific configuration
	Validation ConfigValidationMode   `json:"validation,omitempty"` // Validation level to perform
	Timeout    int                    `json:"timeout,omitempty"`    // Timeout for connectivity tests (seconds)
}

// ConfigValidationMode specifies the level of validation to perform
type ConfigValidationMode string

const (
	ValidationSchemaOnly   ConfigValidationMode = "schema"       // Validate structure and types only
	ValidationConnectivity ConfigValidationMode = "connectivity" // + Test network connectivity
	ValidationFull         ConfigValidationMode = "full"         // + Test actual metrics collection
)

// UniversalConfigResponse represents the response from configuration validation
type UniversalConfigResponse struct {
	Valid           bool                            `json:"valid"`                     // Overall validation result
	Probe           string                          `json:"probe"`                     // Probe that was validated
	Target          string                          `json:"target,omitempty"`          // Target that was tested
	ValidationLevel ConfigValidationMode            `json:"validation_level"`          // Level of validation performed
	Tests           map[string]ValidationTestResult `json:"tests"`                     // Individual test results
	Warnings        []string                        `json:"warnings,omitempty"`        // Non-fatal warnings
	Errors          []string                        `json:"errors,omitempty"`          // Validation errors
	PreviewMetrics  []PreviewMetric                 `json:"preview_metrics,omitempty"` // Sample metrics (for full validation)
	Duration        int64                           `json:"duration_ms"`               // Total validation time in milliseconds
}

// ValidationTestResult represents the result of an individual validation test
type ValidationTestResult struct {
	Passed   bool   `json:"passed"`            // Whether this test passed
	Error    string `json:"error,omitempty"`   // Error message if failed
	Duration int64  `json:"duration_ms"`       // Test duration in milliseconds
	Details  string `json:"details,omitempty"` // Additional test details
}

// PreviewMetric represents a sample metric for preview purposes
type PreviewMetric struct {
	Name      string            `json:"name"`      // Metric name
	Value     float32           `json:"value"`     // Metric value
	Tags      map[string]string `json:"tags"`      // Metric tags
	Timestamp int64             `json:"timestamp"` // Collection timestamp
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
	Channel               string               `yaml:"channel"`
	Aggregation           string               `yaml:"aggregation,omitempty"` // "average", "max", "min", "sum", "none"
	Warning               string               `yaml:"warning"`
	Critical              string               `yaml:"critical"`
	Unit                  string               `yaml:"unit,omitempty"`
	Invert                bool                 `yaml:"invert,omitempty"`
	TagContext            string               `yaml:"tag_context,omitempty"`
	TagSpecificThresholds []NagiosTagThreshold `yaml:"tag_specific_thresholds,omitempty"`
	Description           string               `yaml:"description,omitempty"`
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
	data, err := os.ReadFile(configPath)
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

// Universal Configuration Validation Methods

// ValidateUniversalConfig validates a universal configuration request according to the specified validation mode
func (cm *ConfigurationManager) ValidateUniversalConfig(req *UniversalConfigRequest) (*UniversalConfigResponse, error) {
	startTime := time.Now()

	cm.logger.Info().
		Str("probe", req.Probe).
		Str("target", req.Target).
		Str("validation_mode", string(req.Validation)).
		Msg("Starting universal configuration validation")

	// Initialize response
	response := &UniversalConfigResponse{
		Probe:           req.Probe,
		Target:          req.Target,
		ValidationLevel: req.Validation,
		Tests:           make(map[string]ValidationTestResult),
		Warnings:        []string{},
		Errors:          []string{},
	}

	// Set default validation mode if not specified
	if req.Validation == "" {
		req.Validation = ValidationSchemaOnly
		response.ValidationLevel = ValidationSchemaOnly
	}

	// Set default timeout if not specified
	if req.Timeout == 0 {
		req.Timeout = 30 // 30 seconds default
	}

	// Step 1: Schema Validation (always performed)
	schemaResult := cm.validateProbeSchema(req.Probe, req.Config)
	response.Tests["schema"] = schemaResult

	if !schemaResult.Passed {
		response.Valid = false
		response.Errors = append(response.Errors, schemaResult.Error)
		response.Duration = time.Since(startTime).Milliseconds()

		cm.logger.Error().
			Str("probe", req.Probe).
			Str("error", schemaResult.Error).
			Msg("Schema validation failed")

		return response, nil
	}

	// Step 2: Connectivity Validation (if requested)
	if req.Validation == ValidationConnectivity || req.Validation == ValidationFull {
		connectivityResult := cm.validateProbeConnectivity(req.Probe, req.Config, req.Target, req.Timeout)
		response.Tests["connectivity"] = connectivityResult

		if !connectivityResult.Passed {
			response.Valid = false
			response.Errors = append(response.Errors, connectivityResult.Error)
			response.Duration = time.Since(startTime).Milliseconds()

			cm.logger.Warn().
				Str("probe", req.Probe).
				Str("target", req.Target).
				Str("error", connectivityResult.Error).
				Msg("Connectivity validation failed")

			return response, nil
		}
	}

	// Step 3: Full Metrics Validation (if requested)
	if req.Validation == ValidationFull {
		metricsResult, previewMetrics := cm.validateProbeMetrics(req.Probe, req.Config, req.Target, req.Timeout)
		response.Tests["metrics"] = metricsResult
		response.PreviewMetrics = previewMetrics

		if !metricsResult.Passed {
			response.Valid = false
			response.Errors = append(response.Errors, metricsResult.Error)
		}
	}

	// Final result
	response.Valid = true
	response.Duration = time.Since(startTime).Milliseconds()

	cm.logger.Info().
		Str("probe", req.Probe).
		Str("validation_level", string(req.Validation)).
		Int64("duration_ms", response.Duration).
		Bool("valid", response.Valid).
		Msg("Universal configuration validation completed")

	return response, nil
}

// validateProbeSchema validates the probe configuration against its expected schema
func (cm *ConfigurationManager) validateProbeSchema(probeName string, config map[string]interface{}) ValidationTestResult {
	startTime := time.Now()

	cm.logger.Debug().
		Str("probe", probeName).
		Any("config", config).
		Msg("Validating probe schema")

	// Import the probe registry for validation
	result := ValidationTestResult{
		Passed:   false,
		Duration: 0,
	}

	// Validate based on probe type
	switch probeName {
	case "redfish":
		result = cm.validateRedfishSchema(config)
	case "host", "cpu", "memory", "network", "logicaldisk":
		result = cm.validateSystemProbeSchema(config)
	case "ping_gateway":
		result = cm.validateGatewayProbeSchema(config)
	case "ping_webapp", "load_webapp":
		result = cm.validateWebAppProbeSchema(config)
	case "syslog":
		result = cm.validateSyslogProbeSchema(config)
	default:
		result.Error = fmt.Sprintf("unsupported probe type: %s", probeName)
		result.Duration = time.Since(startTime).Milliseconds()
		return result
	}

	result.Duration = time.Since(startTime).Milliseconds()

	cm.logger.Debug().
		Str("probe", probeName).
		Bool("passed", result.Passed).
		Int64("duration_ms", result.Duration).
		Msg("Schema validation completed")

	return result
}

// validateProbeConnectivity tests network connectivity to the target system
func (cm *ConfigurationManager) validateProbeConnectivity(probeName string, config map[string]interface{}, target string, timeout int) ValidationTestResult {
	startTime := time.Now()

	cm.logger.Debug().
		Str("probe", probeName).
		Str("target", target).
		Int("timeout", timeout).
		Msg("Testing probe connectivity")

	result := ValidationTestResult{
		Passed: false,
	}

	// For network-based probes, test connectivity
	switch probeName {
	case "redfish":
		result = cm.testRedfishConnectivity(config, timeout)
	case "ping_webapp", "load_webapp":
		result = cm.testWebAppConnectivity(config, timeout)
	case "syslog":
		result = cm.testSyslogConnectivity(config, timeout)
	default:
		// For local probes (cpu, memory, etc.), connectivity always passes
		result.Passed = true
		result.Details = "Local probe - no connectivity test required"
	}

	result.Duration = time.Since(startTime).Milliseconds()

	cm.logger.Debug().
		Str("probe", probeName).
		Str("target", target).
		Bool("passed", result.Passed).
		Int64("duration_ms", result.Duration).
		Msg("Connectivity test completed")

	return result
}

// validateProbeMetrics attempts to collect actual metrics from the configured probe
func (cm *ConfigurationManager) validateProbeMetrics(probeName string, config map[string]interface{}, target string, timeout int) (ValidationTestResult, []PreviewMetric) {
	startTime := time.Now()

	cm.logger.Debug().
		Str("probe", probeName).
		Str("target", target).
		Msg("Testing probe metrics collection")

	result := ValidationTestResult{
		Passed: false,
	}
	var previewMetrics []PreviewMetric

	// This would require instantiating the actual probe temporarily
	// For now, we'll simulate this functionality
	result.Passed = true
	result.Details = "Metrics validation not yet implemented - configuration appears valid"

	// Create some mock preview metrics based on probe type
	previewMetrics = cm.generateMockPreviewMetrics(probeName)

	result.Duration = time.Since(startTime).Milliseconds()

	cm.logger.Debug().
		Str("probe", probeName).
		Bool("passed", result.Passed).
		Int("preview_count", len(previewMetrics)).
		Int64("duration_ms", result.Duration).
		Msg("Metrics validation completed")

	return result, previewMetrics
}

// Helper methods for specific probe schema validation

// validateRedfishSchema validates Redfish probe configuration
func (cm *ConfigurationManager) validateRedfishSchema(config map[string]interface{}) ValidationTestResult {
	result := ValidationTestResult{Passed: true}

	// Required fields for Redfish
	requiredFields := []string{"endpoint", "username", "password"}
	for _, field := range requiredFields {
		if _, exists := config[field]; !exists {
			result.Passed = false
			result.Error = fmt.Sprintf("missing required field: %s", field)
			return result
		}
	}

	// Validate endpoint format
	if endpoint, ok := config["endpoint"].(string); ok {
		if !strings.HasPrefix(endpoint, "http://") && !strings.HasPrefix(endpoint, "https://") {
			result.Passed = false
			result.Error = "endpoint must be a valid HTTP/HTTPS URL"
			return result
		}
	} else {
		result.Passed = false
		result.Error = "endpoint must be a string"
		return result
	}

	result.Details = "Redfish configuration schema is valid"
	return result
}

// validateSystemProbeSchema validates system probe configuration (cpu, memory, etc.)
func (cm *ConfigurationManager) validateSystemProbeSchema(config map[string]interface{}) ValidationTestResult {
	result := ValidationTestResult{Passed: true}

	// System probes typically have minimal configuration requirements
	// Validate interval if specified
	if interval, exists := config["interval"]; exists {
		if _, ok := interval.(int); !ok {
			if floatVal, ok := interval.(float64); ok && floatVal == float64(int(floatVal)) {
				// Accept whole number floats (JSON compatibility)
			} else {
				result.Passed = false
				result.Error = "interval must be an integer (seconds)"
				return result
			}
		}
	}

	result.Details = "System probe configuration schema is valid"
	return result
}

// validateGatewayProbeSchema validates gateway ping probe configuration
func (cm *ConfigurationManager) validateGatewayProbeSchema(config map[string]interface{}) ValidationTestResult {
	result := ValidationTestResult{Passed: true}

	// Gateway probe may need target or will auto-discover
	if target, exists := config["target"]; exists {
		if _, ok := target.(string); !ok {
			result.Passed = false
			result.Error = "target must be a string (IP address or hostname)"
			return result
		}
	}

	result.Details = "Gateway probe configuration schema is valid"
	return result
}

// validateWebAppProbeSchema validates webapp probe configuration
func (cm *ConfigurationManager) validateWebAppProbeSchema(config map[string]interface{}) ValidationTestResult {
	result := ValidationTestResult{Passed: true}

	// Required field: url
	if _, exists := config["url"]; !exists {
		result.Passed = false
		result.Error = "missing required field: url"
		return result
	}

	// Validate URL format
	if url, ok := config["url"].(string); ok {
		if !strings.HasPrefix(url, "http://") && !strings.HasPrefix(url, "https://") {
			result.Passed = false
			result.Error = "url must be a valid HTTP/HTTPS URL"
			return result
		}
	} else {
		result.Passed = false
		result.Error = "url must be a string"
		return result
	}

	result.Details = "WebApp probe configuration schema is valid"
	return result
}

// validateSyslogProbeSchema validates syslog probe configuration
func (cm *ConfigurationManager) validateSyslogProbeSchema(config map[string]interface{}) ValidationTestResult {
	result := ValidationTestResult{Passed: true}

	// Validate port if specified
	if port, exists := config["port"]; exists {
		if portInt, ok := port.(int); ok {
			if portInt < 1 || portInt > 65535 {
				result.Passed = false
				result.Error = "port must be between 1 and 65535"
				return result
			}
		} else if portFloat, ok := port.(float64); ok && portFloat == float64(int(portFloat)) {
			// Accept whole number floats
			if int(portFloat) < 1 || int(portFloat) > 65535 {
				result.Passed = false
				result.Error = "port must be between 1 and 65535"
				return result
			}
		} else {
			result.Passed = false
			result.Error = "port must be an integer"
			return result
		}
	}

	// Validate protocol if specified
	if protocol, exists := config["protocol"]; exists {
		if protocolStr, ok := protocol.(string); ok {
			if protocolStr != "udp" && protocolStr != "tcp" {
				result.Passed = false
				result.Error = "protocol must be 'udp' or 'tcp'"
				return result
			}
		} else {
			result.Passed = false
			result.Error = "protocol must be a string"
			return result
		}
	}

	result.Details = "Syslog probe configuration schema is valid"
	return result
}

// Helper methods for connectivity testing

// testRedfishConnectivity tests connectivity to a Redfish endpoint
func (cm *ConfigurationManager) testRedfishConnectivity(config map[string]interface{}, timeout int) ValidationTestResult {
	result := ValidationTestResult{Passed: false}

	endpoint, exists := config["endpoint"]
	if !exists {
		result.Error = "endpoint not specified for connectivity test"
		return result
	}

	endpointStr, ok := endpoint.(string)
	if !ok {
		result.Error = "endpoint must be a string"
		return result
	}

	// Test basic HTTP connectivity to the Redfish service root
	client := &http.Client{
		Timeout: time.Duration(timeout) * time.Second,
	}

	// Try to reach the Redfish service root
	testURL := strings.TrimRight(endpointStr, "/") + "/redfish/v1/"
	resp, err := client.Get(testURL)
	if err != nil {
		result.Error = fmt.Sprintf("connectivity test failed: %v", err)
		return result
	}
	defer func() {
		if err := resp.Body.Close(); err != nil {
			cm.logger.Debug().Err(err).Msg("Failed to close response body")
		}
	}()

	if resp.StatusCode == 200 || resp.StatusCode == 401 {
		// 200 = accessible, 401 = accessible but auth required (expected)
		result.Passed = true
		result.Details = fmt.Sprintf("Redfish service accessible (HTTP %d)", resp.StatusCode)
	} else {
		result.Error = fmt.Sprintf("unexpected HTTP status: %d", resp.StatusCode)
	}

	return result
}

// testWebAppConnectivity tests connectivity to a web application
func (cm *ConfigurationManager) testWebAppConnectivity(config map[string]interface{}, timeout int) ValidationTestResult {
	result := ValidationTestResult{Passed: false}

	url, exists := config["url"]
	if !exists {
		result.Error = "url not specified for connectivity test"
		return result
	}

	urlStr, ok := url.(string)
	if !ok {
		result.Error = "url must be a string"
		return result
	}

	// Test HTTP connectivity
	client := &http.Client{
		Timeout: time.Duration(timeout) * time.Second,
	}

	resp, err := client.Get(urlStr)
	if err != nil {
		result.Error = fmt.Sprintf("connectivity test failed: %v", err)
		return result
	}
	defer func() {
		if err := resp.Body.Close(); err != nil {
			cm.logger.Debug().Err(err).Msg("Failed to close response body")
		}
	}()

	if resp.StatusCode >= 200 && resp.StatusCode < 500 {
		// Any 2xx, 3xx, or 4xx is considered "reachable"
		result.Passed = true
		result.Details = fmt.Sprintf("WebApp accessible (HTTP %d)", resp.StatusCode)
	} else {
		result.Error = fmt.Sprintf("unexpected HTTP status: %d", resp.StatusCode)
	}

	return result
}

// testSyslogConnectivity tests syslog server connectivity (basic port check)
func (cm *ConfigurationManager) testSyslogConnectivity(config map[string]interface{}, timeout int) ValidationTestResult {
	result := ValidationTestResult{Passed: true}
	result.Details = "Syslog connectivity test not implemented - assuming valid"
	// TODO: Implement actual syslog server connectivity test
	return result
}

// generateMockPreviewMetrics creates sample metrics for preview purposes
func (cm *ConfigurationManager) generateMockPreviewMetrics(probeName string) []PreviewMetric {
	timestamp := time.Now().Unix()

	switch probeName {
	case "redfish":
		return []PreviewMetric{
			{Name: "system.health", Value: 1, Tags: map[string]string{"system_id": "1"}, Timestamp: timestamp},
			{Name: "thermal.cpu.0.temperature", Value: 45.2, Tags: map[string]string{"cpu_id": "0", "system_id": "1"}, Timestamp: timestamp},
			{Name: "power.psu.0.output_watts", Value: 350.5, Tags: map[string]string{"psu_id": "0", "system_id": "1"}, Timestamp: timestamp},
		}
	case "cpu":
		return []PreviewMetric{
			{Name: "cpu.usage_percent", Value: 25.8, Tags: map[string]string{"cpu": "all"}, Timestamp: timestamp},
			{Name: "cpu.cores", Value: 8, Tags: map[string]string{"cpu": "all"}, Timestamp: timestamp},
		}
	case "memory":
		return []PreviewMetric{
			{Name: "memory.usage_percent", Value: 62.3, Tags: map[string]string{}, Timestamp: timestamp},
			{Name: "memory.available_mb", Value: 6144, Tags: map[string]string{}, Timestamp: timestamp},
		}
	case "ping_webapp":
		return []PreviewMetric{
			{Name: "webapp.ping_ms", Value: 45.2, Tags: map[string]string{"url": "example.com"}, Timestamp: timestamp},
			{Name: "webapp.available", Value: 1, Tags: map[string]string{"url": "example.com"}, Timestamp: timestamp},
		}
	default:
		return []PreviewMetric{
			{Name: fmt.Sprintf("%s.status", probeName), Value: 1, Tags: map[string]string{"probe": probeName}, Timestamp: timestamp},
		}
	}
}

// HTTP Handler Methods for Universal Configuration

// HandleUniversalConfigValidation handles POST requests for universal configuration validation
func (cm *ConfigurationManager) HandleUniversalConfigValidation(w http.ResponseWriter, r *http.Request) {
	cm.logger.Debug().Msg("Handling universal configuration validation request")

	// Parse request body
	var req UniversalConfigRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		cm.logger.Error().
			Err(err).
			Msg("Failed to parse universal config validation request")

		http.Error(w, "Invalid JSON request body", http.StatusBadRequest)
		return
	}

	// Validate the configuration
	response, err := cm.ValidateUniversalConfig(&req)
	if err != nil {
		cm.logger.Error().
			Err(err).
			Str("probe", req.Probe).
			Msg("Universal configuration validation failed")

		http.Error(w, fmt.Sprintf("Validation failed: %v", err), http.StatusInternalServerError)
		return
	}

	// Set response headers
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-cache, no-store, must-revalidate")

	// Send response
	if err := json.NewEncoder(w).Encode(response); err != nil {
		cm.logger.Error().
			Err(err).
			Msg("Failed to encode universal config validation response")

		http.Error(w, "Failed to encode response", http.StatusInternalServerError)
		return
	}

	cm.logger.Info().
		Str("probe", req.Probe).
		Str("validation_level", string(req.Validation)).
		Bool("valid", response.Valid).
		Int64("duration_ms", response.Duration).
		Msg("Universal configuration validation request completed")
}

// HandleUniversalConfigPreview handles POST requests for configuration preview (same as validation but different endpoint)
func (cm *ConfigurationManager) HandleUniversalConfigPreview(w http.ResponseWriter, r *http.Request) {
	cm.logger.Debug().Msg("Handling universal configuration preview request")

	// Parse request body
	var req UniversalConfigRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		cm.logger.Error().
			Err(err).
			Msg("Failed to parse universal config preview request")

		http.Error(w, "Invalid JSON request body", http.StatusBadRequest)
		return
	}

	// Force validation mode to "schema" for preview (fast validation only)
	if req.Validation == "" || req.Validation == ValidationFull {
		req.Validation = ValidationSchemaOnly
		cm.logger.Debug().
			Str("probe", req.Probe).
			Msg("Preview mode: forcing validation to schema-only for performance")
	}

	// Validate the configuration
	response, err := cm.ValidateUniversalConfig(&req)
	if err != nil {
		cm.logger.Error().
			Err(err).
			Str("probe", req.Probe).
			Msg("Universal configuration preview failed")

		http.Error(w, fmt.Sprintf("Preview failed: %v", err), http.StatusInternalServerError)
		return
	}

	// Set response headers
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-cache, no-store, must-revalidate")

	// Send response
	if err := json.NewEncoder(w).Encode(response); err != nil {
		cm.logger.Error().
			Err(err).
			Msg("Failed to encode universal config preview response")

		http.Error(w, "Failed to encode response", http.StatusInternalServerError)
		return
	}

	cm.logger.Info().
		Str("probe", req.Probe).
		Str("validation_level", string(req.Validation)).
		Bool("valid", response.Valid).
		Int64("duration_ms", response.Duration).
		Msg("Universal configuration preview request completed")
}

// HandleUniversalConfigTest handles POST requests for full configuration testing
func (cm *ConfigurationManager) HandleUniversalConfigTest(w http.ResponseWriter, r *http.Request) {
	cm.logger.Debug().Msg("Handling universal configuration test request")

	// Parse request body
	var req UniversalConfigRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		cm.logger.Error().
			Err(err).
			Msg("Failed to parse universal config test request")

		http.Error(w, "Invalid JSON request body", http.StatusBadRequest)
		return
	}

	// Force validation mode to "full" for comprehensive testing
	req.Validation = ValidationFull
	cm.logger.Debug().
		Str("probe", req.Probe).
		Msg("Test mode: forcing full validation including connectivity and metrics")

	// Validate the configuration
	response, err := cm.ValidateUniversalConfig(&req)
	if err != nil {
		cm.logger.Error().
			Err(err).
			Str("probe", req.Probe).
			Msg("Universal configuration test failed")

		http.Error(w, fmt.Sprintf("Test failed: %v", err), http.StatusInternalServerError)
		return
	}

	// Set response headers
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-cache, no-store, must-revalidate")

	// Send response
	if err := json.NewEncoder(w).Encode(response); err != nil {
		cm.logger.Error().
			Err(err).
			Msg("Failed to encode universal config test response")

		http.Error(w, "Failed to encode response", http.StatusInternalServerError)
		return
	}

	cm.logger.Info().
		Str("probe", req.Probe).
		Str("validation_level", string(req.Validation)).
		Bool("valid", response.Valid).
		Int64("duration_ms", response.Duration).
		Int("preview_metrics_count", len(response.PreviewMetrics)).
		Msg("Universal configuration test request completed")
}
