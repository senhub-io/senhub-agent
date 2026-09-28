package http

import (
	"testing"
	"time"

	"senhub-agent.go/internal/agent/services/data_store/transformers"
	"senhub-agent.go/internal/agent/services/logger"
	"senhub-agent.go/internal/agent/tags"
	"senhub-agent.go/internal/agent/types/datapoint"
)

// cachedUpdatesAged stores one os_updates value and ages it, as an
// hourly probe's single run looks some time after it happened.
func cachedUpdatesAged(t *testing.T, cadence, age time.Duration) (*MetricCache, *FormatConverter) {
	t.Helper()
	baseLogger := createTestLogger()
	moduleLogger := logger.NewModuleLogger(baseLogger, "test")
	cache := NewMetricCache(5*time.Minute, moduleLogger)
	registry := transformers.NewTransformerRegistry(baseLogger)
	cache.NoteProbeCadence("os_updates", cadence)
	cache.AddDataPointsWithTransformer([]datapoint.DataPoint{{
		Name: "os.updates.pending", Value: 20.0, Timestamp: time.Now(),
		Tags: []tags.Tag{{Key: "probe_name", Value: "os_updates"}, {Key: "probe_type", Value: "os_updates"}},
	}}, registry)
	cache.mu.Lock()
	for k, m := range cache.timeSeries {
		m.Timestamp = time.Now().Add(-age)
		cache.timeSeries[k] = m
	}
	cache.mu.Unlock()
	return cache, NewFormatConverter(registry, moduleLogger, cache)
}

// An hourly probe's value stays served between two runs. Held to the
// five-minute TTL, PRTG answered an empty sensor fifty-five minutes an
// hour and the cleanup took it from Nagios and Prometheus as well.
func TestCacheServesAValueItsProbeStillVouchesFor(t *testing.T) {
	cache, fc := cachedUpdatesAged(t, time.Hour, 40*time.Minute)
	cache.cleanup()
	if n := len(cache.GetProbeMetrics("os_updates")); n != 1 {
		t.Fatalf("cleanup evicted an hourly value 40 minutes old; %d left", n)
	}
	if ch := fc.GetMetricsForProbeWithFilter("os_updates", MetricFilter{ShowTags: true}); len(ch) != 1 {
		t.Fatalf("PRTG served %d channels for an hourly value 40 minutes old, want 1", len(ch))
	}
	if st := cache.GetProbeStatistics()["os_updates"]; time.Since(st.LastUpdate) >= st.LiveWindow {
		t.Errorf("the probe reads inactive between two runs: window %v", st.LiveWindow)
	}
}

// Once the probe missed its next run by half a cadence, the value goes.
func TestCacheDropsAValueOnceItsProbeMissedItsRun(t *testing.T) {
	cache, fc := cachedUpdatesAged(t, time.Hour, 2*time.Hour)
	if ch := fc.GetMetricsForProbeWithFilter("os_updates", MetricFilter{ShowTags: true}); len(ch) != 0 {
		t.Fatalf("PRTG still serves a value two cadences old: %d channels", len(ch))
	}
	cache.cleanup()
	if n := len(cache.GetProbeMetrics("os_updates")); n != 0 {
		t.Fatalf("cleanup kept a value two cadences old; %d left", n)
	}
}

// Without a known cadence the TTL alone applies, as before.
func TestCacheKeepsTheTTLWhenTheCadenceIsUnknown(t *testing.T) {
	cache, _ := cachedUpdatesAged(t, 0, 6*time.Minute)
	cache.cleanup()
	if n := len(cache.GetProbeMetrics("os_updates")); n != 0 {
		t.Fatalf("a value past the TTL survived without a cadence; %d left", n)
	}
}
