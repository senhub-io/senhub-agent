package configuration

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"senhub-agent.go/internal/agent/services/configuration/secret"
)

func multiFileForFragments(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	main := filepath.Join(dir, "agent.yaml")
	if err := os.WriteFile(main, []byte("config_version: 3\nagent:\n  key: \"k\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "probes.d"), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "probes.d", "00-host.yaml"), []byte("# hand written\n- name: cpu\n  type: cpu\n  params:\n    interval: 30\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	secret.SetConfigDir(dir)
	secret.SetProvider(secret.NewMemoryProvider())
	return main
}

func TestCreateProbeFragment_RoundTripsAndSealsSecrets(t *testing.T) {
	main := multiFileForFragments(t)
	p := ProbeConfig{Name: "mysql-prod", Type: "mysql", Params: map[string]interface{}{
		"host": "db1", "port": 3306, "username": "monitor", "password": "s3cr3t",
		"tls": map[string]interface{}{"enabled": true},
	}}
	path, err := CreateProbeFragment(main, p, []string{"password"})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	raw, _ := os.ReadFile(path)
	if !strings.HasPrefix(string(raw), managedFragmentHeader) {
		t.Error("fragment must start with the managed header")
	}
	if strings.Contains(string(raw), "s3cr3t") {
		t.Error("the password must not be written in clear")
	}
	if !strings.Contains(string(raw), "${secret:mysql-prod.password}") {
		t.Errorf("expected a secret reference, got:\n%s", raw)
	}
	cfg, err := LoadFromDisk(main, nil)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	var found *ProbeConfig
	for i := range cfg.Probes {
		if cfg.Probes[i].Name == "mysql-prod" {
			found = &cfg.Probes[i]
		}
	}
	if found == nil {
		t.Fatal("the new probe is not loaded back")
	}
	if found.Params["host"] != "db1" || found.Params["port"] != 3306 {
		t.Errorf("params did not round-trip: %v", found.Params)
	}
	if got := found.Params["password"]; got != "s3cr3t" {
		t.Errorf("the loader must resolve the sealed password, got %v", got)
	}
	if !IsManagedProbeFragment(path) {
		t.Error("IsManagedProbeFragment must recognise the file")
	}
}

func TestCreateProbeFragment_Refusals(t *testing.T) {
	main := multiFileForFragments(t)
	if _, err := CreateProbeFragment(main, ProbeConfig{Name: "cpu", Type: "cpu"}, nil); err == nil || !strings.Contains(err.Error(), "already exists") {
		t.Errorf("a duplicate name must be refused, got %v", err)
	}
	if _, err := CreateProbeFragment(main, ProbeConfig{Name: "bad name!", Type: "cpu"}, nil); err == nil {
		t.Error("an invalid name must be refused")
	}
	if _, err := CreateProbeFragment(main, ProbeConfig{Name: "x", Type: ""}, nil); err == nil {
		t.Error("a missing type must be refused")
	}
	legacy := filepath.Join(t.TempDir(), "agent-config.yaml")
	os.WriteFile(legacy, []byte("probes:\n  - name: cpu\n    type: cpu\nstorage: []\n"), 0o600)
	if _, err := CreateProbeFragment(legacy, ProbeConfig{Name: "x", Type: "cpu"}, nil); err == nil || !strings.Contains(err.Error(), "legacy") {
		t.Errorf("the legacy layout must be refused with guidance, got %v", err)
	}
}

func TestUpdateAndDeleteProbeFragment_ManagedOnly(t *testing.T) {
	main := multiFileForFragments(t)
	dir := filepath.Dir(main)
	// A hand-written fragment at the managed path is never touched.
	hand := ProbeFragmentPath(main, "byhand")
	os.WriteFile(hand, []byte("- name: byhand\n  type: cpu\n"), 0o600)
	if _, err := UpdateProbeFragment(main, ProbeConfig{Name: "byhand", Type: "cpu"}, nil); err == nil {
		t.Error("updating a hand-written fragment must be refused")
	}
	if _, err := DeleteProbeFragment(main, "byhand"); err == nil {
		t.Error("deleting a hand-written fragment must be refused")
	}
	if _, err := os.Stat(hand); err != nil {
		t.Error("the hand-written fragment must still exist")
	}
	// A managed one can be updated and deleted.
	if _, err := CreateProbeFragment(main, ProbeConfig{Name: "web", Type: "http_check", Params: map[string]interface{}{"targets": []interface{}{"http://a"}}}, nil); err != nil {
		t.Fatal(err)
	}
	off := false
	if _, err := UpdateProbeFragment(main, ProbeConfig{Name: "web", Type: "http_check", Enabled: &off, Params: map[string]interface{}{"targets": []interface{}{"http://b"}}}, nil); err != nil {
		t.Fatalf("update: %v", err)
	}
	cfg, _ := LoadFromDisk(main, nil)
	for _, p := range cfg.Probes {
		if p.Name == "web" {
			if p.IsEnabled() {
				t.Error("enabled: false did not round-trip")
			}
			if ts, _ := p.Params["targets"].([]interface{}); len(ts) != 1 || ts[0] != "http://b" {
				t.Errorf("updated params not loaded: %v", p.Params)
			}
		}
	}
	if _, err := DeleteProbeFragment(main, "web"); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "probes.d", "50-web.yaml")); !os.IsNotExist(err) {
		t.Error("the managed fragment must be gone")
	}
}
