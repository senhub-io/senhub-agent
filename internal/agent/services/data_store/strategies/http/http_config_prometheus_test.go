package http

import (
	"testing"

	"senhub-agent.go/internal/agent/cliArgs"
	"senhub-agent.go/internal/agent/services/logger"
)

func newTestLogger() *logger.ModuleLogger {
	args := &cliArgs.ParsedArgs{Env: "test", Verbose: false}
	return logger.NewModuleLogger(logger.NewLogger(args), "test.config")
}

func TestPrometheusConfigDefaults(t *testing.T) {
	cm := NewConfigurationManager(nil, map[string]interface{}{}, newTestLogger())
	if !cm.IsPrometheusIncludeProbeTags() {
		t.Errorf("include_probe_tags default: got false, want true")
	}
	if !cm.IsPrometheusExposeHostMetrics() {
		t.Errorf("expose_host_metrics default: got false, want true")
	}
}

func TestPrometheusConfigOverride(t *testing.T) {
	params := map[string]interface{}{
		"prometheus": map[string]interface{}{
			"include_probe_tags":  false,
			"expose_host_metrics": false,
		},
	}
	cm := NewConfigurationManager(nil, params, newTestLogger())
	if cm.IsPrometheusIncludeProbeTags() {
		t.Errorf("include_probe_tags override: got true, want false")
	}
	if cm.IsPrometheusExposeHostMetrics() {
		t.Errorf("expose_host_metrics override: got true, want false")
	}
}

func TestPrometheusConfigInvalidShape_KeepsDefaults(t *testing.T) {
	// If `prometheus:` is a string instead of a map, ignore it gracefully.
	params := map[string]interface{}{"prometheus": "not-a-map"}
	cm := NewConfigurationManager(nil, params, newTestLogger())
	if !cm.IsPrometheusIncludeProbeTags() {
		t.Errorf("malformed config should preserve default true for include_probe_tags")
	}
	if !cm.IsPrometheusExposeHostMetrics() {
		t.Errorf("malformed config should preserve default true for expose_host_metrics")
	}
}
