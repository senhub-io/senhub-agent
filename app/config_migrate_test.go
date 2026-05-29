package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/fsnotify/fsnotify"
	"gopkg.in/yaml.v2"
)

// writeMonolithicFixture stages a minimal-but-realistic monolithic
// config on disk and returns its path. Used by the migrate-engine
// tests to exercise the full split path against the same shapes
// production configs use.
func writeMonolithicFixture(t *testing.T, dir string) string {
	t.Helper()
	body := []byte(`config_version: 2

agent:
  key: "migrate-test-key"
  mode: offline

auto_update:
  enabled: false
  include_beta: false
  url: "https://example.com/releases"

cache:
  retention_minutes: 5

storage:
  - name: http
    params:
      port: 8080
      bind_address: "127.0.0.1"
      endpoints: ["prtg", "web", "nagios"]
  - name: otlp
    params:
      endpoint: "127.0.0.1:4317"
      protocol: grpc

probes:
  - name: cpu
    type: cpu
    params:
      interval: 30
  - name: memory
    type: memory
    params:
      interval: 30
`)
	path := filepath.Join(dir, "agent-config.yaml")
	if err := os.WriteFile(path, body, 0600); err != nil {
		t.Fatalf("seed monolithic fixture: %v", err)
	}
	return path
}

// TestRunMigrate_HappyPath drives the full migrate engine on a
// realistic monolithic file and asserts the resulting multi-file
// layout: agent.yaml without probes/storage, probes.d/00-host.yaml
// with the original probes, and one strategies.d file per strategy.
func TestRunMigrate_HappyPath(t *testing.T) {
	dir := t.TempDir()
	configPath := writeMonolithicFixture(t, dir)

	result, err := runMigrate(configPath)
	if err != nil {
		t.Fatalf("runMigrate: %v", err)
	}
	if result.AlreadyMultiFile {
		t.Fatal("AlreadyMultiFile should be false — fixture is monolithic")
	}
	if result.BackupPath == "" {
		t.Fatal("BackupPath should be set after migration")
	}
	if !result.WroteProbes {
		t.Fatal("WroteProbes should be true — fixture has probes")
	}
	if result.StrategyCount != 2 {
		t.Errorf("StrategyCount = %d, want 2", result.StrategyCount)
	}

	// agent.yaml at the original path no longer carries probes or storage.
	rewritten, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("read rewritten agent.yaml: %v", err)
	}
	var rewrittenMap map[string]interface{}
	if err := yaml.Unmarshal(rewritten, &rewrittenMap); err != nil {
		t.Fatalf("parse rewritten agent.yaml: %v", err)
	}
	if _, has := rewrittenMap["probes"]; has {
		t.Error("agent.yaml still contains a top-level probes: block")
	}
	if _, has := rewrittenMap["storage"]; has {
		t.Error("agent.yaml still contains a top-level storage: block")
	}
	if rewrittenMap["config_version"] == nil || rewrittenMap["agent"] == nil {
		t.Error("agent.yaml should still carry config_version and agent globals")
	}

	// probes.d/00-host.yaml is a YAML array with the original probes.
	probesPath := filepath.Join(dir, "probes.d", "00-host.yaml")
	probesRaw, err := os.ReadFile(probesPath)
	if err != nil {
		t.Fatalf("read probes.d/00-host.yaml: %v", err)
	}
	var migratedProbes []map[string]interface{}
	if err := yaml.Unmarshal(probesRaw, &migratedProbes); err != nil {
		t.Fatalf("parse probes fragment: %v", err)
	}
	if len(migratedProbes) != 2 {
		t.Errorf("migrated probes count = %d, want 2", len(migratedProbes))
	}

	// strategies.d has one file per strategy. Names are
	// NN-<name>.yaml — NN preserves source order.
	strategiesDir := filepath.Join(dir, "strategies.d")
	entries, err := os.ReadDir(strategiesDir)
	if err != nil {
		t.Fatalf("read strategies.d: %v", err)
	}
	if len(entries) != 2 {
		t.Errorf("strategies.d entries = %d, want 2", len(entries))
	}
	found := map[string]bool{}
	for _, e := range entries {
		raw, _ := os.ReadFile(filepath.Join(strategiesDir, e.Name()))
		var single map[string]map[string]interface{}
		if err := yaml.Unmarshal(raw, &single); err != nil {
			t.Errorf("parse %s: %v", e.Name(), err)
			continue
		}
		for name := range single {
			found[name] = true
		}
	}
	for _, want := range []string{"http", "otlp"} {
		if !found[want] {
			t.Errorf("strategies.d missing strategy %q (found %v)", want, found)
		}
	}

	// Backup file exists and contains the original content.
	backupBody, err := os.ReadFile(result.BackupPath)
	if err != nil {
		t.Fatalf("read backup: %v", err)
	}
	if !strings.Contains(string(backupBody), "endpoint: \"127.0.0.1:4317\"") {
		t.Error("backup is missing the original storage content")
	}
}

