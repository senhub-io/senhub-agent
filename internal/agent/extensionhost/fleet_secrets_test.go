package extensionhost

import (
	"errors"
	"slices"
	"strings"
	"testing"

	"senhub-agent.go/internal/agent/services/configuration/secret"
	"senhub-agent.go/internal/agent/services/extension"
)

func newTestFleetSecretStore() *fleetSecretStore {
	return &fleetSecretStore{p: secret.NewMemoryProvider()}
}

func TestFleetSecretStoreRoundTrip(t *testing.T) {
	store := newTestFleetSecretStore()

	// Operator secret, must remain invisible to List.
	if err := store.p.Set("db.password", secret.New("opsecret")); err != nil {
		t.Fatalf("set operator secret: %v", err)
	}

	// Set a fleet secret.
	if err := store.Set("pg", "fleetsupersecret"); err != nil {
		t.Fatalf("Set(fleet.pg): %v", err)
	}

	// Get it back.
	got, err := store.Get("pg")
	if err != nil {
		t.Fatalf("Get(fleet.pg): %v", err)
	}
	if got != "fleetsupersecret" {
		t.Errorf("Get(fleet.pg) = %q, want %q", got, "fleetsupersecret")
	}

	// List should contain only fleet secret names, without prefix.
	names, err := store.List()
	if err != nil {
		t.Fatalf("List(): %v", err)
	}
	want := []string{"pg"}
	if !slices.Equal(names, want) {
		t.Errorf("List() = %v, want %v", names, want)
	}

	// Delete and verify it's gone.
	if err := store.Delete("pg"); err != nil {
		t.Fatalf("Delete(fleet.pg): %v", err)
	}
	if _, err := store.Get("pg"); !errors.Is(err, extension.ErrFleetSecretNotFound) {
		t.Errorf("Get(deleted) error = %v, want ErrFleetSecretNotFound", err)
	}
}

func TestFleetSecretStoreGetNotFound(t *testing.T) {
	store := newTestFleetSecretStore()
	_, err := store.Get("does-not-exist")
	if !errors.Is(err, extension.ErrFleetSecretNotFound) {
		t.Errorf("Get(unknown) error = %v, want ErrFleetSecretNotFound", err)
	}
}

func TestFleetSecretStoreInvalidNames(t *testing.T) {
	store := newTestFleetSecretStore()

	invalid := []string{
		"",
		"Upper",
		"a/b",
		"../x",
		"fleet:x",
		strings.Repeat("a", 65),
	}
	for _, name := range invalid {
		if extension.ValidFleetSecretName(name) {
			t.Errorf("ValidFleetSecretName(%q) = true, want false", name)
		}
		if err := store.Set(name, "value"); err == nil {
			t.Errorf("Set(%q) expected error, got nil", name)
		}
		if _, err := store.Get(name); err == nil {
			t.Errorf("Get(%q) expected error, got nil", name)
		}
		if err := store.Delete(name); err == nil {
			t.Errorf("Delete(%q) expected error, got nil", name)
		}
	}

	// Names starting with underscore are allowed (reserved for agent).
	valid := "_agent.keys"
	if !extension.ValidFleetSecretName(valid) {
		t.Errorf("ValidFleetSecretName(%q) = false, want true", valid)
	}
	if err := store.Set(valid, "keydata"); err != nil {
		t.Errorf("Set(_agent.keys) unexpected error: %v", err)
	}
	if _, err := store.Get(valid); err != nil {
		t.Errorf("Get(_agent.keys) unexpected error: %v", err)
	}
}

func TestFleetSecretStoreSetErrorDoesNotLeakValue(t *testing.T) {
	store := newTestFleetSecretStore()
	const secretValue = "top-secret-value"
	err := store.Set("invalid/name", secretValue)
	if err == nil {
		t.Fatal("Set(invalid/name) expected error")
	}
	if strings.Contains(err.Error(), secretValue) {
		t.Errorf("error message %q contains secret value %q", err, secretValue)
	}
}
