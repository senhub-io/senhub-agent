//go:build !windows

package http

import (
	"os"
	"path/filepath"
	"testing"
)

const siteNagios = `version: "site-1"
checks:
  - name: cpu_user_check
    metrics:
      - channel: cpu_user
        warning: "80"
        critical: "90"
`

// `config check` run from a directory the service account cannot read
// reported "open config/nagios.yaml: permission denied" for a configuration
// that has no defect (#606). The file is looked for beside the
// configuration, and a working directory the caller cannot read is not a
// finding.
func TestCheckNagiosFile_FromAForeignWorkingDirectory(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root reads any directory")
	}
	confDir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(confDir, "config"), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(confDir, "config", "nagios.yaml"), []byte(siteNagios), 0o600); err != nil {
		t.Fatal(err)
	}

	foreign := t.TempDir()
	locked := filepath.Join(foreign, "config")
	if err := os.MkdirAll(locked, 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(locked, 0o000); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(locked, 0o750) })
	t.Chdir(foreign)

	report, found := CheckNagiosFile(filepath.Join(confDir, "agent.yaml"))
	if !found || report.Err != nil {
		t.Fatalf("found=%v err=%v, want the file under the configuration directory read cleanly", found, report.Err)
	}
	if want := filepath.Join(confDir, "config", "nagios.yaml"); report.Path != want {
		t.Errorf("read %s, want %s", report.Path, want)
	}
}

func TestCheckNagiosFile_NoFileAnywhereIsNotAProblem(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root reads any directory")
	}
	foreign := t.TempDir()
	locked := filepath.Join(foreign, "config")
	if err := os.MkdirAll(locked, 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(locked, 0o000); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(locked, 0o750) })
	t.Chdir(foreign)

	report, found := CheckNagiosFile(filepath.Join(t.TempDir(), "agent.yaml"))
	if found || report.Err != nil {
		t.Errorf("found=%v err=%v, want nothing reported", found, report.Err)
	}
}
