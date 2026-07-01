//go:build linux

package secret

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

// TestSystemdCredsGetReadsRuntimeCredential covers the non-root daemon path:
// the value systemd decrypted into $CREDENTIALS_DIRECTORY/<key> is returned with
// trailing whitespace trimmed, and an absent name (in both the runtime mount and
// the persistent store) yields ErrNotFound. No systemd-creds binary is needed.
func TestSystemdCredsGetReadsRuntimeCredential(t *testing.T) {
	runtimeDir := t.TempDir()
	configDir := t.TempDir()
	t.Setenv("CREDENTIALS_DIRECTORY", runtimeDir)

	const name = "veeam-prod.password"
	const plaintext = "decrypted-by-systemd"
	if err := os.WriteFile(filepath.Join(runtimeDir, SanitizeKey(name)), []byte(plaintext+"\n"), 0o600); err != nil {
		t.Fatalf("seeding credential: %v", err)
	}

	p := newSystemdCredsProvider(configDir)
	if p.Name() != "systemd-creds" {
		t.Fatalf("Name() = %q, want systemd-creds", p.Name())
	}

	got, err := p.Get(name)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got != plaintext {
		t.Fatalf("Get = %q, want %q (trailing whitespace must be trimmed)", got, plaintext)
	}

	if _, err := p.Get("absent.password"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Get(absent) error = %v, want ErrNotFound", err)
	}
}

// TestSystemdCredsListReadsPersistentStore confirms List enumerates the
// persistent creds.d/ store (the admin path, no $CREDENTIALS_DIRECTORY) by
// stripping the .cred suffix, and ignores non-.cred entries.
func TestSystemdCredsListReadsPersistentStore(t *testing.T) {
	configDir := t.TempDir()
	store := filepath.Join(configDir, credsStoreSubdir)
	if err := os.MkdirAll(store, 0o750); err != nil {
		t.Fatal(err)
	}
	for _, f := range []string{"pg-prod.password.cred", "cloud.bearer_token.cred", "README", "notes.txt"} {
		if err := os.WriteFile(filepath.Join(store, f), []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	p := newSystemdCredsProvider(configDir)
	names, err := p.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	want := map[string]bool{"pg-prod.password": true, "cloud.bearer_token": true}
	if len(names) != len(want) {
		t.Fatalf("List = %v, want keys %v", names, want)
	}
	for _, n := range names {
		if !want[n] {
			t.Errorf("unexpected key %q in List (non-.cred files must be ignored)", n)
		}
	}

	// List on a config dir with no store is empty, not an error.
	if n, err := newSystemdCredsProvider(t.TempDir()).List(); err != nil || len(n) != 0 {
		t.Errorf("List(no store) = %v, %v; want [], nil", n, err)
	}
}
