package secret

import (
	"errors"
	"testing"
)

// A first secret stored with the systemd-creds backend on a host whose
// secrets live in the age store must not orphan them: they stay readable,
// the new one is found too, and a delete removes a name from both.
func TestLayeredStoreKeepsTheOlderSecretsReachable(t *testing.T) {
	older := NewMemoryProvider()
	if err := older.Set("agent.key", New("k")); err != nil {
		t.Fatal(err)
	}
	newer := NewMemoryProvider()
	l := &layeredProvider{primary: newer, fallback: older}

	if err := l.Set("lab.creds_test", New("v")); err != nil {
		t.Fatal(err)
	}
	if v, err := l.Get("agent.key"); err != nil || v != "k" {
		t.Errorf("a secret of the older store = %q, %v", v, err)
	}
	if v, err := l.Get("lab.creds_test"); err != nil || v != "v" {
		t.Errorf("the new secret = %q, %v", v, err)
	}
	if names, _ := l.List(); len(names) != 2 {
		t.Errorf("List = %v, want both stores", names)
	}
	if err := l.Delete("agent.key"); err != nil {
		t.Fatal(err)
	}
	if _, err := l.Get("agent.key"); !errors.Is(err, ErrNotFound) {
		t.Errorf("a deleted secret came back from the other store: %v", err)
	}
}
