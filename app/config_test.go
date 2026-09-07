package app

import (
	"os"
	"path/filepath"
	"senhub-agent.go/internal/agent/probes"
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
	if errs, _ := validateProbeParams("p", "zz-schema-check", map[string]interface{}{"mode": "zzz", "hots": "h"}); errs != 3 {
		t.Errorf("missing required + bad enum + unknown key: %d errors, want 3", errs)
	}
}
