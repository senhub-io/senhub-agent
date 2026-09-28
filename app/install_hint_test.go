package app

import (
	"os"
	"strings"
	"testing"
)

func TestInstalledHintNamesTheSystemBinary(t *testing.T) {
	got := installedHint("/usr/local/bin/senhub-agent")
	if !strings.Contains(got, "sudo /usr/local/bin/senhub-agent start") {
		t.Errorf("hint does not name the installed binary by its full path:\n%s", got)
	}
	if strings.Contains(got, os.Args[0]) {
		t.Errorf("hint repeats the installer's own path %q:\n%s", os.Args[0], got)
	}
}

func TestInstalledHintFallsBackToTheInvocationPath(t *testing.T) {
	if got := installedHint(""); !strings.Contains(got, os.Args[0]+" start") {
		t.Errorf("without an installed path the hint must use the invocation path:\n%s", got)
	}
}
