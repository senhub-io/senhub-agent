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

func redisBatch(names ...string) []datapoint.DataPoint {
	out := make([]datapoint.DataPoint, 0, len(names))
	for _, n := range names {
		out = append(out, datapoint.DataPoint{Name: n, Value: 1.0, Timestamp: time.Now(),
			Tags: []tags.Tag{{Key: "probe_name", Value: "Lab-Redis"}, {Key: "probe_type", Value: "redis"}}})
	}
	return out
}

// A target that goes down leaves its probe reporting only that it is
// down. The values the previous run reported must leave with the next
// run instead of being served as current until the window closes (#951).
func TestCacheRetiresWhatTheLatestRunNoLongerReports(t *testing.T) {
	baseLogger := createTestLogger()
	cache := NewMetricCache(5*time.Minute, logger.NewModuleLogger(baseLogger, "test"))
	registry := transformers.NewTransformerRegistry(baseLogger)
	cache.NoteProbeCadence("Lab-Redis", time.Minute)
	cache.AddDataPointsWithTransformer(redisBatch("redis_up", "redis_uptime"), registry)

	// The previous run happened one cadence ago.
	cache.mu.Lock()
	for k, m := range cache.timeSeries {
		m.Timestamp = time.Now().Add(-time.Minute)
		cache.timeSeries[k] = m
	}
	cache.mu.Unlock()

	cache.AddDataPointsWithTransformer(redisBatch("redis_up"), registry)
	got := cache.GetProbeMetrics("lab-redis")
	if len(got) != 1 || got[0].MetricName != "redis_up" {
		t.Errorf("cache holds %d series after the target went down, want only redis_up", len(got))
	}

	// A second batch of the same run a few seconds later keeps the first.
	cache.AddDataPointsWithTransformer(redisBatch("redis_clients"), registry)
	if n := len(cache.GetProbeMetrics("lab-redis")); n != 2 {
		t.Errorf("a second batch of one run retired the first: %d series", n)
	}
}
