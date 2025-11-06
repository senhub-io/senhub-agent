// senhub-agent/internal/agent/services/data_store/http_config_nagios.go
package http

import (
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v2"
)

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
	data, err := os.ReadFile(filepath.Clean(configPath)) // #nosec G304 - configPath is hardcoded to "config/nagios.yaml"
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

// ReloadNagiosConfig forces a reload of the Nagios configuration
func (cm *ConfigurationManager) ReloadNagiosConfig() error {
	cm.nagiosConfigMu.Lock()
	defer cm.nagiosConfigMu.Unlock()

	// Clear cached config to force reload
	cm.nagiosConfig = nil

	cm.logger.Info().Msg("Nagios configuration cache cleared, will reload on next access")
	return nil
}
