package configuration

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rs/zerolog"

	"senhub-agent.go/internal/agent/services/logger"
)

// loaderTestLogger returns a ModuleLogger that swallows output but
// records WARN/ERROR messages so tests can assert that the loader
// surfaces the right diagnostics without spamming stderr during
// `make test`.
func loaderTestLogger(t *testing.T) *logger.ModuleLogger {
	t.Helper()
	l := zerolog.New(os.Stderr).Level(zerolog.Disabled)
	base := (*logger.Logger)(&l)
	return logger.NewModuleLogger(base, "configuration.loader.test")
}

// writeFile is a small testdata helper — paths in the tests are all
// joined relative to t.TempDir() so the filesystem state is sandboxed
// per-test and parallel runs don't collide.
func writeFile(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0750); err != nil {
		t.Fatalf("mkdir %s: %v", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, []byte(body), 0600); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func TestLoadFromDisk_MonolithicLegacy(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "agent-config.yaml")
	writeFile(t, configPath, `
config_version: 2
agent:
  key: "k1"
  mode: offline
probes:
  - name: cpu
    type: cpu
    params: {interval: 30}
storage:
  - name: http
    params:
      port: 8080
`)
	// Pre-create .d/ directories with content — the loader must
	// IGNORE them in legacy mode.
	writeFile(t, filepath.Join(dir, "probes.d", "10-x.yaml"), `- {name: memory, type: memory, params: {}}`)

	data, err := LoadFromDisk(configPath, loaderTestLogger(t))
	if err != nil {
		t.Fatalf("LoadFromDisk: %v", err)
	}
	if data.Agent.Key != "k1" {
		t.Errorf("agent.key = %q, want k1", data.Agent.Key)
	}
	if len(data.Probes) != 1 || data.Probes[0].Name != "cpu" {
		t.Errorf("legacy mode should ignore probes.d/; got probes=%+v", data.Probes)
	}
}

func TestLoadFromDisk_MultiFile_Probes(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "agent.yaml")
	writeFile(t, configPath, `
config_version: 2
agent:
  key: "k1"
  mode: offline
`)
	writeFile(t, filepath.Join(dir, "probes.d", "01-system.yaml"),
		"- {name: cpu, type: cpu, params: {interval: 30}}\n- {name: memory, type: memory, params: {}}\n")
	writeFile(t, filepath.Join(dir, "probes.d", "10-net.yaml"),
		"- {name: net, type: network, params: {}}\n")

	data, err := LoadFromDisk(configPath, loaderTestLogger(t))
	if err != nil {
		t.Fatalf("LoadFromDisk: %v", err)
	}
	if len(data.Probes) != 3 {
		t.Fatalf("got %d probes, want 3 (cpu, memory, net): %+v", len(data.Probes), data.Probes)
	}
}

func TestLoadFromDisk_MultiFile_Strategies(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "agent.yaml")
	writeFile(t, configPath, `
config_version: 2
agent:
  key: "k1"
  mode: offline
`)
	writeFile(t, filepath.Join(dir, "strategies.d", "01-http.yaml"),
		"http:\n  bind_address: 127.0.0.1\n  port: 8080\n")
	writeFile(t, filepath.Join(dir, "strategies.d", "10-otlp.yaml"),
		"otlp:\n  endpoint: localhost:4317\n")

	data, err := LoadFromDisk(configPath, loaderTestLogger(t))
	if err != nil {
		t.Fatalf("LoadFromDisk: %v", err)
	}
	names := map[string]bool{}
	for _, s := range data.Storage {
		names[s.Name] = true
	}
	if !names["http"] || !names["otlp"] {
		t.Errorf("expected both http + otlp strategies, got %+v", names)
	}
}

func TestLoadFromDisk_OrderIsAlpha(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "agent.yaml")
	writeFile(t, configPath, `
config_version: 2
agent:
  key: "k1"
`)
	writeFile(t, filepath.Join(dir, "probes.d", "02-x.yaml"),
		"- {name: second, type: cpu, params: {}}\n")
	writeFile(t, filepath.Join(dir, "probes.d", "01-x.yaml"),
		"- {name: first, type: cpu, params: {}}\n")

	data, err := LoadFromDisk(configPath, loaderTestLogger(t))
	if err != nil {
		t.Fatalf("LoadFromDisk: %v", err)
	}
	if len(data.Probes) != 2 {
		t.Fatalf("got %d probes, want 2", len(data.Probes))
	}
	if data.Probes[0].Name != "first" || data.Probes[1].Name != "second" {
		t.Errorf("probes not in alpha order: got %s, %s", data.Probes[0].Name, data.Probes[1].Name)
	}
}

