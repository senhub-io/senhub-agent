//go:build linux

package secret

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestSystemdCredsGetReadsCredential(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("CREDENTIALS_DIRECTORY", dir)

	const name = "veeam-prod.password"
	const plaintext = "decrypted-by-systemd"
	if err := os.WriteFile(filepath.Join(dir, SanitizeKey(name)), []byte(plaintext+"\n"), 0o600); err != nil {
		t.Fatalf("seeding credential: %v", err)
	}

	p := &systemdCredsProvider{}
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
