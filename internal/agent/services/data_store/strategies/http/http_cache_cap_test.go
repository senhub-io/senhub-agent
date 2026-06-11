package http

import (
	"fmt"
	"testing"
	"time"

	"senhub-agent.go/internal/agent/services/agentstate"
	"senhub-agent.go/internal/agent/services/data_store/transformers"
	"senhub-agent.go/internal/agent/tags"
	"senhub-agent.go/internal/agent/types/datapoint"
)

// networkSeriesDataPoint builds a datapoint whose cache key is
// discriminated by the interface tag — each distinct interface value
// creates a distinct time series (DiscriminantTagsRegistry["network"]).
func networkSeriesDataPoint(iface string, value float32) datapoint.DataPoint {
	return datapoint.DataPoint{
		Name:  "bytes_sent",
		Value: value,
		Tags: []tags.Tag{
			{Key: "probe_name", Value: "network"},
			{Key: "probe_type", Value: "network"},
			{Key: "interface", Value: iface},
		},
	}
}

func newCapTestCache(t *testing.T, ttl time.Duration, maxSeries int) (*MetricCache, *transformers.TransformerRegistry) {
	t.Helper()
	moduleLogger := createTestModuleLogger()
	cache := NewMetricCache(ttl, moduleLogger)
	cache.SetMaxSeries(maxSeries)
	return cache, transformers.NewTransformerRegistry(moduleLogger.Logger)
}

func httpCacheCapDrops() uint64 {
	return agentstate.GetHTTPCacheDroppedByReason()["http_cache_cap"]
}

func TestMetricCacheCap_DropsNewSeriesAtCap(t *testing.T) {
	agentstate.ResetHTTPCacheDroppedForTest()
	t.Cleanup(agentstate.ResetHTTPCacheDroppedForTest)

	cache, registry := newCapTestCache(t, 5*time.Minute, 10)

	var dps []datapoint.DataPoint
	for i := 0; i < 15; i++ {
		dps = append(dps, networkSeriesDataPoint(fmt.Sprintf("eth%d", i), 1))
	}
	cache.AddDataPointsWithTransformer(dps, registry)

	if got := len(cache.GetAllMetrics()); got != 10 {
		t.Errorf("cache size: got %d, want 10 (cap)", got)
	}
	if got := httpCacheCapDrops(); got != 5 {
		t.Errorf("drop counter: got %d, want 5", got)
	}
}

func TestMetricCacheCap_ExistingSeriesKeepUpdating(t *testing.T) {
	agentstate.ResetHTTPCacheDroppedForTest()
	t.Cleanup(agentstate.ResetHTTPCacheDroppedForTest)

	cache, registry := newCapTestCache(t, 5*time.Minute, 3)

	cache.AddDataPointsWithTransformer([]datapoint.DataPoint{
		networkSeriesDataPoint("eth0", 1),
		networkSeriesDataPoint("eth1", 1),
		networkSeriesDataPoint("eth2", 1),
	}, registry)

	// At cap: a new series is refused, an existing one still updates.
	cache.AddDataPointsWithTransformer([]datapoint.DataPoint{
		networkSeriesDataPoint("eth3", 1),
		networkSeriesDataPoint("eth0", 42),
	}, registry)

	metrics := cache.GetAllMetrics()
	if got := len(metrics); got != 3 {
		t.Fatalf("cache size: got %d, want 3 (cap)", got)
	}
	var eth0Value interface{}
	for _, m := range metrics {
		if m.Tags["interface"] == "eth0" {
			eth0Value = m.Value
		}
		if m.Tags["interface"] == "eth3" {
			t.Errorf("eth3 should have been dropped at cap")
		}
	}
	if eth0Value != float32(42) {
		t.Errorf("existing series eth0 not updated: got %v, want 42", eth0Value)
	}
	if got := httpCacheCapDrops(); got != 1 {
		t.Errorf("drop counter: got %d, want 1", got)
	}
}

