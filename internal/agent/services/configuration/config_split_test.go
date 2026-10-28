package configuration

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/rs/zerolog"
	"gopkg.in/yaml.v2"

	"senhub-agent.go/internal/agent/services/configuration/secret"
	"senhub-agent.go/internal/agent/services/logger"
)

// writeMonolithicFixture stages a minimal-but-realistic monolithic
// config on disk and returns its path. Used by the split-engine tests
// to exercise the full path against the same shapes production
// configs use.
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

// TestMigrateToMultiFile_HappyPath drives the full engine on a
// realistic monolithic file and asserts the resulting multi-file
// layout: agent.yaml without probes/storage, one probes.d file per
// probe, and one strategies.d file per strategy.
func TestMigrateToMultiFile_HappyPath(t *testing.T) {
	dir := t.TempDir()
	configPath := writeMonolithicFixture(t, dir)

	result, err := MigrateToMultiFile(configPath, nil)
	if err != nil {
		t.Fatalf("MigrateToMultiFile: %v", err)
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

	// probes.d holds one fragment per probe, in source order.
	if result.ProbeCount != 2 {
		t.Errorf("ProbeCount = %d, want 2", result.ProbeCount)
	}
	probeFiles, _ := filepath.Glob(filepath.Join(dir, "probes.d", "*.yaml"))
	if len(probeFiles) != 2 {
		t.Fatalf("probes.d fragments = %v, want 2", probeFiles)
	}
	if filepath.Base(probeFiles[0]) != "10-cpu.yaml" || filepath.Base(probeFiles[1]) != "11-memory.yaml" {
		t.Errorf("fragment names = %v", probeFiles)
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

// TestMigrateToMultiFile_Idempotent confirms re-running migrate on an
// already-multi-file layout is a no-op and reports AlreadyMultiFile.
func TestMigrateToMultiFile_Idempotent(t *testing.T) {
	dir := t.TempDir()
	configPath := writeMonolithicFixture(t, dir)

	if _, err := MigrateToMultiFile(configPath, nil); err != nil {
		t.Fatalf("first MigrateToMultiFile: %v", err)
	}
	result2, err := MigrateToMultiFile(configPath, nil)
	if err != nil {
		t.Fatalf("second MigrateToMultiFile: %v", err)
	}
	if !result2.AlreadyMultiFile {
		t.Error("second MigrateToMultiFile should report AlreadyMultiFile=true")
	}
	if result2.BackupPath != "" {
		t.Error("idempotent run should not write a new backup")
	}
}

// TestMigrateToMultiFile_MissingFile surfaces a clear error rather
// than half-creating things.
func TestMigrateToMultiFile_MissingFile(t *testing.T) {
	_, err := MigrateToMultiFile("/nonexistent/path/to/agent.yaml", nil)
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
			got := HasMonolithicMarkers([]byte(tc.yaml))
			if got != tc.want {
				t.Errorf("HasMonolithicMarkers(%q) = %v, want %v", tc.name, got, tc.want)
			}
		})
	}
}

// TestSafeFilenameComponent pins the strategy-name → filename
// sanitiser. The loader matches strategies by top-level YAML key, not
// filename, but the filename still needs to be portable.
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

func backups(t *testing.T, dir string) []string {
	t.Helper()
	m, _ := filepath.Glob(filepath.Join(dir, "*.pre-multi-file.*"))
	return m
}

