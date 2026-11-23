//go:build linux

package app

import "testing"

func TestRunUpdateApplyRefusesArguments(t *testing.T) {
	for _, args := range [][]string{{"1.2.3"}, {"--version", "1.2.3"}, {"/etc/shadow"}} {
		if code := runUpdateApply(args); code != 2 {
			t.Errorf("runUpdateApply(%v) = %d, want 2", args, code)
		}
	}
}

func TestUpdateApplyIsAKnownInternalCommand(t *testing.T) {
	if _, ok := knownTopLevelArgs["update-apply"]; !ok {
		t.Fatal("update-apply missing from knownTopLevelArgs: Main would reject it as unknown")
	}
	if !linuxCommandNeedsRoot("update-apply") {
		t.Fatal("update-apply must be gated on root")
	}
}
