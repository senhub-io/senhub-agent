package common

import "testing"

func TestDegenerateHostIDIsJudgedOnItsShape(t *testing.T) {
	for id, want := range map[string]bool{
		"01234567-89ab-cdef-0123-456789abcdef": true, // the documentation example a bench copied
		"00000000-0000-0000-0000-000000000000": true,
		"ffffffff-ffff-ffff-ffff-ffffffffffff": true,
		"6a6d1121-4a85-4e64-a222-746f7bc9c04c": false, // a real DMI UUID
		"7380f238-3651-4a38-a81d-0aa3a593128f": false,
		"not-an-id":                            false,
	} {
		if got := DegenerateHostID(id); got != want {
			t.Errorf("DegenerateHostID(%q) = %v, want %v", id, got, want)
		}
	}
}

func TestAConfiguredHostIDIsRecognised(t *testing.T) {
	t.Setenv("SENHUB_HOST_ID", "AABBCCDD-EEFF-0011-2233-445566778899")
	if !HostIDFromConfiguration("aabbccddeeff00112233445566778899") {
		t.Error("the id written from SENHUB_HOST_ID was not recognised, dashes and case aside")
	}
	if HostIDFromConfiguration("6a6d1121-4a85-4e64-a222-746f7bc9c04c") {
		t.Error("a derived id was reported as configured")
	}
}
