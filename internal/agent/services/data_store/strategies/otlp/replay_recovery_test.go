package otlp

import (
	"sync/atomic"
	"testing"
	"time"
)

func TestReplayClock_IsCappedAtOneMinute(t *testing.T) {
	if replayMaxDelay > time.Minute {
		t.Errorf("replayMaxDelay = %v, want at most 1m", replayMaxDelay)
	}
	cur := replayFirstDelay
	for i := 0; i < 10; i++ {
		cur = nextReplayDelay(cur, replayFirstDelay, replayMaxDelay, true)
	}
	if cur != replayMaxDelay {
		t.Errorf("backoff settles at %v, want %v", cur, replayMaxDelay)
	}
}

// A live export that gets through while a backlog waits starts the replay at
// once, even though the backend was never marked down.
func TestLiveSuccessWithBacklogStartsReplay(t *testing.T) {
	col := newFakeCollector(t, "grpc")
	rig := newWireRig(t, col, t.TempDir(), "traces", 0)
	var replays atomic.Int32
	rig.ch.setOnRecovered(func() { replays.Add(1) })

	if !rig.ch.persistRequest([]byte("x"), 1) {
		t.Fatal("could not seed the queue")
	}
	if err := rig.pushSpan("live"); err != nil {
		t.Fatal(err)
	}
	eventually(t, "the replay to start", func() bool { return replays.Load() > 0 })

	// With nothing waiting, a live success starts no replay.
	rig2 := newWireRig(t, col, t.TempDir(), "traces", 0)
	var other atomic.Int32
	rig2.ch.setOnRecovered(func() { other.Add(1) })
	if err := rig2.pushSpan("live"); err != nil {
		t.Fatal(err)
	}
	time.Sleep(100 * time.Millisecond)
	if other.Load() != 0 {
		t.Error("a live success with an empty queue started a replay")
	}
}

// Ordered metrics wait behind the backlog, so only the retry clock can see
// the collector back: it does within its bounded gap.
func TestOrderedMetricsBacklogRecoversOnTheClock(t *testing.T) {
	col := newFakeCollector(t, "grpc")
	rig := newWireRig(t, col, t.TempDir(), "metrics", 0)
	startRigLoop(t, rig)
	col.setDown(true)
	for i := 1; i <= 3; i++ {
		_ = rig.pushMetric(i, 1)
	}
	col.setDown(false)
	_ = rig.pushMetric(4, 1) // behind the backlog, not sent live
	start := time.Now()
	eventually(t, "the backlog to be delivered", func() bool { return len(col.receivedMetrics()) == 4 })
	if d := time.Since(start); d > rig.rp.maxDelay+3*time.Second {
		t.Errorf("recovery took %v, want within the retry gap %v", d, rig.rp.maxDelay)
	}
}