func TestLoadFromDisk_DisabledFiles(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "agent.yaml")
	writeFile(t, configPath, `
config_version: 2
agent:
  key: "k1"
`)
	// Active fragment.
	writeFile(t, filepath.Join(dir, "probes.d", "10-cpu.yaml"),
		"- {name: cpu, type: cpu, params: {}}\n")
	// Disabled by suffix.
	writeFile(t, filepath.Join(dir, "probes.d", "20-net.yaml.disabled"),
		"- {name: net, type: network, params: {}}\n")
	// Dotfile (editor backup, hidden by convention).
	writeFile(t, filepath.Join(dir, "probes.d", ".swp.yaml"),
		"- {name: swp, type: cpu, params: {}}\n")

	data, err := LoadFromDisk(configPath, loaderTestLogger(t))
	if err != nil {
		t.Fatalf("LoadFromDisk: %v", err)
	}
	if len(data.Probes) != 1 || data.Probes[0].Name != "cpu" {
		t.Errorf("only cpu should load; got %+v", data.Probes)
	}
}

func TestLoadFromDisk_DuplicateStrategy(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "agent.yaml")
	writeFile(t, configPath, `
config_version: 2
agent:
  key: "k1"
`)
	// Two files declare a 'http' strategy. The second file wins.
	writeFile(t, filepath.Join(dir, "strategies.d", "01-http.yaml"),
		"http:\n  bind_address: 127.0.0.1\n  port: 8080\n")
	writeFile(t, filepath.Join(dir, "strategies.d", "02-http-override.yaml"),
		"http:\n  bind_address: 0.0.0.0\n  port: 9999\n")

	data, err := LoadFromDisk(configPath, loaderTestLogger(t))
	if err != nil {
		t.Fatalf("LoadFromDisk: %v", err)
	}
	// Exactly one http strategy in the merged output.
	httpCount := 0
	var found StorageConfig
	for _, s := range data.Storage {
		if s.Name == "http" {
			httpCount++
			found = s
		}
	}
	if httpCount != 1 {
		t.Fatalf("got %d http strategies, want 1 (dup-collapse failed): %+v", httpCount, data.Storage)
	}
	// The later file's value won.
	if port, _ := found.Params["port"].(int); port != 9999 {
		t.Errorf("override file should have won; port=%v, want 9999", found.Params["port"])
	}
}

func TestLoadFromDisk_ParseError(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "agent.yaml")
	writeFile(t, configPath, `
agent:
  key: "k1"
`)
	bad := filepath.Join(dir, "probes.d", "10-bad.yaml")
	writeFile(t, bad, "this: is: bad: yaml: : : :\n")

	_, err := LoadFromDisk(configPath, loaderTestLogger(t))
	if err == nil {
		t.Fatal("expected parse error from malformed YAML")
	}
	if !strings.Contains(err.Error(), bad) {
		t.Errorf("error must mention the bad file path %q, got: %v", bad, err)
	}

	var parseErr *ParseError
	if !errors.As(err, &parseErr) {
		t.Fatalf("decode failure must be a *ParseError, got %T: %v", err, err)
	}
	if parseErr.Path != bad {
		t.Errorf("ParseError.Path = %q, want %q", parseErr.Path, bad)
	}
	// The unwrapped decoder error keeps its own "yaml: line N:" prefix;
	// `config check` scans it to point at the offending line, so a
	// wrapped-in message here would silently kill that output.
	if !strings.HasPrefix(parseErr.Err.Error(), "yaml:") {
		t.Errorf("ParseError.Err should be the raw decoder error, got %q", parseErr.Err)
	}
}

