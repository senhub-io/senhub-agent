package hostiface

import "testing"

func TestDedupeOwnedMACs(t *testing.T) {
	candidates := []macOwnerCandidate{
		{name: "eth0", mac: "AA-BB-CC-DD-EE-FF"},
		{name: "eth1", mac: "11-22-33-44-55-66"},
		{name: "eth2", mac: "AA-BB-CC-DD-EE-FF"},
	}

	got := dedupeOwnedMACs(candidates)

	if _, ok := got["AA-BB-CC-DD-EE-FF"]; ok {
		t.Error("duplicate MAC must be dropped")
	}
	if got["11-22-33-44-55-66"] != "eth1" {
		t.Errorf("non-duplicate MAC owner = %q, want eth1", got["11-22-33-44-55-66"])
	}
}

func TestAllZeroMAC(t *testing.T) {
	if !allZeroMAC([]byte{0, 0, 0, 0, 0, 0}) {
		t.Error("all-zero MAC must be reported as zero")
	}
	if allZeroMAC([]byte{0, 0, 0, 0, 0, 1}) {
		t.Error("non-zero MAC must not be reported as zero")
	}
	if allZeroMAC(nil) {
		t.Error("empty MAC must not be reported as all-zero")
	}
}
