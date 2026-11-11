//go:build !windows

package secret

import (
	"os"
	"path/filepath"
	"testing"
)

func TestInitRegistryFallsBackToStateDir(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root bypasses directory permission modes")
	}

	stateDir := t.TempDir()
	t.Setenv("SENHUB_STATE_DIR", stateDir)

	configDir := t.TempDir()
	if err := os.Chmod(configDir, 0500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(configDir, 0700) })

	SetProvider(nil)
	t.Cleanup(func() { SetProvider(nil) })

	if err := InitRegistry(configDir); err != nil {
		t.Fatalf("InitRegistry on read-only config dir: %v", err)
	}
	if got := StoreDir(); got != stateDir {
		t.Fatalf("StoreDir() = %q, want state dir %q", got, stateDir)
	}
	if _, err := os.Stat(filepath.Join(stateDir, "agent-secret.key")); err != nil {
		t.Fatalf("age key not created in state dir: %v", err)
	}
}

func TestInitRegistryPrefersExistingStateDirKey(t *testing.T) {
	stateDir := t.TempDir()
	t.Setenv("SENHUB_STATE_DIR", stateDir)
	configDir := t.TempDir()
	if _, err := NewAgeKeyfileProvider(filepath.Join(stateDir, "agent-secret.key"), filepath.Join(stateDir, "secrets.age")); err != nil {
		t.Fatalf("creating age key in state dir: %v", err)
	}
	SetProvider(nil)
	t.Cleanup(func() { SetProvider(nil) })
	if err := InitRegistry(configDir); err != nil {
		t.Fatalf("InitRegistry with existing state-dir key: %v", err)
	}
	if got := StoreDir(); got != stateDir {
		t.Fatalf("StoreDir() = %q, want state dir %q", got, stateDir)
	}
}

func TestInitRegistryKeepsWritableConfigDir(t *testing.T) {
	configDir := t.TempDir()

	SetProvider(nil)
	t.Cleanup(func() { SetProvider(nil) })

	if err := InitRegistry(configDir); err != nil {
		t.Fatalf("InitRegistry on writable config dir: %v", err)
	}
	if got := StoreDir(); got != configDir {
		t.Fatalf("StoreDir() = %q, want config dir %q", got, configDir)
	}
}
