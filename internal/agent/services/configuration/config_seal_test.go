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

	// On success the pre-change backup is SCRUBBED — it held the plaintext we
	// just sealed, so leaving it would defeat the purpose. No *.backup.* file
	// must survive a successful seal.
	entries, _ := os.ReadDir(filepath.Join(dir, "probes.d"))
	for _, e := range entries {
		if strings.Contains(e.Name(), ".backup.") {
			t.Errorf("seal backup not scrubbed after success: %s", e.Name())
		}
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

	// Idempotent: a second pass seals nothing, writes no backup, leaves none.
	if err := SealInlineSecrets(cfg, nil); err != nil {
		t.Fatalf("second SealInlineSecrets: %v", err)
	}
	entries2, _ := os.ReadDir(filepath.Join(dir, "probes.d"))
	for _, e := range entries2 {
		if strings.Contains(e.Name(), ".backup.") {
			t.Errorf("idempotency: unexpected backup after no-op seal: %s", e.Name())
		}
	}
}

// TestSealInlineSecrets_Monolithic feeds a legacy monolithic config and asserts
// the combined harmonise+seal behaviour: the file is split into the multi-file
// layout (agent.yaml globals + probes.d/ + strategies.d/), every plaintext
// secret across the resulting fragments is moved to the store, the agent key is
// sealed while the license stays clear, and a reload resolves everything back to
// the originals.
func TestSealInlineSecrets_Monolithic(t *testing.T) {
	mp := secret.NewMemoryProvider()
	secret.SetProvider(mp)
	t.Cleanup(func() { secret.SetProvider(nil) })

	dir := t.TempDir()
	cfg := filepath.Join(dir, "agent-config.yaml")
	writeSealFile(t, cfg, `config_version: 2
agent:
  key: aaaa-bbbb-cccc
  license: my-jwt
probes:
  - name: pg-prod
    type: postgresql
    params:
      host: 10.0.0.5
      username: svc
      password: pg-pw
      director:
        auth:
          password: nested-pw
storage:
  - name: cloud
    params:
      bind_address: ":9100"
      bearer_token: bearer-pw
`)

	if err := SealInlineSecrets(cfg, nil); err != nil {
		t.Fatalf("SealInlineSecrets monolithic: %v", err)
	}

	// Harmonisation happened: the monolithic file was split. The original path
	// is now globals-only and a pre-multi-file backup exists.
	cfgRaw, _ := os.ReadFile(cfg)
	if strings.Contains(string(cfgRaw), "\nprobes:") || strings.Contains(string(cfgRaw), "\nstorage:") {
		t.Errorf("config not harmonised — probes/storage still inline:\n%s", cfgRaw)
	}
	if _, err := os.Stat(filepath.Join(dir, "probes.d")); err != nil {
		t.Errorf("probes.d not created by harmonisation: %v", err)
	}
	// The harmonise backup held plaintext (the pre-split monolithic source); a
	// successful seal scrubs it so no password survives in the config dir.
	if backups, _ := filepath.Glob(cfg + ".pre-multi-file.*"); len(backups) != 0 {
		t.Errorf("pre-multi-file backup not scrubbed after seal: %v", backups)
	}

	// Every secret — flat, nested, storage, agent.key — landed in the store.
	names, _ := mp.List()
	have := map[string]bool{}
	for _, n := range names {
		have[n] = true
	}
	for _, want := range []string{
		"agent.key", "pg-prod.password", "pg-prod.director.auth.password", "cloud.bearer_token",
	} {
		if !have[want] {
			t.Errorf("store missing %q (have %v)", want, names)
		}
	}

	// No plaintext secret survives in ANY resulting file (globals + fragments),
	// excluding the intentional backup of the pre-seal source.
	var leaks []string
	_ = filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return nil
		}
		if strings.Contains(path, ".pre-multi-file.") || strings.Contains(path, ".backup.") {
			return nil // backups legitimately hold the pre-seal plaintext
		}
		body, _ := os.ReadFile(path)
		for _, plaintext := range []string{"pg-pw", "nested-pw", "bearer-pw", "aaaa-bbbb-cccc"} {
			if strings.Contains(string(body), plaintext) {
				leaks = append(leaks, plaintext+" in "+filepath.Base(path))
			}
		}
		return nil
	})
	if len(leaks) > 0 {
		t.Errorf("plaintext survived in live config files: %v", leaks)
	}
	// license stays in clear in the globals file.
	if !strings.Contains(string(cfgRaw), "license: my-jwt") {
		t.Errorf("license should stay clear:\n%s", cfgRaw)
	}

	after, err := LoadFromDisk(cfg, nil)
	if err != nil {
		t.Fatalf("reload monolithic: %v", err)
	}
	if after.ConfigVersion != 3 {
		t.Errorf("config_version = %d, want 3", after.ConfigVersion)
	}
	if after.Agent.Key != "aaaa-bbbb-cccc" {
		t.Errorf("agent key resolved to %q", after.Agent.Key)
	}
	if after.Agent.License != "my-jwt" {
		t.Errorf("license = %q, want my-jwt", after.Agent.License)
	}
	var sawPG bool
	for _, p := range after.Probes {
		if p.Name == "pg-prod" {
			sawPG = true
			if p.Params["password"] != "pg-pw" {
				t.Errorf("pg password resolved to %v", p.Params["password"])
			}
		}
	}
	if !sawPG {
		t.Error("pg-prod probe missing after harmonise+seal+reload")
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
