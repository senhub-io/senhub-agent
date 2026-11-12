package probes

import (
	"testing"
	"time"

	"senhub-agent.go/internal/agent/services/agentstate"
)

func TestScheduleObserver(t *testing.T) {
	agentstate.ResetCollectionScheduleForTest()
	t.Cleanup(agentstate.ResetCollectionScheduleForTest)

	prev := agentstate.ProcessStart()
	agentstate.SetProcessStartForTest(time.Now().Add(-2 * time.Second))
	t.Cleanup(func() { agentstate.SetProcessStartForTest(prev) })

	probe := agentstate.ProbeKey{Name: "instance-1", Type: "test-probe-type"}
	obs := newScheduleObserver(probe.Name, probe.Type)

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

	if firstDelay < 2 || firstDelay > 3 {
		t.Errorf("first collection delay = %vs, want about 2s since the injected process start", firstDelay)
	}

	lagWindow := agentstate.TakeCollectLagWindow("test")
	if lag, ok := lagWindow[probe]; !ok || lag <= 0 {
		t.Errorf("collect lag not recorded for probe %q: %v", probe, lagWindow)
	}

	obs.CallOverran()
	if got := agentstate.GetCollectOverruns()[probe]; got != 1 {
		t.Errorf("collect overruns for probe %q = %d, want 1", probe, got)
	}

	obs.CallStarted(0)
	obs.CallEnded(false)
	if after := agentstate.GetFirstCollectionDelays(); after[probe] != firstDelay {
		t.Errorf("non-first CallEnded recorded first collection delay: %f -> %f", firstDelay, after[probe])
	}
}