// Fragments a failed attempt left behind made every later attempt fail
// the equality check and leave one more backup, at every start (#974).
func TestMigrateToMultiFile_RemovesLeftoversOfAFailedAttempt(t *testing.T) {
	dir := t.TempDir()
	path := writeMonolithicFixture(t, dir)
	if err := os.MkdirAll(filepath.Join(dir, "probes.d"), 0o750); err != nil {
		t.Fatal(err)
	}
	leftover := filepath.Join(dir, "probes.d", "00-host.yaml")
	if err := os.WriteFile(leftover, []byte("# Probes migrated from monolithic config on 20260805-101010.\n- name: stale\n  type: cpu\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	res, err := MigrateToMultiFile(path, nil)
	if err != nil {
		t.Fatalf("migration with leftovers of an earlier attempt: %v", err)
	}
	if _, err := os.Stat(leftover); err == nil {
		t.Error("the leftover fragment was kept instead of being replaced by one file per probe")
	}
	if len(backups(t, dir)) != 1 || res.BackupPath == "" {
		t.Errorf("a successful migration keeps exactly one backup, got %v", backups(t, dir))
	}
}

// A fragment the operator wrote is ignored by the legacy loader; migrating
// would start it, so the migration refuses and writes nothing.
func TestMigrateToMultiFile_RefusesBesideOperatorFragments(t *testing.T) {
	dir := t.TempDir()
	path := writeMonolithicFixture(t, dir)
	before, _ := os.ReadFile(path)
	if err := os.MkdirAll(filepath.Join(dir, "strategies.d"), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "strategies.d", "50-mine.yaml"), []byte("otlp:\n  endpoint: x:4317\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := MigrateToMultiFile(path, nil)
	var fe *FragmentsBesideMonolithicError
	if !errors.As(err, &fe) || len(fe.Fragments) != 1 {
		t.Fatalf("want FragmentsBesideMonolithicError naming the fragment, got %v", err)
	}
	after, _ := os.ReadFile(path)
	if string(after) != string(before) || len(backups(t, dir)) != 0 {
		t.Error("a refused migration must leave the file untouched and no backup")
	}
	if _, err := os.Stat(filepath.Join(dir, "probes.d")); err == nil {
		t.Error("a refused migration must not write fragments")
	}
}

// A migration that fails after writing leaves the host as it found it.
func TestMigrateToMultiFile_FailureLeavesNothingBehind(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "agent-config.yaml")
	src := []byte("config_version: 2\nagent:\n  key: \"k\"\nstorage:\n  - params: {port: 8080}\nprobes:\n  - name: cpu\n    type: cpu\n")
	if err := os.WriteFile(path, src, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := MigrateToMultiFile(path, nil); err == nil {
		t.Fatal("a strategy without a name must fail the migration")
	}
	after, _ := os.ReadFile(path)
	if string(after) != string(src) {
		t.Error("the original file was not restored")
	}
	if b := backups(t, dir); len(b) != 0 {
		t.Errorf("the backup of a restored file must go, got %v", b)
	}
	owned, foreign := classifyFragments(dir)
	if len(owned)+len(foreign) != 0 {
		t.Errorf("fragments left behind: %v %v", owned, foreign)
	}
}

// The split is the migration the legacy-layout warning asks for: it must not
// be logged just before the split runs on install defaults.
func TestMigrateToMultiFile_NoLegacyWarningBeforeTheSplit(t *testing.T) {
	dir := t.TempDir()
	path := writeMonolithicFixture(t, dir)
	if err := os.MkdirAll(filepath.Join(dir, "probes.d"), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "probes.d", "00-host.yaml"), []byte("# nothing yet\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	zl := zerolog.New(&buf)
	log := logger.NewModuleLogger((*logger.Logger)(&zl), "configuration.split.test")
	if _, err := MigrateToMultiFile(path, log); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(buf.String(), "Legacy monolithic") {
		t.Errorf("legacy warning logged before the split: %s", buf.String())
	}
}

func TestLoadWarnsOfLegacyOnlyWhenTheSplitWillNotRun(t *testing.T) {
	for _, tc := range []struct {
		name     string
		fragment string
		body     string
		warn     bool
	}{
		{"install defaults only", "00-host.yaml", "# nothing yet\n", false},
		{"a fragment of the operator", "50-mine.yaml", operatorFragment, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			path := writeMonolithicFixture(t, dir)
			if err := os.MkdirAll(filepath.Join(dir, "probes.d"), 0o750); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(dir, "probes.d", tc.fragment), []byte(tc.body), 0o600); err != nil {
				t.Fatal(err)
			}
			var buf bytes.Buffer
			zl := zerolog.New(&buf)
			if _, err := LoadFromDisk(path, logger.NewModuleLogger((*logger.Logger)(&zl), "configuration.test")); err != nil {
				t.Fatal(err)
			}
			if got := strings.Contains(buf.String(), "Legacy monolithic"); got != tc.warn {
				t.Errorf("legacy warning = %v, want %v: %s", got, tc.warn, buf.String())
			}
		})
	}
}

const operatorFragment = "- name: mine\n  type: cpu\n"

const mixedMonolithic = `config_version: 2
agent:
  key: "migrate-test-key"
  mode: offline
storage:
  - name: http
    params:
      port: 8080
      bind_address: "127.0.0.1"
probes:
  - name: cpu
    type: cpu
    params:
      interval: 30
  - name: lb-main
    type: netscaler
    params:
      host: "10.0.0.1"
      username: monitor
      password: "${secret:probe.lb-main.password}"
      interval: 60
    governance:
      owner: team-network
      criticality: high
  - name: db.prod/1
    type: mysql
    params:
      dsn: "user@tcp(127.0.0.1:3306)/"
`

func TestMigrateToMultiFile_OneFilePerProbe(t *testing.T) {
	mp := secret.NewMemoryProvider()
	_ = mp.Set("probe.lb-main.password", secret.New("pw-for-test"))
	secret.SetProvider(mp)
	t.Cleanup(func() { secret.SetProvider(nil) })

	dir := t.TempDir()
	path := filepath.Join(dir, "agent-config.yaml")
	if err := os.WriteFile(path, []byte(mixedMonolithic), 0o600); err != nil {
		t.Fatal(err)
	}
	before, err := LoadFromDisk(path, nil)
	if err != nil {
		t.Fatalf("load source: %v", err)
	}

	res, err := MigrateToMultiFile(path, nil)
	if err != nil {
		t.Fatalf("migrate: %v", err)
	}
	if res.ProbeCount != 3 {
		t.Errorf("ProbeCount = %d, want 3", res.ProbeCount)
	}
	files, _ := filepath.Glob(filepath.Join(dir, "probes.d", "*.yaml"))
	want := []string{"10-cpu.yaml", "11-lb-main.yaml", "12-db-prod-1.yaml"}
	if len(files) != len(want) {
		t.Fatalf("fragments = %v, want %v", files, want)
	}
	for i, f := range files {
		if filepath.Base(f) != want[i] {
			t.Errorf("fragment %d = %s, want %s", i, filepath.Base(f), want[i])
		}
		body, _ := os.ReadFile(f)
		if !strings.HasPrefix(string(body), "# Probe ") || !strings.Contains(strings.SplitN(string(body), "\n", 2)[0], migratedFragmentMarker) {
			t.Errorf("%s: header comment missing or without the migration marker", f)
		}
	}
	lb, _ := os.ReadFile(files[1])
	for _, frag := range []string{"${secret:probe.lb-main.password}", "governance:", "criticality: high"} {
		if !strings.Contains(string(lb), frag) {
			t.Errorf("netscaler fragment lost %q:\n%s", frag, lb)
		}
	}

	after, err := LoadFromDisk(path, nil)
	if err != nil {
		t.Fatalf("load migrated: %v", err)
	}
	if !reflect.DeepEqual(before.Probes, after.Probes) {
		t.Errorf("merged probes differ after migration:\nbefore %+v\nafter  %+v", before.Probes, after.Probes)
	}

	again, err := MigrateToMultiFile(path, nil)
	if err != nil || !again.AlreadyMultiFile {
		t.Fatalf("rerun: %+v, %v", again, err)
	}
	files2, _ := filepath.Glob(filepath.Join(dir, "probes.d", "*.yaml"))
	if len(files2) != 3 {
		t.Errorf("rerun changed the fragments: %v", files2)
	}
}

func TestSplitProbes_FixedWidthKeepsOrder(t *testing.T) {
	dir := t.TempDir()
	list := make([]interface{}, 95)
	for i := range list {
		list[i] = map[interface{}]interface{}{"name": fmt.Sprintf("p%d", i), "type": "cpu"}
	}
	n, err := splitProbes(list, dir, "ts")
	if err != nil || n != 95 {
		t.Fatalf("splitProbes: %d, %v", n, err)
	}
	files, _ := filepath.Glob(filepath.Join(dir, "*.yaml"))
	if len(files) != 95 || filepath.Base(files[0]) != "100-p0.yaml" || filepath.Base(files[94]) != "194-p94.yaml" {
		t.Errorf("names = %s ... %s (%d)", filepath.Base(files[0]), filepath.Base(files[len(files)-1]), len(files))
	}
}
