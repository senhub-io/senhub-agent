package probes

import (
	"testing"
	"time"

	"senhub-agent.go/internal/agent/services/agentstate"
)

func TestScheduleObserver(t *testing.T) {
	agentstate.ResetCollectionScheduleForTest()
	t.Cleanup(agentstate.ResetCollectionScheduleForTest)

	const probe = "test-probe-type"
	obs := newScheduleObserver(probe)

	obs.markStart()
	time.Sleep(30 * time.Millisecond)

	obs.CallStarted(25 * time.Millisecond)
	if got := agentstate.CollectionsInFlight(); got != 1 {
		t.Errorf("CollectionsInFlight after CallStarted = %d, want 1", got)
	}

	obs.CallEnded(true)
	if got := agentstate.CollectionsInFlight(); got != 0 {
		t.Errorf("CollectionsInFlight after CallEnded = %d, want 0", got)
	}

	delays := agentstate.GetFirstCollectionDelays()
	firstDelay, ok := delays[probe]
	if !ok {
		t.Fatalf("first collection delay not recorded for probe %q: %v", probe, delays)
	}
	if firstDelay < 0.015 || firstDelay > 0.2 {
		t.Errorf("first collection delay = %f, want about 0.03s", firstDelay)
	}

	lagWindow := agentstate.TakeCollectLagWindow("test")
	lag, ok := lagWindow[probe]
	if !ok || lag <= 0 {
		t.Errorf("collect lag not recorded for probe %q: %v", probe, lagWindow)
	}

	obs.CallOverran()
	overruns := agentstate.GetCollectOverruns()
	if got := overruns[probe]; got != 1 {
		t.Errorf("collect overruns for probe %q = %d, want 1", probe, got)
	}

	before := firstDelay
	obs.CallStarted(0)
	obs.CallEnded(false)
	after := agentstate.GetFirstCollectionDelays()
	if after[probe] != before {
		t.Errorf("non-first CallEnded recorded first collection delay: %f -> %f", before, after[probe])
	}
}
