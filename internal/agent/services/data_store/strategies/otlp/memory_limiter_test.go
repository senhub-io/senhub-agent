package otlp

import (
	"context"
	"testing"
	"time"

	"senhub-agent.go/internal/agent/services/agentstate"
	"senhub-agent.go/internal/agent/tags"
	"senhub-agent.go/internal/agent/types/datapoint"
)

func TestMemoryLimiter_DisabledWhenBothZero(t *testing.T) {
	ml := newMemoryLimiter(0, 0, time.Second)
	if ml.enabled() {
		t.Error("expected disabled when both limits are zero")
	}
	if got := ml.currentState(); got != memoryOK {
		t.Errorf("disabled limiter should report OK, got %v", got)
	}
}

func TestMemoryLimiter_EnabledWhenSoftOrHardSet(t *testing.T) {
	if !newMemoryLimiter(100, 0, time.Second).enabled() {
		t.Error("expected enabled when only soft is set")
	}
	if !newMemoryLimiter(0, 200, time.Second).enabled() {
		t.Error("expected enabled when only hard is set")
	}
	if !newMemoryLimiter(100, 200, time.Second).enabled() {
		t.Error("expected enabled when both are set")
	}
}

func TestMemoryLimiter_PollComputesStateFromMemStats(t *testing.T) {
	// 1-byte soft + 2-byte hard guarantees we're always over both
	// thresholds. Test runner footprint is well above 2 bytes.
	ml := newMemoryLimiter(1, 2, time.Hour)
	ml.pollAndUpdate()
	if got := ml.currentState(); got != memoryHard {
		t.Errorf("expected hard state with 1B/2B limits, got %v", got)
	}

	// A trillion-byte soft limit guarantees we're well under, even on
	// memory-heavy CI runners.
	ml = newMemoryLimiter(1e12, 1e12+1, time.Hour)
	ml.pollAndUpdate()
	if got := ml.currentState(); got != memoryOK {
		t.Errorf("expected OK state with 1TB limits, got %v", got)
	}
}

func TestMemoryLimiter_SoftStateBetweenSoftAndHard(t *testing.T) {
	// Soft state requires HeapAlloc to be in [soft, hard). The test
	// runner footprint is somewhere in the tens of MiB; 1 KiB soft +
	// 100 GiB hard puts us squarely in the soft band.
	ml := newMemoryLimiter(1024, 100*1024*1024*1024, time.Hour)
	ml.pollAndUpdate()
	if got := ml.currentState(); got != memorySoft {
		t.Errorf("expected soft state with 1KiB/100GiB band, got %v", got)
	}
}

func TestMemoryLimiter_StopWithoutStartIsSafe(t *testing.T) {
	// Historical bug: Shutdown calls memLimiter.stop() even if Start
	// was never called. Without the started guard, stop() blocks
	// forever on stoppedCh.
	ml := newMemoryLimiter(1024, 2048, time.Hour)
	done := make(chan struct{})
	go func() {
		ml.stop()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("stop() deadlocked when start() was never called")
	}
}

func TestMemoryLimiter_StopIsIdempotent(t *testing.T) {
	ml := newMemoryLimiter(1024, 2048, time.Hour)
	ml.start(context.Background())
	ml.stop()
	done := make(chan struct{})
	go func() {
		ml.stop()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("second stop() deadlocked")
	}
}

func TestMetricStore_MemoryLimiter_SoftDropsNewSeries(t *testing.T) {
	// Drive the limiter into soft state via micro-thresholds.
	ml := newMemoryLimiter(1024, 100*1024*1024*1024, time.Hour)
	ml.pollAndUpdate()
	if got := ml.currentState(); got != memorySoft {
		t.Fatalf("setup error: expected soft state, got %v", got)
	}

	store := newMetricStore().withMemoryLimiter(ml)
	dp := datapoint.DataPoint{
		Name: "new_series_should_be_dropped",
		Tags: []tags.Tag{
			{Key: "probe_name", Value: "p"},
			{Key: "probe_type", Value: "t"},
		},
	}
	before := agentstate.GetOTLPDroppedByReason()["memory_soft_limit"]
	store.upsert(dp)
	after := agentstate.GetOTLPDroppedByReason()["memory_soft_limit"]
	if after != before+1 {
		t.Errorf("memory_soft_limit counter delta: before=%d after=%d, want +1", before, after)
	}
	if got := store.size(); got != 0 {
		t.Errorf("new series should be dropped under soft pressure, store size=%d", got)
	}
}

func TestMetricStore_MemoryLimiter_HardDropsEverything(t *testing.T) {
	ml := newMemoryLimiter(1, 2, time.Hour)
	ml.pollAndUpdate()
	if got := ml.currentState(); got != memoryHard {
		t.Fatalf("setup error: expected hard state, got %v", got)
	}

	store := newMetricStore().withMemoryLimiter(ml)
	dp := datapoint.DataPoint{
		Name: "m",
		Tags: []tags.Tag{
			{Key: "probe_name", Value: "p"},
			{Key: "probe_type", Value: "t"},
		},
	}
	before := agentstate.GetOTLPDroppedByReason()["memory_hard_limit"]
	store.upsert(dp)
	after := agentstate.GetOTLPDroppedByReason()["memory_hard_limit"]
	if after != before+1 {
		t.Errorf("memory_hard_limit counter delta: before=%d after=%d, want +1", before, after)
	}
	if got := store.size(); got != 0 {
		t.Errorf("hard pressure should reject all writes, store size=%d", got)
	}
}
