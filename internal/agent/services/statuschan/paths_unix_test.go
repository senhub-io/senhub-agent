//go:build !windows

package statuschan

import (
	"os"
	"path/filepath"
	"testing"
)

func TestCandidatePathsOrder(t *testing.T) {
	t.Setenv("STATE_DIRECTORY", "/run/state:/other")
	got := candidatePaths("/etc/x/agent.yaml")
	if len(got) < 3 || got[0] != "/run/state/control.sock" || got[len(got)-2] != "/etc/x/control.sock" || got[len(got)-1] != socketIn(fallbackDir()) {
		t.Errorf("paths = %v: state directory first, then the config directory, the private temp directory last", got)
	}
}

func TestFallbackDirMustBePrivate(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "senhub-agent")
	if err := ensurePrivateDir(dir); err != nil {
		t.Fatalf("a new directory must be accepted: %v", err)
	}
	if err := os.Chmod(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := ensurePrivateDir(dir); err == nil {
		t.Error("a directory others can enter must be refused")
	}
}