func TestMetricCacheCap_TTLEvictionFreesCapacity(t *testing.T) {
	agentstate.ResetHTTPCacheDroppedForTest()
	t.Cleanup(agentstate.ResetHTTPCacheDroppedForTest)

	cache, registry := newCapTestCache(t, 30*time.Millisecond, 2)

	cache.AddDataPointsWithTransformer([]datapoint.DataPoint{
		networkSeriesDataPoint("eth0", 1),
		networkSeriesDataPoint("eth1", 1),
	}, registry)

	// At cap: eth2 is refused.
	cache.AddDataPointsWithTransformer([]datapoint.DataPoint{
		networkSeriesDataPoint("eth2", 1),
	}, registry)
	if got := httpCacheCapDrops(); got != 1 {
		t.Fatalf("drop counter: got %d, want 1", got)
	}

	// Let both stored series expire and run the TTL sweep.
	time.Sleep(60 * time.Millisecond)
	cache.cleanup()
	if got := len(cache.GetAllMetrics()); got != 0 {
		t.Fatalf("cache size after TTL sweep: got %d, want 0", got)
	}

	// The previously refused series must now be admitted.
	cache.AddDataPointsWithTransformer([]datapoint.DataPoint{
		networkSeriesDataPoint("eth2", 7),
	}, registry)
	metrics := cache.GetAllMetrics()
	if got := len(metrics); got != 1 {
		t.Fatalf("cache size: got %d, want 1", got)
	}
	if metrics[0].Tags["interface"] != "eth2" {
		t.Errorf("admitted series: got interface=%q, want eth2", metrics[0].Tags["interface"])
	}
	if got := httpCacheCapDrops(); got != 1 {
		t.Errorf("drop counter after re-admission: got %d, want 1 (unchanged)", got)
	}
}

func TestMetricCacheCap_ZeroMeansUnbounded(t *testing.T) {
	agentstate.ResetHTTPCacheDroppedForTest()
	t.Cleanup(agentstate.ResetHTTPCacheDroppedForTest)

	cache, registry := newCapTestCache(t, 5*time.Minute, 0)

	var dps []datapoint.DataPoint
	for i := 0; i < 20; i++ {
		dps = append(dps, networkSeriesDataPoint(fmt.Sprintf("eth%d", i), 1))
	}
	cache.AddDataPointsWithTransformer(dps, registry)

	if got := len(cache.GetAllMetrics()); got != 20 {
		t.Errorf("cache size: got %d, want 20 (unbounded)", got)
	}
	if got := httpCacheCapDrops(); got != 0 {
		t.Errorf("drop counter: got %d, want 0", got)
	}
}

func TestMetricCacheCap_DefaultMatchesOTLPStoreCap(t *testing.T) {
	cache := NewMetricCache(5*time.Minute, createTestModuleLogger())
	if cache.maxSeries != DefaultMaxCacheSeries {
		t.Errorf("default cap: got %d, want %d", cache.maxSeries, DefaultMaxCacheSeries)
	}
	if DefaultMaxCacheSeries != 50000 {
		t.Errorf("DefaultMaxCacheSeries: got %d, want 50000 (same as the OTLP store)", DefaultMaxCacheSeries)
	}
}

// TestUpdateConfiguration_PropagatesMaxCacheSize pins the reviewer
// finding on #281: a runtime config reload must re-apply the
// cardinality cap to the live cache — port, bind_address and TTL were
// already handled, max_cache_size was the one knob left stale (the
// config manager reported the new value while the cache enforced the
// old one until restart).
func TestUpdateConfiguration_PropagatesMaxCacheSize(t *testing.T) {
	agentstate.ResetHTTPCacheDroppedForTest()
	t.Cleanup(agentstate.ResetHTTPCacheDroppedForTest)

	strategy := newServerTestStrategy(t, 0)
	strategy.cache.SetMaxSeries(2)
	registry := transformers.NewTransformerRegistry(createTestModuleLogger().Logger)

	fill := func(n int) {
		var dps []datapoint.DataPoint
		for i := 0; i < n; i++ {
			dps = append(dps, networkSeriesDataPoint(fmt.Sprintf("eth%d", i), 1))
		}
		strategy.cache.AddDataPointsWithTransformer(dps, registry)
	}
	fill(5)
	if got := len(strategy.cache.GetAllMetrics()); got != 2 {
		t.Fatalf("pre-reload size = %d, want capped at 2", got)
	}

	if err := strategy.UpdateConfiguration(map[string]interface{}{"max_cache_size": 10}); err != nil {
		t.Fatalf("UpdateConfiguration: %v", err)
	}
	fill(5)
	if got := len(strategy.cache.GetAllMetrics()); got <= 2 {
		t.Fatalf("post-reload size = %d; the raised cap was not propagated to the live cache", got)
	}
}
