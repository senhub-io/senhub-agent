package cliexit

import "testing"

func TestCodesAreStable(t *testing.T) {
	want := map[string]struct{ got, code int }{
		"ok":        {OK, 0},
		"warning":   {Warning, 1},
		"failure":   {Failure, 2},
		"unchanged": {Unchanged, 3},
	}
	for name, c := range want {
		if c.got != c.code {
			t.Errorf("%s = %d, the published contract is %d", name, c.got, c.code)
		}
		if Name(c.got) != name {
			t.Errorf("Name(%d) = %q, want %q", c.got, Name(c.got), name)
		}
	}
	if Name(42) != "unknown" {
		t.Errorf("Name(42) = %q, want unknown", Name(42))
	}
}
