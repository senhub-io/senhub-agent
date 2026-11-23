package otlp

import (
	"sync/atomic"
	"testing"
	"time"
)

func TestExporterHealth_OneRecoveryCallbackAtATime(t *testing.T) {
	var h exporterHealth
	h.init("otlp/test", testModuleLogger(t))
	release := make(chan struct{})
	var started atomic.Int32
	h.setOnRecovered(func() { started.Add(1); <-release })
	for i := 0; i < 5; i++ {
		h.markFailure()
		h.markSuccess()
	}
	time.Sleep(50 * time.Millisecond)
	if n := started.Load(); n != 1 {
		t.Errorf("%d callbacks in flight after a flapping backend, want 1", n)
	}
	close(release)
	h.closeRecovery()
}

func TestReplayLoop_StopTwiceAndNoCallbackAfterStop(t *testing.T) {
	col := newFakeCollector(t, "grpc")
	rig := newWireRig(t, col, t.TempDir(), "metrics", 0)
	var calls atomic.Int32
	rig.ch.setOnRecovered(func() { calls.Add(1) })
	rig.rp.start()
	rig.rp.stop()
	rig.rp.stop() // must not panic
	rig.ch.markFailure()
	rig.ch.markSuccess()
	time.Sleep(50 * time.Millisecond)
	if calls.Load() != 0 {
		t.Error("recovery callback ran after the replayer stopped")
	}
}
