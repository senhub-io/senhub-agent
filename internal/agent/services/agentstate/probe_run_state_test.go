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
