package http

import "testing"

func TestFormatCommitHashIgnoresAGInTheTagName(t *testing.T) {
	cases := map[string]string{
		"g302b166ab":         "302b166",
		"0.5.5-12-gd73e4556": "d73e4556"[:7],
		"backup/feat-probe-governance-0-gd73e4556":       "d73e455",
		"backup/feat-probe-governance-0-gd73e4556-dirty": "d73e455 (modified)",
	}
	for in, want := range cases {
		if got := formatCommitHash(in); got != want {
			t.Errorf("formatCommitHash(%q) = %q, want %q", in, got, want)
		}
	}
}
