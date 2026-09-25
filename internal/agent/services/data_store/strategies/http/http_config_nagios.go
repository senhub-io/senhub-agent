// senhub-agent/internal/agent/services/data_store/http_config_nagios.go
package http

import (
	"errors"
	"fmt"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"

	"gopkg.in/yaml.v2"

	"senhub-agent.go/internal/agent/services/data_store/transformers"
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

	// Operator-provided file takes precedence. A file that exists but
	// cannot be used is reported, not silently replaced by the default:
	// the operator would otherwise see the curated checks and never
	// learn why theirs are missing.
	for _, configPath := range cm.nagiosConfigCandidates() {
		config, err := cm.loadNagiosConfigFromFile(configPath)
		if err == nil {
			cm.logger.Info().Str("path", configPath).Msg("Loaded Nagios configuration from file")
			cm.warnUndeclaredNagiosChannels(configPath, config)
			cm.nagiosConfig = config
			return config
		}
		if !errors.Is(err, fs.ErrNotExist) {
			cm.logger.Error().Err(err).Str("path", configPath).Msg("Nagios configuration file ignored, serving the default checks")
		}
	}

	// Default: the curated configuration shipped with the agent
	// (transformers/definitions/nagios.yaml, embedded). Its checks
	// reference channels the probes actually emit; the hardcoded
	// fallback below only covers an unparseable embedded file.
	if config, err := cm.loadEmbeddedNagiosConfig(); err == nil {
		cm.logger.Info().Str("version", config.Version).Msg("Loaded embedded default Nagios configuration")
		cm.nagiosConfig = config
		return config
	} else {
		cm.logger.Warn().Err(err).Msg("Embedded Nagios configuration unusable, using minimal fallback")
	}

	cm.nagiosConfig = cm.createFallbackNagiosConfig()
	return cm.nagiosConfig
}

// nagiosConfigCandidates lists where an operator's nagios.yaml is read
// from, first match wins: next to the agent configuration, then the
// historical config/nagios.yaml under the working directory. The
// working directory is /usr/local/bin for a Linux service and the
// install folder on Windows, so the historical path alone could not be
// documented as a place an operator should write to.
func (cm *ConfigurationManager) nagiosConfigCandidates() []string {
	var candidates []string
	if cm.agentConfig != nil {
		if path := cm.agentConfig.GetConfigPath(); path != "" {
			candidates = append(candidates, filepath.Join(filepath.Dir(path), "nagios.yaml"))
		}
	}
	return append(candidates, filepath.Join("config", "nagios.yaml"))
}

// undeclaredNagiosChannel is a check metric that cannot answer on this
// agent. Hint carries the metric name when the channel given is a
// display label of a definition, the usual mistake: a Nagios check
// matches the metric name, not the PRTG channel. Platforms is set when
// a definition declares the name for other operating systems only.
type undeclaredNagiosChannel struct {
	Check     string
	Channel   string
	Hint      string
	Platforms []string
}

// undeclaredNagiosChannels lists the check metrics that can only ever
// report UNKNOWN on goos, unless a probe with dynamic names (exec,
// prometheus_scrape, snmp_poll, otlp_receiver) happens to emit them.
func undeclaredNagiosChannels(config *NagiosConfig, goos string) ([]undeclaredNagiosChannel, error) {
	defs, err := transformers.DefinitionMetrics()
	if err != nil {
		return nil, fmt.Errorf("reading probe definitions: %w", err)
	}
	runsHere := make(map[string]bool)
	elsewhere := make(map[string][]string)
	byLabel := make(map[string]string)
	for _, metrics := range defs {
		for _, m := range metrics {
			if m.RunsOn(goos) {
				runsHere[m.Name] = true
			} else {
				elsewhere[m.Name] = m.Platforms
			}
			for _, label := range []string{m.Channel, m.DisplayName} {
				if label != "" && label != m.Name {
					byLabel[label] = m.Name
				}
			}
		}
	}

	var out []undeclaredNagiosChannel
	for _, check := range config.Checks {
		for _, metric := range check.Metrics {
			if runsHere[metric.Channel] {
				continue
			}
			out = append(out, undeclaredNagiosChannel{
				Check:     check.Name,
				Channel:   metric.Channel,
				Hint:      byLabel[metric.Channel],
				Platforms: elsewhere[metric.Channel],
			})
		}
	}
	return out, nil
}

func (cm *ConfigurationManager) warnUndeclaredNagiosChannels(path string, config *NagiosConfig) {
	undeclared, err := undeclaredNagiosChannels(config, runtime.GOOS)
	if err != nil {
		cm.logger.Warn().Err(err).Str("path", path).Msg("Nagios channels not checked against the probe definitions")
		return
	}
	for _, u := range undeclared {
		event := cm.logger.Warn().Str("path", path).Str("check", u.Check).Str("channel", u.Channel)
		switch {
		case len(u.Platforms) > 0:
			event.Strs("emitted_on", u.Platforms).Str("platform", runtime.GOOS).Msg("Nagios channel is not emitted on this platform; the check reports UNKNOWN here")
		case u.Hint != "":
			event.Str("metric_name", u.Hint).Msg("Nagios channel is a display label; a check matches the metric name, use metric_name")
		default:
			event.Msg("No probe definition emits this Nagios channel; the check reports UNKNOWN unless a probe with dynamic metric names produces it")
		}
	}
}