// TestRunMigrate_Idempotent confirms re-running migrate on an
// already-multi-file layout is a no-op and reports AlreadyMultiFile.
func TestRunMigrate_Idempotent(t *testing.T) {
	dir := t.TempDir()
	configPath := writeMonolithicFixture(t, dir)

	if _, err := runMigrate(configPath); err != nil {
		t.Fatalf("first runMigrate: %v", err)
	}
	result2, err := runMigrate(configPath)
	if err != nil {
		t.Fatalf("second runMigrate: %v", err)
	}
	if !result2.AlreadyMultiFile {
		t.Error("second runMigrate should report AlreadyMultiFile=true")
	}
	if result2.BackupPath != "" {
		t.Error("idempotent run should not write a new backup")
	}
}

// TestRunMigrate_MissingFile surfaces a clear error rather than
// half-creating things.
func TestRunMigrate_MissingFile(t *testing.T) {
	_, err := runMigrate("/nonexistent/path/to/agent.yaml")
	if err == nil {
		t.Fatal("expected error for missing config file")
	}
	if !strings.Contains(err.Error(), "cannot read") {
		t.Errorf("error should mention 'cannot read', got %v", err)
	}
}

// TestHasMonolithicMarkers pins the detection logic that drives the
// migrate "nothing to do" idempotency check.
func TestHasMonolithicMarkers(t *testing.T) {
	cases := []struct {
		name string
		yaml string
		want bool
	}{
		{
			name: "has probes",
			yaml: "probes:\n  - name: cpu\n    type: cpu\n",
			want: true,
		},
		{
			name: "has storage",
			yaml: "storage:\n  - name: http\n",
			want: true,
		},
		{
			name: "has both",
			yaml: "probes: []\nstorage: []\n",
			want: true,
		},
		{
			name: "globals only",
			yaml: "config_version: 2\nagent:\n  key: abc\n",
			want: false,
		},
		{
			name: "empty",
			yaml: "",
			want: false,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := hasMonolithicMarkers([]byte(tc.yaml))
			if got != tc.want {
				t.Errorf("hasMonolithicMarkers(%q) = %v, want %v", tc.name, got, tc.want)
			}
		})
	}
}

// TestSafeFilenameComponent pins the strategy-name → filename
// sanitiser. The loader matches strategies by top-level YAML key,
// not filename, but the filename still needs to be portable.
func TestSafeFilenameComponent(t *testing.T) {
	cases := map[string]string{
		"http":        "http",
		"my_strategy": "my_strategy",
		"my-strategy": "my-strategy",
		"weird/name":  "weird-name",
		"with spaces": "with-spaces",
		"":            "strategy",
		"!@#$%^&*()":  "----------",
		"unicode-é":   "unicode--",
	}
	for in, want := range cases {
		got := safeFilenameComponent(in)
		if got != want {
			t.Errorf("safeFilenameComponent(%q) = %q, want %q", in, got, want)
		}
	}
}

// unused but kept to demonstrate the shouldIgnoreEvent contract is
// stable enough that cmd/agent test wiring can synthesize fsnotify
// events without a real watcher. Left for symmetry with the
// configuration-package test of the same name; we don't import it
// to avoid a circular dependency.
var _ = fsnotify.Event{}
