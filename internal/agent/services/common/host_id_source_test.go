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

// TestEffectiveHostIDHonoursTheVariable checks SENHUB_HOST_ID wins over the
// derived identity in the binary itself, rendered as the derivation renders
// a machine id, and that an unusable value falls back to the derived one.
func TestEffectiveHostIDHonoursTheVariable(t *testing.T) {
	derived := "ABA6731F-5D1A-4820-8CA1-AD4BC653533D"
	cases := []struct {
		env, want string
	}{
		{"", "aba6731f-5d1a-4820-8ca1-ad4bc653533d"},
		{"3f2a9c7e1b5d48e6a0c4f7b29d18e5a3", "3f2a9c7e-1b5d-48e6-a0c4-f7b29d18e5a3"},
		{"3F2A9C7E-1B5D-48E6-A0C4-F7B29D18E5A3", "3f2a9c7e-1b5d-48e6-a0c4-f7b29d18e5a3"},
		{"not-a-machine-id", "aba6731f-5d1a-4820-8ca1-ad4bc653533d"},
		{"00000000000000000000000000000000", "aba6731f-5d1a-4820-8ca1-ad4bc653533d"},
		{"0123456789abcdef0123456789abcdef", "aba6731f-5d1a-4820-8ca1-ad4bc653533d"},
	}
	for _, c := range cases {
		t.Setenv("SENHUB_HOST_ID", c.env)
		if got := effectiveHostID(derived); got != c.want {
			t.Errorf("SENHUB_HOST_ID=%q: effectiveHostID = %q, want %q", c.env, got, c.want)
		}
	}
	t.Setenv("SENHUB_HOST_ID", "3f2a9c7e1b5d48e6a0c4f7b29d18e5a3")
	if !HostIDFromConfiguration(effectiveHostID(derived)) {
		t.Error("the configured identity must be recognised as configured")
	}
}
