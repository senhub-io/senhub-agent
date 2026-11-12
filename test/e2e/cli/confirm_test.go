//go:build e2e

package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLicenseRemoveWithoutATerminalFails(t *testing.T) {
	t.Parallel()
	// ent#922, 0.7.0-rc.5: non-interactive license remove asks for --force, not --yes.
	dir := t.TempDir()
	configPath := filepath.Join(dir, "agent.yaml")
	if err := os.WriteFile(configPath, []byte("config_version: 3\nagent:\n  name: e2e\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	stdout, stderr, code := run(t, bin, "license", "remove", "--config-path", configPath)
	_ = stdout
	if code == 0 {
		t.Fatal("expected non-zero exit without a terminal")
	}
	if !strings.Contains(stderr, "rerun with --force") {
		t.Fatalf("stderr missing --force hint:\n%s", stderr)
	}
	if strings.Contains(stderr, "--yes") {
		t.Fatalf("stderr must not suggest --yes:\n%s", stderr)
	}
}

func TestUninstallWithoutATerminalFails(t *testing.T) {
	t.Parallel()
	// ent#922, 0.7.0-rc.5: non-interactive uninstall requires --yes.
	stdout, stderr, code := run(t, bin, "uninstall")
	out := stdout + stderr
	if code == 0 {
		t.Fatal("expected non-zero exit without a terminal")
	}
	if !strings.Contains(out, "confirmation required: rerun with --yes") {
		t.Fatalf("output missing confirmation message:\n%s", out)
	}
}

func TestHelpDoesNotAct(t *testing.T) {
	t.Parallel()
	// rc.1, -h: help output must not trigger confirmation or root guards.
	for _, args := range [][]string{
		{"install", "-h"},
		{"uninstall", "--help"},
		{"update", "-h"},
		{"license", "remove", "-h"},
		{"refresh-unit", "-h"},
	} {
		stdout, stderr, code := run(t, bin, args...)
		out := stdout + stderr
		if code != 0 {
			t.Fatalf("%v: expected zero exit, got %d:\n%s", args, code, out)
		}
		if strings.Contains(out, "confirmation required") {
			t.Fatalf("%v: help must not ask for confirmation:\n%s", args, out)
		}
		if strings.Contains(out, "must be run with root") {
			t.Fatalf("%v: help must not require root:\n%s", args, out)
		}
	}
}
