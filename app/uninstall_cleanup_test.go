package app

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"senhub-agent.go/internal/agent/cliArgs"
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

// failingRemover stands in for a machine with no service unit: the
// removal fails, and the question is what happens to the files.
type failingRemover struct{ err error }

func (f failingRemover) Uninstall() error { return f.err }

// TestRemoveServiceCleansUpWhenTheUnitIsGone pins the #849 behaviour.
//
// Gating the file cleanup on the service removal meant `uninstall --yes`
// on a machine whose unit had already been removed printed nothing,
// removed nothing and exited 0 — leaving the sealed secret store on a
// machine the operator believed they had cleaned.
func TestRemoveServiceCleansUpWhenTheUnitIsGone(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "agent.yaml")
	if err := os.WriteFile(configPath, []byte("config_version: 3\n"), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	var out, errOut bytes.Buffer
	err := removeService(
		failingRemover{err: errors.New("no such unit")},
		&cliArgs.ParsedArgs{ConfigPath: configPath},
		&out, &errOut,
	)

	if err == nil {
		t.Error("a failed service removal must be reported to the caller, which turns it into a non-zero exit")
	}
	if !strings.Contains(errOut.String(), "no such unit") {
		t.Errorf("the failure must name what went wrong, got %q", errOut.String())
	}
	if _, statErr := os.Stat(configPath); !os.IsNotExist(statErr) {
		t.Error("the configuration survived an uninstall the operator confirmed")
	}
}

// TestRemoveServiceReportsSuccess keeps the nominal path honest: when
// the unit does go away, the operator is told so and nothing is
// reported on stderr.
func TestRemoveServiceReportsSuccess(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "agent.yaml")
	if err := os.WriteFile(configPath, []byte("config_version: 3\n"), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	var out, errOut bytes.Buffer
	if err := removeService(failingRemover{}, &cliArgs.ParsedArgs{ConfigPath: configPath}, &out, &errOut); err != nil {
		t.Fatalf("removeService: %v", err)
	}
	if !strings.Contains(out.String(), "Service uninstalled successfully") {
		t.Errorf("success must be stated, got %q", out.String())
	}
	if errOut.Len() != 0 {
		t.Errorf("nothing belongs on stderr on the nominal path, got %q", errOut.String())
	}
}
