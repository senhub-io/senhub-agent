package app

import "testing"

// answerIsYes gates every destructive-command confirmation (uninstall,
// secret rm). The safety-critical case is the empty answer: EOF or a
// non-TTY stdin must NOT be read as consent.
func TestAnswerIsYes(t *testing.T) {
	cases := []struct {
		name   string
		answer string
		want   bool
	}{
		{"lowercase y", "y", true},
		{"uppercase Y", "Y", true},
		{"padded y", "  y  ", true},
		{"empty aborts", "", false},
		{"whitespace only aborts", "   ", false},
		{"yes word aborts", "yes", false},
		{"n aborts", "n", false},
		{"N aborts", "N", false},
		{"garbage aborts", "maybe", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := answerIsYes(tc.answer); got != tc.want {
				t.Errorf("answerIsYes(%q) = %v, want %v", tc.answer, got, tc.want)
			}
		})
	}
}
