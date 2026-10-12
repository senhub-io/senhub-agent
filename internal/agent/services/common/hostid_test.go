package common

import "testing"

func TestCanonicalHostID(t *testing.T) {
	const want = "0123abcd-4567-89ef-0123-456789abcdef"
	for _, in := range []string{
		"0123abcd456789ef0123456789abcdef",
		"0123ABCD456789EF0123456789ABCDEF",
		"0123ABCD-4567-89EF-0123-456789ABCDEF",
		want,
		"{" + want + "}",
		"  " + want + "\n",
	} {
		got, ok := CanonicalHostID(in)
		if !ok || got != want {
			t.Errorf("CanonicalHostID(%q) = %q, %v; want %q, true", in, got, ok, want)
		}
	}
}

func TestCanonicalHostIDLeavesOtherValuesUntouched(t *testing.T) {
	for _, in := range []string{"", "my-host", "0123abcd456789ef0123456789abcde", "zzzzabcd456789ef0123456789abcdef", "01-23abcd456789ef0123456789abcdef"} {
		got, ok := CanonicalHostID(in)
		if ok || got != in {
			t.Errorf("CanonicalHostID(%q) = %q, %v; want unchanged, false", in, got, ok)
		}
	}
}
