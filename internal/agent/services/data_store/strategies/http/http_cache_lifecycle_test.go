package http

import (
	"testing"
	"time"

	"senhub-agent.go/internal/agent/types/datapoint"
)

// These tests pin #270: the cleanup stop channel was made once at
// construction, so the HTTP server restart path (port / bind-address
// change → Shutdown → Start) left the TTL cleanup goroutine dead
// after the first restart (unbounded cache growth) and panicked
// (close of closed channel) on the second.

func TestMetricCache_StopStartCyclesDoNotPanic(t *testing.T) {
	cache := NewMetricCache(time.Minute, createTestModuleLogger())

	for i := 0; i < 3; i++ {
		cache.StartCleanupRoutine()
		cache.Stop()
	}
	// Stop without a running routine is a no-op, not a panic.
	cache.Stop()
}

func TestMetricCache_StartCleanupRoutineIdempotent(t *testing.T) {
	cache := NewMetricCache(time.Minute, createTestModuleLogger())
	defer cache.Stop()

	cache.StartCleanupRoutine()
	cache.StartCleanupRoutine() // second call while running: no-op, no second goroutine
}

func TestMetricCache_CleanupAliveAfterRestart(t *testing.T) {
	cache, registry := newCapTestCache(t, 50*time.Millisecond, 0)

	// First lifecycle.
	cache.StartCleanupRoutine()
	cache.Stop()

	// Restart — the cleanup routine must come back to life.
	cache.StartCleanupRoutine()
	defer cache.Stop()

	cache.AddDataPointsWithTransformer(
		[]datapoint.DataPoint{networkSeriesDataPoint("eth0", 1)}, registry)
	if got := len(cache.GetAllMetrics()); got != 1 {
		t.Fatalf("cache size after add: got %d, want 1", got)
	}

	deadline := time.Now().Add(5 * time.Second)
	for len(cache.GetAllMetrics()) > 0 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if got := len(cache.GetAllMetrics()); got != 0 {
		t.Errorf("cache size after TTL: got %d, want 0 (cleanup routine dead after restart)", got)
	}
}
