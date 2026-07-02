package configuration

import (
	"path/filepath"
	"testing"

	"senhub-agent.go/internal/agent/services/configuration/secret"
)

// TestLoadForShow_SetsSecretConfigDir pins the `config show` panic fix: the
// show load path must record the configuration directory (as the boot loader
// does) so a ${secret:} reference resolved during --resolved/--redact finds the
// OS-native secret backend. Before the fix, loadMerged never called
// secret.SetConfigDir and a SEALED install crashed on `config show --redact`.
func TestLoadForShow_SetsSecretConfigDir(t *testing.T) {
	// Point the registry at a sentinel dir first. The show load must OVERRIDE
	// it with the config's own directory; if it doesn't, the sentinel survives
	// and the assertion below fails (which is the old, buggy behaviour).
	secret.SetConfigDir(filepath.Join(t.TempDir(), "sentinel-should-be-overridden"))

	dir := t.TempDir()
	configPath := filepath.Join(dir, "agent.yaml")
	writeFile(t, configPath, "config_version: 2\nagent:\n  key: \"k1\"\n")

	if _, err := LoadForShow(configPath, ShowRedact, loaderTestLogger(t)); err != nil {
		t.Fatalf("LoadForShow: %v", err)
	}

	if got := secret.ConfigDir(); got != dir {
		t.Errorf("secret config dir = %q, want %q (loadMerged must record the config dir for ${secret:} resolution)", got, dir)
	}
}
