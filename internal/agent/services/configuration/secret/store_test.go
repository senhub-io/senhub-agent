package secret

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// xorCipher is a trivial reversible Cipher for exercising FileStore mechanics
// without a real OS backend. NOT a security primitive — tests only.
type xorCipher struct{}

func (xorCipher) Encrypt(p []byte) ([]byte, error) { return xorBytes(p), nil }
func (xorCipher) Decrypt(c []byte) ([]byte, error) { return xorBytes(c), nil }
func (xorCipher) Name() string                     { return "xor-test" }
func xorBytes(b []byte) []byte {
	out := make([]byte, len(b))
	for i := range b {
		out[i] = b[i] ^ 0x5a
	}
	return out
}

func TestFileStore_RoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "secrets.store")
	st := NewFileStore(path, xorCipher{})

	if err := st.Set("veeam-prod.password", New("h3llo$ecret")); err != nil {
		t.Fatal(err)
	}
	if err := st.Set("pg.password", New("pgpw")); err != nil {
		t.Fatal(err)
	}

	if v, err := st.Get("veeam-prod.password"); err != nil || v != "h3llo$ecret" {
		t.Errorf("Get = %q, %v", v, err)
	}

	// The on-disk file holds NO plaintext, and is 0600.
	raw, _ := os.ReadFile(path)
	if string(raw) == "" {
		t.Fatal("store file empty")
	}
	if got := string(raw); contains(got, "h3llo$ecret") {
		t.Errorf("plaintext leaked into the store file: %s", got)
	}
	if runtime.GOOS != "windows" {
		if fi, _ := os.Stat(path); fi.Mode().Perm() != 0o600 {
			t.Errorf("store file mode = %v, want 0600", fi.Mode().Perm())
		}
	}

	// Persistence across a fresh handle.
	st2 := NewFileStore(path, xorCipher{})
	names, _ := st2.List()
	if len(names) != 2 {
		t.Errorf("List after reopen = %v", names)
	}
	if err := st2.Delete("pg.password"); err != nil {
		t.Fatal(err)
	}
	if _, err := st2.Get("pg.password"); err == nil {
		t.Error("deleted secret still present")
	}
}

// TestFileStore_LoadCorruptStore covers the store-corruption UX (m9): a
// malformed store file must surface a parse error from Get/List, not masquerade
// as "not found" or an empty store.
func TestFileStore_LoadCorruptStore(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "secrets.store")
	if err := os.WriteFile(path, []byte("{ this is not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	st := NewFileStore(path, xorCipher{})
	if _, err := st.Get("anything"); err == nil {
		t.Error("Get on a corrupt store should error, not report not-found")
	}
	if _, err := st.List(); err == nil {
		t.Error("List on a corrupt store should surface the parse error")
	}
}

// TestFileStore_PreservesWhitespace pins the Provider.Get exact-bytes contract:
// a value with leading/trailing whitespace round-trips unchanged (the
// systemd-creds backend was fixed to match this).
func TestFileStore_PreservesWhitespace(t *testing.T) {
	dir := t.TempDir()
	st := NewFileStore(filepath.Join(dir, "s.store"), xorCipher{})
	const v = "  padded value\n"
	if err := st.Set("k", New(v)); err != nil {
		t.Fatal(err)
	}
	got, err := st.Get("k")
	if err != nil {
		t.Fatal(err)
	}
	if got != v {
		t.Errorf("Get = %q, want exact %q", got, v)
	}
}

func contains(haystack, needle string) bool {
	return len(haystack) >= len(needle) && (func() bool {
		for i := 0; i+len(needle) <= len(haystack); i++ {
			if haystack[i:i+len(needle)] == needle {
				return true
			}
		}
		return false
	})()
}
