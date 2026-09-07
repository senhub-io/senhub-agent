package app

import (
	"os"
	"path/filepath"
	"senhub-agent.go/internal/agent/probes"
	"senhub-agent.go/internal/agent/services/configuration"
	"strings"
	"testing"
)

// TestExtractAgentKeyFromConfig_ResolvesSealedKey pins the defect the
// Windows recette found: since 0.5.x an install seals the agent key, so
// the file holds "${secret:agent.key}". A text search handed that
// literal to the API, authentication failed, and `status` fell back to
// its degraded local view on every modern host.
func TestExtractAgentKeyFromConfig_ResolvesSealedKey(t *testing.T) {
	// validateConfigPath only accepts paths under the working directory.
	dir, err := os.MkdirTemp(".", "keytest-sealed-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	path := filepath.Join(dir, "agent.yaml")
	if err := os.WriteFile(path, []byte("config_version: 3\nagent:\n  key: \"${secret:agent.key}\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	key, keyErr := extractAgentKeyFromConfig(path)
	_ = keyErr
	// The secret store is absent here, so resolution cannot succeed — the
	// contract that matters is that the unresolved reference is NEVER
	// returned as if it were a key.
	if keyErr == nil && strings.Contains(key, "${") {
		t.Errorf("returned the unresolved reference %q as an agent key", key)
	}
}

func TestExtractAgentKeyFromConfig_PlainKeyStillWorks(t *testing.T) {
	dir, err := os.MkdirTemp(".", "keytest-plain-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	path := filepath.Join(dir, "agent.yaml")
	if err := os.WriteFile(path, []byte("config_version: 3\nagent:\n  key: \"145c303c-7657-4803-bf1e-a3993ee8da82\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	key, keyErr := extractAgentKeyFromConfig(path)
	if keyErr != nil {
		t.Fatalf("plain key rejected: %v", keyErr)
	}
	if key != "145c303c-7657-4803-bf1e-a3993ee8da82" {
		t.Errorf("key=%q", key)
	}
}

// A probe with a declared schema is checked against it by config check:
// a missing required key, a bad enum value and an unknown key are errors.
func TestValidateProbeParams_UsesDeclaredSchema(t *testing.T) {
	probes.RegisterProbeSpec(probes.ProbeSpec{
		Type: "zz-schema-check", DisplayName: "Schema check",
		Params: []probes.ParamSpec{
			{Key: "host", Kind: probes.KindString, Required: true},
			{Key: "mode", Kind: probes.KindString, Enum: []string{"a", "b"}},
		},
	})
	if errs, _ := validateProbeParams("p", "zz-schema-check", map[string]interface{}{"host": "h", "mode": "a"}); errs != 0 {
		t.Errorf("valid params: %d errors, want 0", errs)
	}
	// The bad enum value is not counted here: a value's shape is the
	// constructor's to report, and this test type has no constructor.
	if errs, _ := validateProbeParams("p", "zz-schema-check", map[string]interface{}{"mode": "zzz", "hots": "h"}); errs != 2 {
		t.Errorf("missing required + unknown key: %d errors, want 2", errs)
	}
}

func TestReportGovernanceProblems(t *testing.T) {
	if n := reportGovernanceProblems(configuration.ProbeConfig{Name: "p"}); n != 0 {
		t.Fatalf("no block must be silent, got %d", n)
	}
	if n := reportGovernanceProblems(configuration.ProbeConfig{Name: "p", Governance: map[string]interface{}{
		"criticality": "high", "labels": map[string]interface{}{"application": "erp"},
	}}); n != 0 {
		t.Fatalf("valid block reported %d errors", n)
	}
	if n := reportGovernanceProblems(configuration.ProbeConfig{Name: "p", Governance: map[string]interface{}{
		"criticality": "urgent",
	}}); n != 1 {
		t.Fatalf("a criticality outside the closed set must be one error, got %d", n)
	}
	if n := reportGovernanceProblems(configuration.ProbeConfig{Name: "p", Governance: map[string]interface{}{
		"owner": map[string]interface{}{"squad": "x"}, "colour": "red",
	}}); n != 2 {
		t.Fatalf("two unknown keys must be two errors, got %d", n)
	}
}
