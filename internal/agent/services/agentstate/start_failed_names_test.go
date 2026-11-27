package agentstate

import "testing"

func TestStartFailedProbeNames(t *testing.T) {
	t.Cleanup(func() { SetStartFailedProbeNames(nil) })

	SetStartFailedProbeNames([]string{"Citrix-Prod", ""})
	if !ProbeStartFailedByName("citrix-prod") {
		t.Fatal("ProbeStartFailedByName(\"citrix-prod\") = false, want true")
	}
	if ProbeStartFailedByName("other") {
		t.Fatal("ProbeStartFailedByName(\"other\") = true, want false")
	}

	SetStartFailedProbeNames(nil)
	if ProbeStartFailedByName("citrix-prod") {
		t.Fatal("ProbeStartFailedByName(\"citrix-prod\") after reset = true, want false")
	}
}
