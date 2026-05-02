package http

import (
	"testing"
	"time"

	"senhub-agent.go/internal/agent/cliArgs"
	"senhub-agent.go/internal/agent/services/data_store/transformers"
	"senhub-agent.go/internal/agent/services/logger"
)

func newTestModuleLogger() *logger.ModuleLogger {
	args := &cliArgs.ParsedArgs{Env: "test", Verbose: false}
	return logger.NewModuleLogger(logger.NewLogger(args), "test.prom")
}

func newTestCache(t *testing.T) *MetricCache {
	return NewMetricCache(5*time.Minute, newTestModuleLogger())
}

func TestCacheAdapter_HostLevelFilterDisabledByDefault(t *testing.T) {
	cache := newTestCache(t)
	registry := transformers.NewTransformerRegistry(newTestModuleLogger().Logger)

	adapter := &cacheAdapter{
		cache:            cache,
		registry:         registry,
		excludeHostLevel: false, // expose_host_metrics: true (default)
	}

	// cpu probe is host_level: true in YAML
	if adapter.isHostLevelProbe("cpu") != true {
		t.Errorf("cpu must be detected as host_level (its YAML has host_level: true)")
	}

	// netscaler is NOT host-level (it monitors a remote appliance)
	if adapter.isHostLevelProbe("netscaler") {
		t.Errorf("netscaler must NOT be host_level (remote target)")
	}

	// Unknown probe types are NOT host-level (safer default).
	if adapter.isHostLevelProbe("nonexistent_probe") {
		t.Errorf("unknown probe must default to NOT host_level")
	}

	// Empty probe type → not host-level.
	if adapter.isHostLevelProbe("") {
		t.Errorf("empty probe type must not be host_level")
	}
}

func TestCacheAdapter_HostLevelMemoization(t *testing.T) {
	registry := transformers.NewTransformerRegistry(newTestModuleLogger().Logger)
	adapter := &cacheAdapter{
		cache:            newTestCache(t),
		registry:         registry,
		excludeHostLevel: true,
	}

	// First call populates cache.
	first := adapter.isHostLevelProbe("cpu")
	// Second call hits cache.
	second := adapter.isHostLevelProbe("cpu")
	if first != second || !first {
		t.Errorf("memoization broken: first=%v second=%v", first, second)
	}

	// Cache map must contain the key.
	adapter.hostLevelMu.RLock()
	_, found := adapter.hostLevelCache["cpu"]
	adapter.hostLevelMu.RUnlock()
	if !found {
		t.Errorf("hostLevelCache should contain 'cpu' after lookup")
	}
}

// All four host-level probes must be flagged correctly.
func TestCacheAdapter_AllHostLevelProbesDetected(t *testing.T) {
	registry := transformers.NewTransformerRegistry(newTestModuleLogger().Logger)
	adapter := &cacheAdapter{
		cache:            newTestCache(t),
		registry:         registry,
		excludeHostLevel: true,
	}

	hostLevel := []string{"cpu", "memory", "network", "logicaldisk"}
	for _, pt := range hostLevel {
		if !adapter.isHostLevelProbe(pt) {
			t.Errorf("probe %q must be host_level (per its YAML host_level: true)", pt)
		}
	}

	// Spot check non-host-level probes
	notHostLevel := []string{"netscaler", "citrix", "redfish", "veeam", "ping_webapp"}
	for _, pt := range notHostLevel {
		if adapter.isHostLevelProbe(pt) {
			t.Errorf("probe %q must NOT be host_level", pt)
		}
	}
}
