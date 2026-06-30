//go:build windows

package secret

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDPAPIProvider_RoundTrip(t *testing.T) {
	dir := t.TempDir()

	p, err := NewDPAPIProvider(dir)
	if err != nil {
		t.Fatalf("NewDPAPIProvider: %v", err)
	}
	if p.Name() != "dpapi" {
		t.Errorf("Name = %q, want dpapi", p.Name())
	}

	const plaintext = "h3llo$ecret-dpapi"
	if err := p.Set("veeam-prod.password", New(plaintext)); err != nil {
		t.Fatalf("Set: %v", err)
	}

	got, err := p.Get("veeam-prod.password")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got != plaintext {
		t.Errorf("Get = %q, want %q", got, plaintext)
	}

	// The on-disk store must hold NO plaintext.
	raw, err := os.ReadFile(filepath.Join(dir, dpapiStoreFile))
	if err != nil {
		t.Fatalf("reading store: %v", err)
	}
	if len(raw) == 0 {
		t.Fatal("store file empty")
	}
	if contains(string(raw), plaintext) {
		t.Error("plaintext leaked into the DPAPI store file")
	}

	// Entropy must have been created and be the expected size.
	ent, err := os.ReadFile(filepath.Join(dir, entropyFile))
	if err != nil {
		t.Fatalf("reading entropy: %v", err)
	}
	if len(ent) != entropyLen {
		t.Errorf("entropy length = %d, want %d", len(ent), entropyLen)
	}
}

func TestDPAPIProvider_PersistAcrossFreshProvider(t *testing.T) {
	dir := t.TempDir()

	p1, err := NewDPAPIProvider(dir)
	if err != nil {
		t.Fatalf("NewDPAPIProvider #1: %v", err)
	}
	const plaintext = "persist-me-42"
	if err := p1.Set("pg.password", New(plaintext)); err != nil {
		t.Fatalf("Set: %v", err)
	}

	// A fresh provider over the same dir reuses the persisted entropy and store.
	p2, err := NewDPAPIProvider(dir)
	if err != nil {
		t.Fatalf("NewDPAPIProvider #2: %v", err)
	}
	got, err := p2.Get("pg.password")
	if err != nil {
		t.Fatalf("Get from fresh provider: %v", err)
	}
	if got != plaintext {
		t.Errorf("Get = %q, want %q", got, plaintext)
	}

	names, err := p2.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(names) != 1 || names[0] != "pg.password" {
		t.Errorf("List = %v, want [pg.password]", names)
	}
}
