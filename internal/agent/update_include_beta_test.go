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

// The spellings of a boolean that YAML 1.1 accepts, and the ones it refuses,
// must read the same whichever library parses the file.
func TestReadIncludeBetaFromConfig_Spellings(t *testing.T) {
	cases := []struct {
		value string
		want  bool
	}{
		{"true", true}, {"True", true}, {"yes", true}, {"Yes", true}, {"on", true}, {"y", true},
		{"false", false}, {"no", false}, {"off", false}, {"n", false},
		{`"yes"`, false}, {`"true"`, false}, {"1", false}, {"maybe", false},
	}
	for _, tc := range cases {
		t.Run(tc.value, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "agent.yaml")
			body := "auto_update:\n  include_beta: " + tc.value + "\n"
			if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
				t.Fatal(err)
			}
			if got := readIncludeBetaFromConfig(path); got != tc.want {
				t.Errorf("include_beta: %s read as %v, want %v", tc.value, got, tc.want)
			}
		})
	}
}
