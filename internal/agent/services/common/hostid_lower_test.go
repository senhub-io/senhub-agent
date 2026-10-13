package common

import "testing"

func TestLowerHostIDKeepsTheContractForm(t *testing.T) {
	cases := map[string]string{
		"ABCDEF01-2345-6789-ABCD-EF0123456789":    "abcdef01-2345-6789-abcd-ef0123456789",
		"abcdef01-2345-6789-abcd-ef0123456789":    "abcdef01-2345-6789-abcd-ef0123456789",
		" AbCdEf01-2345-6789-abcd-ef0123456789\n": "abcdef01-2345-6789-abcd-ef0123456789",
		"": "",
	}
	for in, want := range cases {
		if got := lowerHostID(in); got != want {
			t.Errorf("lowerHostID(%q) = %q, want %q", in, got, want)
		}
	}
}
