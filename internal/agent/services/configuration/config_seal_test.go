package configuration

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"senhub-agent.go/internal/agent/services/configuration/secret"
)

func writeSealFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestSealInlineSecrets_MultiFile(t *testing.T) {
	mp := secret.NewMemoryProvider()
	secret.SetProvider(mp)
	t.Cleanup(func() { secret.SetProvider(nil) })

	dir := t.TempDir()
	cfg := filepath.Join(dir, "agent.yaml")
	writeSealFile(t, cfg, "config_version: 2\n")
	probe := filepath.Join(dir, "probes.d", "10-veeam.yaml")
	writeSealFile(t, probe, `- name: veeam-prod
  type: veeam
  params:
    endpoint: https://vbr:9419
    username: svc            # not a secret
    password: my-plaintext-pw
    director:
      auth:
        password: nested-pw
`)

	if err := SealInlineSecrets(cfg, nil); err != nil {
		t.Fatalf("SealInlineSecrets: %v", err)
	}

	// Both secrets are now in the store, keyed by instance.
	names, _ := mp.List()
	got := map[string]bool{}
	for _, n := range names {
		got[n] = true
	}
	for _, want := range []string{"veeam-prod.password", "veeam-prod.director.auth.password"} {
		if !got[want] {
			t.Errorf("store missing key %q (have %v)", want, names)
		}
	}
	if v, _ := mp.Get("veeam-prod.password"); v != "my-plaintext-pw" {
		t.Errorf("stored value = %q", v)
	}

	// The file no longer holds plaintext; it holds references.
	raw, _ := os.ReadFile(probe)
	if strings.Contains(string(raw), "my-plaintext-pw") || strings.Contains(string(raw), "nested-pw") {
		t.Errorf("plaintext survived in the file:\n%s", raw)
	}
	if !strings.Contains(string(raw), "${secret:veeam-prod.password}") {
		t.Errorf("reference not written:\n%s", raw)
	}
	// username (not a secret) is untouched.
	if !strings.Contains(string(raw), "username: svc") {
		t.Errorf("non-secret field altered:\n%s", raw)
	}

	// A timestamped backup exists.
	entries, _ := os.ReadDir(filepath.Join(dir, "probes.d"))
	foundBackup := false
	for _, e := range entries {
		if strings.Contains(e.Name(), "10-veeam.yaml.backup.") {
			foundBackup = true
		}
	}
	if !foundBackup {
		t.Error("no backup created")
	}

	// Re-loading resolves the references back to the original values.
	after, err := LoadFromDisk(cfg, nil)
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	var found bool
	for _, p := range after.Probes {
		if p.Name == "veeam-prod" {
			found = true
			if p.Params["password"] != "my-plaintext-pw" {
				t.Errorf("resolved password = %v", p.Params["password"])
			}
		}
	}
	if !found {
		t.Error("veeam-prod probe missing after reload")
	}

	// Idempotent: a second pass seals nothing and creates no new backup.
	if err := SealInlineSecrets(cfg, nil); err != nil {
		t.Fatalf("second SealInlineSecrets: %v", err)
	}
	entries2, _ := os.ReadDir(filepath.Join(dir, "probes.d"))
	backups := 0
	for _, e := range entries2 {
		if strings.Contains(e.Name(), ".backup.") {
			backups++
		}
	}
	if backups != 1 {
		t.Errorf("idempotency: expected exactly 1 backup, got %d", backups)
	}
}

func TestSealInlineSecrets_NoSecrets_NoOp(t *testing.T) {
	mp := secret.NewMemoryProvider()
	secret.SetProvider(mp)
	t.Cleanup(func() { secret.SetProvider(nil) })

	dir := t.TempDir()
	cfg := filepath.Join(dir, "agent.yaml")
	writeSealFile(t, cfg, "config_version: 2\n")
	probe := filepath.Join(dir, "probes.d", "10-cpu.yaml")
	writeSealFile(t, probe, "- name: cpu\n  type: cpu\n  params: {}\n")

	if err := SealInlineSecrets(cfg, nil); err != nil {
		t.Fatalf("SealInlineSecrets: %v", err)
	}
	entries, _ := os.ReadDir(filepath.Join(dir, "probes.d"))
	for _, e := range entries {
		if strings.Contains(e.Name(), ".backup.") {
			t.Error("a backup was created for a secret-free config")
		}
	}
	if names, _ := mp.List(); len(names) != 0 {
		t.Errorf("store should be empty, has %v", names)
	}
}
