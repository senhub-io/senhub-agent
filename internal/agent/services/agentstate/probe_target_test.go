package agentstate

import "testing"

func TestProbeRunState_CarriesTargetOutcome(t *testing.T) {
	defer SetActiveProbes(nil)

	SetActiveProbes([]string{"p1"})
	RecordProbeTarget("p1", true, "target_down")

	rs := GetProbeRunState("p1")
	if !rs.TargetDown {
		t.Errorf("TargetDown = false, want true")
	}
	if rs.TargetError != "target_down" {
		t.Errorf("TargetError = %q, want %q", rs.TargetError, "target_down")
	}
	if rs.Health != "unknown" {
		t.Errorf("Health = %q, want %q", rs.Health, "unknown")
	}
	if !rs.Running {
		t.Errorf("Running = false, want true")
	}

	RecordProbeTarget("p1", false, "x")
	rs = GetProbeRunState("p1")
	if rs.TargetDown {
		t.Errorf("TargetDown = true, want false")
	}
	if rs.TargetError != "" {
		t.Errorf("TargetError = %q, want empty", rs.TargetError)
	}
}
