package app

import (
	"os"
	"path/filepath"
	"testing"
)

// TestCleanupTargetsWholeInstalledConfigDir pins what an operator gets
// when they answer "yes".
//
// Before #841 only agent.yaml was removed, so probes.d/, strategies.d/
// and — the part that matters — the sealed secret store survived an
// explicit uninstall, under a line reading "Cleanup completed". Sealed
// credentials outliving the machine's purpose is not what "uninstall"
// means.
func TestCleanupTargetsWholeInstalledConfigDir(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "agent.yaml")

	for _, rel := range []string{
		"agent.yaml",
		"probes.d/10-host.yaml",
		"strategies.d/00-http.yaml",
		"secrets.dpapi",
		"entropy.bin",
	} {
		full := filepath.Join(dir, rel)
		if err := os.MkdirAll(filepath.Dir(full), 0o750); err != nil {
			t.Fatalf("mkdir: %v", err)
		}
		if err := os.WriteFile(full, []byte("x"), 0o600); err != nil {
			t.Fatalf("write %s: %v", rel, err)
		}
	}

	files, dirs := cleanupTargets(configPath, true)

	if len(dirs) == 0 {
		t.Fatal("no directory targeted — the installed layout would survive uninstall")
	}
	found := false
	for _, d := range dirs {
		if d == dir {
			found = true
		}
	}
	if !found {
		t.Errorf("the installed configuration directory %q is not targeted; dirs=%v files=%v", dir, dirs, files)
	}
}

// TestCleanupSparesAnOperatorDirectory is the other half. A config
// passed with an explicit --config-path can sit next to files the agent
// does not own — a home directory, a shared /etc. Removing its parent
// would take them, so only the config file goes.
func TestCleanupSparesAnOperatorDirectory(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "agent.yaml")
	neighbour := filepath.Join(dir, "something-else.conf")

	for _, f := range []string{configPath, neighbour} {
		if err := os.WriteFile(f, []byte("x"), 0o600); err != nil {
			t.Fatalf("write: %v", err)
		}
	}

	files, dirs := cleanupTargets(configPath, false)

	for _, d := range dirs {
		if d == dir {
			t.Fatalf("a non-installed directory %q was targeted for removal — it may hold files the agent does not own", dir)
		}
	}
	if len(files) != 1 || files[0] != configPath {
		t.Errorf("files = %v, want just the config file", files)
	}
}
