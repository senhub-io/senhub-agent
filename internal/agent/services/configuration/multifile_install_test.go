package configuration

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/fsnotify/fsnotify"
	"gopkg.in/yaml.v2"

	"senhub-agent.go/internal/agent/cliArgs"
)

// TestCreateDefaultConfiguration_WritesMultiFileLayout pins the
// 0.2.x install contract: a fresh `agent install` produces
// agent.yaml (globals only) + probes.d/00-host.yaml (host probes
// array) + strategies.d/00-http.yaml (one http strategy). Pre-0.2.x
// the install path wrote a single monolithic file with everything
// inline; that mode is removed.
func TestCreateDefaultConfiguration_WritesMultiFileLayout(t *testing.T) {
	tempDir := t.TempDir()
	configPath := filepath.Join(tempDir, "agent.yaml")

	args := &cliArgs.ParsedArgs{ConfigPath: configPath}
	lc := NewLocalConfiguration(args, createTestLocalLogger())

	quitChan := make(chan struct{})
	defer close(quitChan)
	if err := lc.Start(quitChan); err != nil {
		t.Fatalf("Start: %v", err)
	}

	// 1. agent.yaml exists and is globals-only — no probes:, no storage:.
	raw, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("read agent.yaml: %v", err)
	}
	if hasMonolithic(raw) {
		t.Fatal("agent.yaml should be globals-only — found top-level probes: or storage:")
	}

	// 2. probes.d/00-host.yaml exists and parses as a non-empty
	//    YAML array of probe configs.
	probesPath := filepath.Join(tempDir, "probes.d", "00-host.yaml")
	probesRaw, err := os.ReadFile(probesPath)
	if err != nil {
		t.Fatalf("read probes.d/00-host.yaml: %v", err)
	}
	var probes []ProbeConfig
	if err := yaml.Unmarshal(probesRaw, &probes); err != nil {
		t.Fatalf("probes fragment parse: %v", err)
	}
	if len(probes) == 0 {
		t.Fatal("probes.d/00-host.yaml should contain at least one probe")
	}
	wantProbeTypes := map[string]bool{"cpu": false, "memory": false, "network": false, "logicaldisk": false}
	for _, p := range probes {
		if _, ok := wantProbeTypes[p.Type]; ok {
			wantProbeTypes[p.Type] = true
		}
	}
	for typ, found := range wantProbeTypes {
		if !found {
			t.Errorf("probes.d/00-host.yaml missing expected probe type %q", typ)
		}
	}

	// 3. strategies.d/00-http.yaml exists and has exactly one
	//    top-level key (the strategy name) — that's the file format
	//    contract enforced by loadStrategiesD.
	httpPath := filepath.Join(tempDir, "strategies.d", "00-http.yaml")
	httpRaw, err := os.ReadFile(httpPath)
	if err != nil {
		t.Fatalf("read strategies.d/00-http.yaml: %v", err)
	}
	var httpMap map[string]map[string]interface{}
	if err := yaml.Unmarshal(httpRaw, &httpMap); err != nil {
		t.Fatalf("http strategy fragment parse: %v", err)
	}
	if _, ok := httpMap["http"]; !ok {
		t.Fatal("strategies.d/00-http.yaml should have a top-level 'http:' key")
	}
	if len(httpMap) != 1 {
		t.Errorf("strategies.d/00-http.yaml should have exactly one top-level key, got %d", len(httpMap))
	}
}

