package http

import (
	"testing"

	"senhub-agent.go/internal/agent/services/data_store/transformers"
)

// TestLoadNagiosConfig_DefaultIsEmbedded asserts that with no operator
// file present, the agent serves the curated embedded configuration —
// not the minimal hardcoded fallback whose historical channel names
// (cpu_usage_percent, network_bytes_total) no probe emits (#315).
func TestLoadNagiosConfig_DefaultIsEmbedded(t *testing.T) {
	cm := NewConfigurationManager(nil, map[string]interface{}{}, newTestLogger())

	config := cm.LoadNagiosConfig()
	if config == nil {
		t.Fatal("LoadNagiosConfig returned nil")
	}
	if config.Description == "Fallback Nagios configuration" {
		t.Fatalf("default Nagios config is the hardcoded fallback (version %s); expected the embedded definitions/nagios.yaml", config.Version)
	}
	if len(config.Checks) < 2 {
		t.Fatalf("embedded Nagios config has %d checks, expected the curated set", len(config.Checks))
	}
}

// TestNagiosDefaultChecks_ChannelsExist asserts that every channel
// referenced by the served default Nagios configuration AND by the
// hardcoded fallback exists in the transformer probe definitions —
// i.e. some probe actually emits it. A check referencing a phantom
// channel returns permanent UNKNOWN (#315).
func TestNagiosDefaultChecks_ChannelsExist(t *testing.T) {
	perProbe, err := transformers.DefinitionMetricNames()
	if err != nil {
		t.Fatalf("DefinitionMetricNames: %v", err)
	}
	emitted := make(map[string]bool)
	for _, names := range perProbe {
		for _, n := range names {
			emitted[n] = true
		}
	}
	if len(emitted) == 0 {
		t.Fatal("no metric names extracted from embedded probe definitions")
	}

	cm := NewConfigurationManager(nil, map[string]interface{}{}, newTestLogger())

	configs := map[string]*NagiosConfig{
		"served-default":     cm.LoadNagiosConfig(),
		"hardcoded-fallback": cm.createFallbackNagiosConfig(),
	}
	for source, config := range configs {
		for _, check := range config.Checks {
			for _, metric := range check.Metrics {
				if !emitted[metric.Channel] {
					t.Errorf("%s: check %q references channel %q which no probe definition emits", source, check.Name, metric.Channel)
				}
			}
		}
	}
}
