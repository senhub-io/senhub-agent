//go:build linux

package secret

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSystemdCredentialDropIn(t *testing.T) {
	configDir := t.TempDir()

	// No store yet -> empty drop-in (caller removes any stale file).
	if body, err := SystemdCredentialDropIn(configDir); err != nil || body != "" {
		t.Fatalf("empty store: body=%q err=%v, want \"\", nil", body, err)
	}

	store := filepath.Join(configDir, credsStoreSubdir)
	if err := os.MkdirAll(store, 0o750); err != nil {
		t.Fatal(err)
	}
	for _, f := range []string{"pg-prod.password.cred", "cloud.bearer_token.cred", "notes.txt"} {
		if err := os.WriteFile(filepath.Join(store, f), []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	body, err := SystemdCredentialDropIn(configDir)
	if err != nil {
		t.Fatalf("SystemdCredentialDropIn: %v", err)
	}
	if !strings.HasPrefix(strings.TrimLeft(body, "#\n"), "") || !strings.Contains(body, "[Service]") {
		t.Errorf("drop-in missing [Service] section:\n%s", body)
	}
	// One directive per .cred, id==stem, absolute path, sorted, non-.cred ignored.
	wantLines := []string{
		"LoadCredentialEncrypted=cloud.bearer_token:" + filepath.Join(store, "cloud.bearer_token.cred"),
		"LoadCredentialEncrypted=pg-prod.password:" + filepath.Join(store, "pg-prod.password.cred"),
	}
	for _, w := range wantLines {
		if !strings.Contains(body, w) {
			t.Errorf("drop-in missing directive %q\n--- got ---\n%s", w, body)
		}
	}
	if strings.Contains(body, "notes.txt") {
		t.Errorf("non-.cred file leaked into drop-in:\n%s", body)
	}
	if n := strings.Count(body, "LoadCredentialEncrypted="); n != 2 {
		t.Errorf("expected 2 directives, got %d:\n%s", n, body)
	}
	// Sorted: cloud.* must come before pg-prod.*
	if strings.Index(body, "cloud.bearer_token") > strings.Index(body, "pg-prod.password") {
		t.Errorf("directives not sorted:\n%s", body)
	}
}
