package agent

import (
	"os"
	"path/filepath"
	"testing"
)

// TestReadIncludeBetaFromConfig checks the CLI reads include_beta from the
// agent.yaml it is given, the same file the daemon announces betas from.
func TestReadIncludeBetaFromConfig(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "agent.yaml")
	if err := os.WriteFile(path, []byte("agent:\n  key: k\nauto_update:\n  enabled: true\n  include_beta: true\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if !readIncludeBetaFromConfig(path) {
		t.Error("include_beta: true in agent.yaml was not read")
	}
	if readIncludeBetaFromConfig(filepath.Join(dir, "missing.yaml")) {
		t.Error("a missing config must read as false")
	}
}
