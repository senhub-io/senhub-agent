package app

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func writeNagios(t *testing.T, body string) string {
	t.Helper()
	dir := t.TempDir()
	cfg := filepath.Join(dir, "agent.yaml")
	if err := os.WriteFile(filepath.Join(dir, "nagios.yaml"), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return cfg
}

// config check reads nagios.yaml the way the agent will: a file the
// loader refuses is an error, a check that can only answer UNKNOWN here
// is a warning, and no file at all is nothing to report.
func TestConfigCheckReportsTheNagiosFile(t *testing.T) {
	bad := writeNagios(t, "version: \"1\"\nchecks:\n  - name: disk\n    metrics:\n      - channel: fs_used_percent\n        critcal: \"90\"\n")
	if e, _ := reportNagiosFile(bad, 0, 0); e != 1 {
		t.Errorf("a misspelt key counted %d errors, want 1", e)
	}

	other := "disk_free_percent" // Windows only
	if runtime.GOOS == "windows" {
		other = "fs_used_percent" // Linux and macOS only
	}
	good := writeNagios(t, "version: \"1\"\nchecks:\n  - name: disk\n    metrics:\n      - channel: "+other+"\n        warning: \"80\"\n        critical: \"90\"\n")
	if e, w := reportNagiosFile(good, 0, 0); e != 0 || w != 1 {
		t.Errorf("a metric of another platform gave %d errors and %d warnings, want 0 and 1", e, w)
	}

	if e, w := reportNagiosFile(filepath.Join(t.TempDir(), "agent.yaml"), 0, 0); e != 0 || w != 0 {
		t.Errorf("no nagios.yaml gave %d errors and %d warnings, want nothing", e, w)
	}
}
