package configuration

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rs/zerolog"

	"senhub-agent.go/internal/agent/services/logger"
)

func seedInstallDefaults(t *testing.T, dir string) {
	t.Helper()
	files := map[string]string{
		filepath.Join(dir, "probes.d", "00-host.yaml"): HostProbesFragmentTemplate,
		filepath.Join(dir, "strategies.d", "00-http.yaml"): fmt.Sprintf(HTTPStrategyFragmentTemplate,
			8080, "127.0.0.1", `"prtg", "web", "nagios", "prometheus"`, "11111111-2222-4333-8444-555555555555", ""),
	}
	for p, body := range files {
		if err := os.MkdirAll(filepath.Dir(p), 0o750); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
}

// The files `install` creates are ignored in legacy mode; they must not
// stop the split (#589).
func TestMigrateToMultiFile_SetsInstallDefaultsAside(t *testing.T) {
	dir := t.TempDir()
	path := writeMonolithicFixture(t, dir)
	seedInstallDefaults(t, dir)

	if _, err := MigrateToMultiFile(path, nil); err != nil {
		t.Fatalf("install defaults beside a monolithic file must not block the split: %v", err)
	}
	for _, aside := range []string{
		filepath.Join(dir, "probes.d", "00-host.yaml.install-default"),
		filepath.Join(dir, "strategies.d", "00-http.yaml.install-default"),
	} {
		if _, err := os.Stat(aside); err != nil {
			t.Errorf("default not kept aside: %v", err)
		}
	}
	probes, _ := os.ReadFile(filepath.Join(dir, "probes.d", "00-host.yaml"))
	if !strings.Contains(string(probes), "migrated from monolithic config") {
		t.Errorf("probes.d/00-host.yaml should hold the migrated probes, got:\n%s", probes)
	}
}

func TestMigrateToMultiFile_EmptyFragmentDirsDoNotBlock(t *testing.T) {
	dir := t.TempDir()
	path := writeMonolithicFixture(t, dir)
	for _, sub := range []string{"probes.d", "strategies.d"} {
		if err := os.MkdirAll(filepath.Join(dir, sub), 0o750); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := MigrateToMultiFile(path, nil); err != nil {
		t.Fatalf("empty *.d directories must not block the split: %v", err)
	}
}

// User content beside install defaults: one error naming only the user's
// file, and the defaults stay where they are.
func TestMigrateToMultiFile_UserFragmentsRefuseWithOneMessage(t *testing.T) {
	dir := t.TempDir()
	path := writeMonolithicFixture(t, dir)
	seedInstallDefaults(t, dir)
	edited := filepath.Join(dir, "probes.d", "10-mine.yaml")
	if err := os.WriteFile(edited, []byte("- name: mine\n  type: cpu\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	_, err := MigrateToMultiFile(path, nil)
	var fe *FragmentsBesideMonolithicError
	if !errors.As(err, &fe) {
		t.Fatalf("want FragmentsBesideMonolithicError, got %v", err)
	}
	if len(fe.Fragments) != 1 || fe.Fragments[0] != edited {
		t.Errorf("error should name only %s, got %v", edited, fe.Fragments)
	}
	if _, err := os.Stat(filepath.Join(dir, "probes.d", "00-host.yaml")); err != nil {
		t.Errorf("a refused split must leave the defaults in place: %v", err)
	}
	if !strings.Contains(err.Error(), "10-mine.yaml") {
		t.Errorf("message should name the file: %v", err)
	}
}

// An edited 00-host.yaml is the operator's: not an install default.
func TestMigrateToMultiFile_EditedDefaultIsUserContent(t *testing.T) {
	dir := t.TempDir()
	path := writeMonolithicFixture(t, dir)
	seedInstallDefaults(t, dir)
	host := filepath.Join(dir, "probes.d", "00-host.yaml")
	if err := os.WriteFile(host, []byte(HostProbesFragmentTemplate+"\n- name: extra\n  type: disk\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := MigrateToMultiFile(path, nil)
	var fe *FragmentsBesideMonolithicError
	if !errors.As(err, &fe) {
		t.Fatalf("an edited default must be refused, got %v", err)
	}
}

// A split that fails after the defaults were set aside puts them back.
func TestMigrateToMultiFile_FailureRestoresDefaults(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "agent-config.yaml")
	src := "config_version: 2\nagent:\n  key: \"k\"\nstorage:\n  - params: {port: 8080}\nprobes:\n  - name: cpu\n    type: cpu\n"
	if err := os.WriteFile(path, []byte(src), 0o600); err != nil {
		t.Fatal(err)
	}
	seedInstallDefaults(t, dir)
	if _, err := MigrateToMultiFile(path, nil); err == nil {
		t.Fatal("a strategy without a name must fail the split")
	}
	if _, err := os.Stat(filepath.Join(dir, "probes.d", "00-host.yaml")); err != nil {
		t.Errorf("default probes not restored: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "probes.d", "00-host.yaml.install-default")); err == nil {
		t.Error("the aside copy should be gone once restored")
	}
}

// The loader runs several times per start; the legacy notice is one line.
func TestLoadFromDisk_LegacyWarningOncePerFile(t *testing.T) {
	dir := t.TempDir()
	path := writeMonolithicFixture(t, dir)
	if err := os.MkdirAll(filepath.Join(dir, "probes.d"), 0o750); err != nil {
		t.Fatal(err)
	}
	// A fragment of the operator: the split will not run, so the notice is due.
	if err := os.WriteFile(filepath.Join(dir, "probes.d", "50-mine.yaml"), []byte(operatorFragment), 0o600); err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	zl := zerolog.New(&buf)
	log := logger.NewModuleLogger((*logger.Logger)(&zl), "configuration.loader.test")
	for i := 0; i < 4; i++ {
		if _, err := LoadFromDisk(path, log); err != nil {
			t.Fatal(err)
		}
	}
	if n := strings.Count(buf.String(), "Legacy monolithic config detected"); n != 1 {
		t.Errorf("legacy warning logged %d times, want 1", n)
	}
}

// harmoniseLayout hands the refusal back instead of logging it, so Start
// reports it once and skips a seal that would only repeat it.
func TestHarmoniseLayout_ReturnsTheRefusal(t *testing.T) {
	dir := t.TempDir()
	path := writeMonolithicFixture(t, dir)
	if err := os.MkdirAll(filepath.Join(dir, "strategies.d"), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "strategies.d", "50-mine.yaml"), []byte("otlp:\n  endpoint: x:4317\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	backup, err := harmoniseLayout(path, nil)
	var fe *FragmentsBesideMonolithicError
	if backup != "" || !errors.As(err, &fe) {
		t.Errorf("want the refusal returned, got backup=%q err=%v", backup, err)
	}
}

// A host installed with 0.5.x and upgraded through 0.6.0 keeps the 0.5.x
// 00-http.yaml and a copy of its otlp strategy that 0.6.0 wrote beside a
// still monolithic agent.yaml. The split must complete and keep the otlp
// strategy of agent.yaml.
func TestMigrateToMultiFile_HalfMigratedBy060(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "agent.yaml")
	body := `config_version: 3
agent:
  key: "migrate-test-key"
storage:
  - name: otlp
    params:
      endpoint: "ingest.example.test:443"
      protocol: http
probes:
  - name: cpu
    type: cpu
`
	files := map[string]string{
		path: body,
		filepath.Join(dir, "strategies.d", "00-http.yaml"): fmt.Sprintf(httpStrategyFragmentTemplate05,
			8080, `"127.0.0.1"`, `["prtg", "web", "nagios"]`),
		filepath.Join(dir, "strategies.d", "10-otlp.yaml"): "# Strategy 'otlp' migrated from monolithic config on 20260929-224101.\n" +
			"otlp:\n  endpoint: \"ingest.example.test:443\"\n  protocol: http\n",
	}
	for p, b := range files {
		if err := os.MkdirAll(filepath.Dir(p), 0o750); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(b), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if !isInstallDefaultFragment(filepath.Join(dir, "strategies.d", "00-http.yaml")) {
		t.Fatal("the 0.5.x install 00-http.yaml is not recognised")
	}
	if _, err := MigrateToMultiFile(path, nil); err != nil {
		t.Fatalf("split refused: %v", err)
	}
	after, err := LoadFromDisk(path, nil)
	if err != nil {
		t.Fatal(err)
	}
	var otlp, http int
	for _, s := range after.Storage {
		switch s.Name {
		case "otlp":
			otlp++
		case "http":
			http++
		}
	}
	if otlp != 1 || http != 0 {
		t.Fatalf("after the split: %d otlp, %d http strategies, want 1 and 0 (as before)", otlp, http)
	}
}
