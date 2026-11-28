package agentstate

import "testing"

func TestGetProbeTargetDownCount(t *testing.T) {
	SetActiveProbes([]string{"a", "b", "c"})
	t.Cleanup(func() {
		SetActiveProbes(nil)
	})

	RecordProbeTarget("a", true, "timeout")
	RecordProbeTarget("b", false, "")
	RecordProbeTarget("z", true, "timeout") // z is not active, must not count.

	if got := GetProbeTargetDownCount(); got != 1 {
		t.Fatalf("GetProbeTargetDownCount() = %d, want 1", got)
	}
}
