package configuration

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestApplyInstallOverrides(t *testing.T) {
	dir := t.TempDir()
	cfg := filepath.Join(dir, "agent.yaml")
	// A minimal generated-style agent.yaml with an agent block and comments.
	seed := `# SenHub Agent — global configuration
config_version: 2

agent:
  key: 11111111-2222-3333-4444-555555555555
  # license: ""  # add your license token here
`
	if err := os.WriteFile(cfg, []byte(seed), 0o600); err != nil {
		t.Fatal(err)
	}

	if err := ApplyInstallOverrides(cfg, "eyJ-fake-jwt", map[string]string{"site": "paris", "env": "prod"}); err != nil {
		t.Fatalf("ApplyInstallOverrides: %v", err)
	}

	// Re-load through the real loader: license + global_tags must be set.
	after, err := LoadFromDisk(cfg, nil)
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	if after.Agent.License != "eyJ-fake-jwt" {
		t.Errorf("license = %q, want the provisioned token", after.Agent.License)
	}
	if after.Agent.GlobalTags["site"] != "paris" || after.Agent.GlobalTags["env"] != "prod" {
		t.Errorf("global_tags = %v, want site=paris env=prod", after.Agent.GlobalTags)
	}
	// The agent key must be preserved, not clobbered.
	if after.Agent.Key != "11111111-2222-3333-4444-555555555555" {
		t.Errorf("agent key changed to %q", after.Agent.Key)
	}
	// A template comment survives the node-level edit.
	raw, _ := os.ReadFile(cfg)
	if !strings.Contains(string(raw), "SenHub Agent — global configuration") {
		t.Errorf("header comment lost:\n%s", raw)
	}

	// Empty inputs are a no-op (no error, file unchanged in content shape).
	if err := ApplyInstallOverrides(cfg, "", nil); err != nil {
		t.Fatalf("no-op ApplyInstallOverrides: %v", err)
	}
}
