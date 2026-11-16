package secret

import "testing"

func TestOperatorKeyNeverLandsInTheFleetPart(t *testing.T) {
	for in, want := range map[string]string{
		"fleet.password":      "local-fleet.password",
		"Fleet.password":      "local-Fleet.password",
		"fleet":               "local-fleet",
		"veeam-prod.password": "veeam-prod.password",
		"fleetwood.password":  "fleetwood.password",
	} {
		if got := OperatorKey(in); got != want {
			t.Errorf("OperatorKey(%q) = %q, want %q", in, got, want)
		}
	}
}
