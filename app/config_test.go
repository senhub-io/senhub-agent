package app

import (
	"os"
	"path/filepath"
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
