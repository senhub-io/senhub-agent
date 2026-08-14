package auto_update

import (
	"strings"
	"testing"
)

// The security rule this whole change exists for: the Linux daemon never
// installs a binary. Exercised for every platform on every runner, because a
// rule that can only be tested where it applies is a rule nobody runs.
func TestSelfApplyRefusal(t *testing.T) {
	cases := []struct {
		name           string
		goos           string
		operatorDriven bool
		wantRefused    bool
	}{
		{"linux daemon must not install", "linux", false, true},
		{"linux operator CLI may install", "linux", true, false},
		{"windows daemon may install (LocalSystem, or msiexec)", "windows", false, false},
		{"windows operator CLI may install", "windows", true, false},
		{"darwin daemon may install", "darwin", false, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := selfApplyRefusalFor(tc.goos, tc.operatorDriven)
			if refused := got != ""; refused != tc.wantRefused {
				t.Errorf("refused = %v, want %v (message: %q)", refused, tc.wantRefused, got)
			}
		})
	}
}

// A refusal that does not say what to do instead is a dead end for whoever
// finds it in a journal at 2am: it is the only thing standing between them and
// an agent that looks stuck on an old version.
func TestSelfApplyRefusalTellsTheOperatorWhatToDo(t *testing.T) {
	msg := selfApplyRefusalFor("linux", false)
	if !strings.Contains(msg, "senhub-agent update") {
		t.Errorf("the refusal must name the command that does install; got %q", msg)
	}
}