// A syntax error in the top-level file is caught by the legacy-marker
// probe before the real unmarshal ever runs. It has to arrive as the
// same *ParseError, otherwise the most common failure of all — a typo
// in agent.yaml — is the one case the caller cannot classify.
func TestLoadFromDisk_ParseErrorTopLevel(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "agent.yaml")
	writeFile(t, configPath, "agent:\n  key: \"k1\"\n key_misaligned: oops\n")

	_, err := LoadFromDisk(configPath, loaderTestLogger(t))
	if err == nil {
		t.Fatal("expected parse error from malformed top-level YAML")
	}
	var parseErr *ParseError
	if !errors.As(err, &parseErr) {
		t.Fatalf("decode failure must be a *ParseError, got %T: %v", err, err)
	}
	if parseErr.Path != configPath {
		t.Errorf("ParseError.Path = %q, want %q", parseErr.Path, configPath)
	}
}

// A missing file is not a parse failure: callers branch on the
// difference to decide whether printing a line-context block makes
// any sense.
func TestLoadFromDisk_MissingFileIsNotParseError(t *testing.T) {
	dir := t.TempDir()
	_, err := LoadFromDisk(filepath.Join(dir, "absent.yaml"), loaderTestLogger(t))
	if err == nil {
		t.Fatal("expected an error for a missing config file")
	}
	var parseErr *ParseError
	if errors.As(err, &parseErr) {
		t.Errorf("missing file must not classify as *ParseError: %v", err)
	}
	if !errors.Is(err, os.ErrNotExist) {
		t.Errorf("missing file should unwrap to os.ErrNotExist, got: %v", err)
	}
}

func TestLoadFromDisk_EmptyProbesDir(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "agent.yaml")
	writeFile(t, configPath, `
agent:
  key: "k1"
`)
	if err := os.MkdirAll(filepath.Join(dir, "probes.d"), 0750); err != nil {
		t.Fatalf("mkdir probes.d: %v", err)
	}
	// strategies.d also empty.
	if err := os.MkdirAll(filepath.Join(dir, "strategies.d"), 0750); err != nil {
		t.Fatalf("mkdir strategies.d: %v", err)
	}

	data, err := LoadFromDisk(configPath, loaderTestLogger(t))
	if err != nil {
		t.Fatalf("empty .d/ dirs should be valid; got %v", err)
	}
	if len(data.Probes) != 0 || len(data.Storage) != 0 {
		t.Errorf("expected zero probes/strategies, got %d/%d", len(data.Probes), len(data.Storage))
	}
}

func TestLoadFromDisk_SubstitutionAfterMerge(t *testing.T) {
	t.Setenv("SENHUB_LOAD_TEST_PORT", "9999")
	dir := t.TempDir()
	configPath := filepath.Join(dir, "agent.yaml")
	writeFile(t, configPath, `
agent:
  key: "${env:SENHUB_LOAD_TEST_PORT:-fallback}"
`)
	writeFile(t, filepath.Join(dir, "strategies.d", "01-http.yaml"),
		"http:\n  bind_address: 127.0.0.1\n  endpoint: ${env:SENHUB_LOAD_TEST_PORT}\n")

	data, err := LoadFromDisk(configPath, loaderTestLogger(t))
	if err != nil {
		t.Fatalf("LoadFromDisk: %v", err)
	}
	if data.Agent.Key != "9999" {
		t.Errorf("agent.key should be substituted; got %q", data.Agent.Key)
	}
	// Strategy params must also be reached by the walker.
	if got := data.Storage[0].Params["endpoint"]; got != "9999" {
		t.Errorf("strategy param should be substituted; got %v", got)
	}
}

func TestLoadFromDisk_LegacyDetectionMissingMarkers(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "agent.yaml")
	// No `probes:` or `storage:` → multi-file mode, .d/ directories
	// are loaded.
	writeFile(t, configPath, "agent:\n  key: k1\n")
	writeFile(t, filepath.Join(dir, "probes.d", "01.yaml"),
		"- {name: cpu, type: cpu, params: {}}\n")

	data, err := LoadFromDisk(configPath, loaderTestLogger(t))
	if err != nil {
		t.Fatalf("LoadFromDisk: %v", err)
	}
	if len(data.Probes) != 1 {
		t.Errorf("multi-file should load probes.d/; got %d", len(data.Probes))
	}
}
