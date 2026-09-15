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
