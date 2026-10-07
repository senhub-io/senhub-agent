package otlp

import (
	"testing"
	"time"
)

func TestNextReplayDelay_Sequence(t *testing.T) {
	want := []time.Duration{10, 20, 40, 80, 160, 300, 300}
	cur := replayFirstDelay
	for i, w := range want {
		cur = nextReplayDelay(cur, replayFirstDelay, replayMaxDelay, true)
		if cur != w*time.Second {
			t.Fatalf("step %d: %v, want %v", i, cur, w*time.Second)
		}
	}
	if got := nextReplayDelay(cur, replayFirstDelay, replayMaxDelay, false); got != replayFirstDelay {
		t.Errorf("after a success the gap must restart at %v, got %v", replayFirstDelay, got)
	}
}

// The loop itself, driven by an injected clock: the gaps between probes of
// a collector that stays down are 5, 10, 20, 40, 80, 160, 300, 300 s, and
// a later outage starts again at 5 s.
func TestLogsQueueAck_BackoffGapsOnInjectedClock(t *testing.T) {
	r := newAckRig(t, t.TempDir(), true)
	asked := make(chan time.Duration, 32)
	fire := make(chan time.Time)
	r.rp.after = func(d time.Duration) <-chan time.Time {
		asked <- d
		return fire
	}
	r.ple.setOnQueued(r.rp.kick)

	r.emitFlushed("a") // fails, goes to disk
	r.rp.start()
	defer r.rp.stop()

	next := func() time.Duration {
		t.Helper()
		select {
		case d := <-asked:
			return d
		case <-time.After(3 * time.Second):
			t.Fatal("the loop did not arm its clock")
			return 0
		}
	}
	want := []int{5, 10, 20, 40, 80, 160, 300, 300}
	for i, w := range want {
		if got := next(); got != time.Duration(w)*time.Second {
			t.Fatalf("gap %d = %v, want %ds", i, got, w)
		}
		fire <- time.Time{} // the probe runs and fails
	}

	r.exp.setDown(false)
	if got := next(); got != replayMaxDelay {
		t.Fatalf("armed %v before the recovery probe", got)
	}
	fire <- time.Time{} // acknowledged: the queue empties
	if got := next(); got != replayMaxDelay {
		t.Errorf("idle gap after a drained queue = %v, want %v", got, replayMaxDelay)
	}
	if r.pendingRecords() != 0 {
		t.Fatalf("%d records left after recovery", r.pendingRecords())
	}

	r.exp.setDown(true)
	r.ple.healthy.Store(true) // the idle loop has not noticed the new outage yet
	r.emitFlushed("b")
	if got := next(); got != replayFirstDelay {
		t.Errorf("a new outage must restart at %v, got %v", replayFirstDelay, got)
	}
}