// loadEmbeddedNagiosConfig parses and validates the curated Nagios
// configuration embedded in the transformers package.
func (cm *ConfigurationManager) loadEmbeddedNagiosConfig() (*NagiosConfig, error) {
	data, err := transformers.DefaultNagiosConfigYAML()
	if err != nil {
		return nil, fmt.Errorf("reading embedded Nagios configuration: %w", err)
	}

	var config NagiosConfig
	if err := yaml.UnmarshalStrict(data, &config); err != nil {
		return nil, fmt.Errorf("failed to parse embedded Nagios YAML: %w", err)
	}

	if err := cm.validateNagiosConfig(&config); err != nil {
		return nil, fmt.Errorf("invalid embedded Nagios configuration: %w", err)
	}

	return &config, nil
}

// loadNagiosConfigFromFile loads Nagios configuration from a YAML file
func (cm *ConfigurationManager) loadNagiosConfigFromFile(configPath string) (*NagiosConfig, error) {
	data, err := os.ReadFile(filepath.Clean(configPath)) // #nosec G304 - configPath comes from nagiosConfigCandidates, never from a request
	if err != nil {
		return nil, err
	}

	var config NagiosConfig
	if err := yaml.UnmarshalStrict(data, &config); err != nil {
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
			if err := validateNagiosThresholds(metric.Warning, metric.Critical); err != nil {
				return fmt.Errorf("check %s, metric %s: %w", check.Name, metric.Channel, err)
			}
			if !nagiosAggregations[metric.Aggregation] {
				return fmt.Errorf("check %s, metric %s: unknown aggregation %q (none, average, max, min, sum, count)", check.Name, metric.Channel, metric.Aggregation)
			}
			if len(metric.TagSpecificThresholds) > 0 && metric.Aggregation != "" && metric.Aggregation != "none" {
				return fmt.Errorf("check %s, metric %s: tag_specific_thresholds apply per series and need aggregation none", check.Name, metric.Channel)
			}
			for k, entry := range metric.TagSpecificThresholds {
				if len(entry.Tags) == 0 {
					return fmt.Errorf("check %s, metric %s, tag_specific_thresholds %d: tags are required", check.Name, metric.Channel, k)
				}
				if err := validateNagiosThresholds(entry.Warning, entry.Critical); err != nil {
					return fmt.Errorf("check %s, metric %s, tag_specific_thresholds %d: %w", check.Name, metric.Channel, k, err)
				}
			}
		}

		for _, filter := range check.TagFilters {
			if filter.Key == "" {
				return fmt.Errorf("check %s: a tag filter has no key", check.Name)
			}
			if !nagiosTagOperators[filter.Operator] {
				return fmt.Errorf("check %s, tag filter %s: unknown operator %q (in, not_in, equals, not_equals, exists)", check.Name, filter.Key, filter.Operator)
			}
			if filter.Operator != "exists" && len(filter.Values) == 0 {
				return fmt.Errorf("check %s, tag filter %s: operator %s needs values", check.Name, filter.Key, filter.Operator)
			}
		}
	}

	return nil
}

var nagiosAggregations = map[string]bool{
	"": true, "none": true, "average": true, "avg": true, "max": true, "min": true, "sum": true, "count": true,
}

var nagiosTagOperators = map[string]bool{
	"in": true, "not_in": true, "equals": true, "not_equals": true, "exists": true,
}

// validateNagiosThresholds rejects at load what evaluateThreshold would
// otherwise turn into a permanent UNKNOWN. Critical is optional: an
// empty value declares a warn-only check.
func validateNagiosThresholds(warning, critical string) error {
	if warning == "" {
		return fmt.Errorf("warning threshold is required")
	}
	if _, err := strconv.ParseFloat(warning, 64); err != nil {
		return fmt.Errorf("warning threshold %q is not a number", warning)
	}
	if strings.TrimSpace(critical) == "" {
		return nil
	}
	if _, err := strconv.ParseFloat(critical, 64); err != nil {
		return fmt.Errorf("critical threshold %q is not a number", critical)
	}
	return nil
}

// createFallbackNagiosConfig creates a basic fallback configuration.
// Channel names must exist in the transformer definitions (cpu.yaml,
// memory.yaml) — the historical fallback referenced channels no probe
// emits, turning the out-of-the-box checks into permanent UNKNOWN.
func (cm *ConfigurationManager) createFallbackNagiosConfig() *NagiosConfig {
	return &NagiosConfig{
		Version:     "1.0.1",
		Description: "Fallback Nagios configuration",
		Checks: []NagiosCheck{
			{
				Name:        "system_health",
				Description: "Basic system health check",
				Metrics: []NagiosMetric{
					{
						Channel:     "cpu_usage_total",
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