// TestCreateDefaultConfiguration_LoadFromDiskRoundTrip verifies that
// the multi-file layout written by install loads back via the public
// loader with the expected probes + strategy. This is the integration
// version of the per-file checks above: it pins the contract that an
// `agent install` followed by an `agent run` sees the same data
// without any manual intervention.
func TestCreateDefaultConfiguration_LoadFromDiskRoundTrip(t *testing.T) {
	tempDir := t.TempDir()
	configPath := filepath.Join(tempDir, "agent.yaml")

	args := &cliArgs.ParsedArgs{ConfigPath: configPath}
	lc := NewLocalConfiguration(args, createTestLocalLogger())

	quitChan := make(chan struct{})
	defer close(quitChan)
	if err := lc.Start(quitChan); err != nil {
		t.Fatalf("Start: %v", err)
	}

	data, err := LoadFromDisk(configPath, nil)
	if err != nil {
		t.Fatalf("LoadFromDisk: %v", err)
	}
	if data.Agent.Key == "" {
		t.Error("agent.key not populated from agent.yaml")
	}
	if len(data.Probes) < 4 {
		t.Errorf("expected >= 4 probes from probes.d fragment, got %d", len(data.Probes))
	}
	if len(data.Storage) != 1 || data.Storage[0].Name != "http" {
		t.Errorf("expected single http storage from strategies.d fragment, got %+v", data.Storage)
	}
}

// hasMonolithic is a test-local copy of the migrate-side detector —
// kept here so the test does not import cmd/agent. Returns true if
// the YAML contains a top-level `probes:` or `storage:` block.
func hasMonolithic(raw []byte) bool {
	var probe struct {
		Probes  []interface{} `yaml:"probes"`
		Storage []interface{} `yaml:"storage"`
	}
	if err := yaml.Unmarshal(raw, &probe); err != nil {
		return false
	}
	return probe.Probes != nil || probe.Storage != nil
}

// TestWatcher_FragmentDirsAreWatched confirms Start successfully adds
// the probes.d and strategies.d directories to the fsnotify watcher
// when they exist. (The reload-on-fragment-change path is exercised
// implicitly by the existing TestLocalConfiguration_ReloadConfiguration
// suite — this test pins the wiring rather than the behavior.)
func TestWatcher_FragmentDirsAreWatched(t *testing.T) {
	tempDir := t.TempDir()
	configPath := filepath.Join(tempDir, "agent.yaml")

	args := &cliArgs.ParsedArgs{ConfigPath: configPath}
	lc := NewLocalConfiguration(args, createTestLocalLogger())

	quitChan := make(chan struct{})
	defer close(quitChan)
	if err := lc.Start(quitChan); err != nil {
		t.Fatalf("Start: %v", err)
	}

	if lc.watcher == nil {
		t.Fatal("watcher should be initialized after Start")
	}
	watched := lc.watcher.WatchList()

	hasMain, hasProbes, hasStrategies := false, false, false
	for _, w := range watched {
		switch {
		case w == configPath:
			hasMain = true
		case strings.HasSuffix(w, "probes.d"):
			hasProbes = true
		case strings.HasSuffix(w, "strategies.d"):
			hasStrategies = true
		}
	}
	if !hasMain {
		t.Error("main config file should be watched")
	}
	if !hasProbes {
		t.Error("probes.d should be watched")
	}
	if !hasStrategies {
		t.Error("strategies.d should be watched")
	}
}

// TestShouldIgnoreEvent pins the event-filter contract used by the
// watcher to suppress noise from editor swap files and disabled
// fragments.
func TestShouldIgnoreEvent(t *testing.T) {
	cases := []struct {
		name   string
		path   string
		ignore bool
	}{
		{"dotfile in probes.d", "/etc/senhub-agent/probes.d/.10-mysql.yaml.swp", true},
		{"disabled fragment", "/etc/senhub-agent/probes.d/10-mysql.yaml.disabled", true},
		{"non-yaml in fragment dir", "/etc/senhub-agent/probes.d/10-mysql.txt", true},
		{"yaml fragment", "/etc/senhub-agent/probes.d/10-mysql.yaml", false},
		{"yml fragment", "/etc/senhub-agent/strategies.d/20-otlp.yml", false},
		{"top-level config", "/etc/senhub-agent/agent.yaml", false},
		{"top-level editor backup", "/etc/senhub-agent/.agent.yaml.swp", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ev := fsnotify.Event{Name: tc.path}
			got := shouldIgnoreEvent(ev)
			if got != tc.ignore {
				t.Errorf("shouldIgnoreEvent(%q) = %v, want %v", tc.path, got, tc.ignore)
			}
		})
	}
}
