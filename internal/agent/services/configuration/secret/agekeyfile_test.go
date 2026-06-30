package secret

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestAgeKeyfileGeneratesKeyOnFirstUse(t *testing.T) {
	dir := t.TempDir()
	keyPath := filepath.Join(dir, "agent-secret.key")
	storePath := filepath.Join(dir, "secrets.age")

	if _, err := os.Stat(keyPath); !os.IsNotExist(err) {
		t.Fatalf("key file should not exist before first use")
	}

	p, err := NewAgeKeyfileProvider(keyPath, storePath)
	if err != nil {
		t.Fatalf("NewAgeKeyfileProvider: %v", err)
	}
	if p.Name() != "age-keyfile" {
		t.Fatalf("Name() = %q, want age-keyfile", p.Name())
	}

	info, err := os.Stat(keyPath)
	if err != nil {
		t.Fatalf("key file not created: %v", err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Fatalf("key file perm = %o, want 600", perm)
	}
	data, err := os.ReadFile(keyPath)
	if err != nil {
		t.Fatalf("reading key file: %v", err)
	}
	if !strings.Contains(string(data), "AGE-SECRET-KEY-1") {
		t.Fatalf("key file does not contain an age secret key")
	}
}

func TestAgeKeyfileRoundTripAndNoPlaintext(t *testing.T) {
	dir := t.TempDir()
	keyPath := filepath.Join(dir, "agent-secret.key")
	storePath := filepath.Join(dir, "secrets.age")

	p, err := NewAgeKeyfileProvider(keyPath, storePath)
	if err != nil {
		t.Fatalf("NewAgeKeyfileProvider: %v", err)
	}

	const name = "veeam-prod.password"
	const plaintext = "s3cr3t-P@ssw0rd-unlikely-string"
	if err := p.Set(name, New(plaintext)); err != nil {
		t.Fatalf("Set: %v", err)
	}

	got, err := p.Get(name)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got != plaintext {
		t.Fatalf("Get = %q, want %q", got, plaintext)
	}

	store, err := os.ReadFile(storePath)
	if err != nil {
		t.Fatalf("reading store: %v", err)
	}
	if strings.Contains(string(store), plaintext) {
		t.Fatalf("store file contains plaintext secret")
	}

	names, err := p.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(names) != 1 || names[0] != name {
		t.Fatalf("List = %v, want [%q]", names, name)
	}

	if _, err := p.Get("absent.password"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Get(absent) error = %v, want ErrNotFound", err)
	}
}

func TestAgeKeyfilePersistsAcrossFreshProvider(t *testing.T) {
	dir := t.TempDir()
	keyPath := filepath.Join(dir, "agent-secret.key")
	storePath := filepath.Join(dir, "secrets.age")

	p1, err := NewAgeKeyfileProvider(keyPath, storePath)
	if err != nil {
		t.Fatalf("NewAgeKeyfileProvider(1): %v", err)
	}
	const name = "citrix-1.director.auth.password"
	const plaintext = "another-distinct-secret-value"
	if err := p1.Set(name, New(plaintext)); err != nil {
		t.Fatalf("Set: %v", err)
	}

	// A fresh provider must reuse the existing key file (not regenerate it) and
	// decrypt the existing store.
	p2, err := NewAgeKeyfileProvider(keyPath, storePath)
	if err != nil {
		t.Fatalf("NewAgeKeyfileProvider(2): %v", err)
	}
	got, err := p2.Get(name)
	if err != nil {
		t.Fatalf("Get after reload: %v", err)
	}
	if got != plaintext {
		t.Fatalf("Get after reload = %q, want %q", got, plaintext)
	}
}
