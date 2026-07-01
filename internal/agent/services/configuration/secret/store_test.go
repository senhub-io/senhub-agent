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
