package agentstate

import "testing"

func TestGetProbeRunState(t *testing.T) {
	SetActiveProbes([]string{"a", "b"})
	t.Cleanup(func() { SetActiveProbes(nil) })
	RecordProbeHealth("a", true)
	RecordProbeHealth("b", false)

	if st := GetProbeRunState("a"); !st.Running || st.Health != "ok" {
		t.Errorf("a = %+v, want running/ok", st)
	}
	if st := GetProbeRunState("b"); !st.Running || st.Health != "failed" {
		t.Errorf("b = %+v, want running/failed", st)
	}
	if st := GetProbeRunState("c"); st.Running || st.Health != "" {
		t.Errorf("c = %+v, want not running", st)
	}
	SetActiveProbes([]string{"d"})
	if st := GetProbeRunState("d"); !st.Running || st.Health != "unknown" {
		t.Errorf("d = %+v, want running/unknown before its first collect", st)
	}
}

func TestProbeRunStateCarriesTheLastError(t *testing.T) {
	SetActiveProbes([]string{"p1"})
	defer SetActiveProbes(nil)
	RecordProbeError("p1", "access denied")
	if tr := RecordProbeHealth("p1", false); tr != "failed" {
		t.Fatalf("first failure must be a transition, got %q", tr)
	}
	if st := GetProbeRunState("p1"); st.Health != "failed" || st.LastError != "access denied" {
		t.Fatalf("failed state must carry the reason, got %+v", st)
	}
	if tr := RecordProbeHealth("p1", false); tr != "" {
		t.Errorf("a repeated failure is not a transition, got %q", tr)
	}
	if tr := RecordProbeHealth("p1", true); tr != "recovered" {
		t.Errorf("the first success after a failure is a recovery, got %q", tr)
	}
	if st := GetProbeRunState("p1"); st.Health != "ok" || st.LastError != "" {
		t.Errorf("recovery must clear the reason, got %+v", st)
	}
}

func TestFailingExports(t *testing.T) {
	ResetExportActivityForTest()
	ResetEventsForTest()
	defer ResetExportActivityForTest()
	RecordExportSuccess("prtg")
	RecordExportFailure("otlp", "refused")
	RecordExportFailure("event", "refused")
	RecordExportSuccess("event")
	got := FailingExports()
	if len(got) != 1 || got[0] != "otlp" {
		t.Errorf("only otlp is failing, got %v", got)
	}
}

func TestProbeRunStateKeepsItsStartTimeAcrossSyncs(t *testing.T) {
	SetActiveProbes([]string{"s1"})
	t.Cleanup(func() { SetActiveProbes(nil) })
	first := GetProbeRunState("s1").StartedAt
	if first.IsZero() {
		t.Fatal("a running probe has no start time")
	}
	SetActiveProbes([]string{"s1", "s2"})
	if got := GetProbeRunState("s1").StartedAt; !got.Equal(first) {
		t.Errorf("a resync moved the start time of a probe still running: %v -> %v", first, got)
	}
	SetActiveProbes([]string{"s2"})
	if got := GetProbeRunState("s1").StartedAt; !got.IsZero() {
		t.Errorf("a stopped probe keeps a start time: %v", got)
	}
	SetActiveProbes([]string{"s1", "s2"})
	if got := GetProbeRunState("s1").StartedAt; got.Before(first) || got.IsZero() {
		t.Errorf("a restarted probe should carry its new start time, got %v", got)
	}
}

func TestProbeRunStateReportsEventDriven(t *testing.T) {
	SetActiveProbes([]string{"ev", "per"})
	MarkProbeEventDriven("ev")
	if !GetProbeRunState("ev").EventDriven || GetProbeRunState("per").EventDriven {
		t.Fatal("only the marked probe must be event-driven")
	}
	SetActiveProbes([]string{"per"})
	SetActiveProbes([]string{"ev"})
	if GetProbeRunState("ev").EventDriven {
		t.Error("the mark must be pruned once the probe is no longer active")
	}
	SetActiveProbes(nil)
}

func TestFirstCycleSignalClosesOnFirstCycleWhateverItDelivered(t *testing.T) {
	SetActiveProbes([]string{"p"})
	SetActiveProbeNames(map[string]string{"P": "p"})
	t.Cleanup(func() { SetActiveProbes(nil); SetActiveProbeNames(nil) })

	ch, ok := FirstCycleSignalByName("p")
	if !ok {
		t.Fatal("running probe must have a signal")
	}
	select {
	case <-ch:
		t.Fatal("signal closed before any cycle")
	default:
	}
	RecordProbeCycle("p", 0)
	select {
	case <-ch:
	default:
		t.Fatal("signal must close on a 0-point first cycle")
	}
	RecordProbeCycle("p", 3)
	late, _ := FirstCycleSignalByName("p")
	select {
	case <-late:
	default:
		t.Fatal("a signal requested after the first cycle must be closed")
	}
	if _, ok := FirstCycleSignalByName("nope"); ok {
		t.Error("unknown name must not resolve")
	}
}
