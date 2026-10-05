package app

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/kardianos/service"

	"senhub-agent.go/internal/agent/cliArgs"
	"senhub-agent.go/internal/cliexit"
)

const testAgentYAML = `config_version: 3
agent:
  key: 0f4c2a51-9b7e-4d3a-8c61-2e5f7a9b0d13
  mode: offline
`

const testHTTPFragment = `# the http output
http:
  port: 8080
  bind_address: 127.0.0.1
`

// seedConfigTree writes the multi-file layout `install` generates, the way
// generateConfiguration does it: create what is missing, leave what is
// there alone.
func seedConfigTree(args *cliArgs.ParsedArgs) error {
	dir := filepath.Dir(args.ConfigPath)
	for _, sub := range []string{"probes.d", "strategies.d"} {
		if err := os.MkdirAll(filepath.Join(dir, sub), 0o750); err != nil {
			return err
		}
	}
	files := map[string]string{
		args.ConfigPath: testAgentYAML,
		filepath.Join(dir, "strategies.d", "00-http.yaml"): testHTTPFragment,
	}
	for path, body := range files {
		if _, err := os.Stat(path); err == nil {
			continue
		}
		if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
			return err
		}
	}
	return nil
}

func testInitEnv() initEnv {
	return initEnv{
		generate:  seedConfigTree,
		checkPort: func(string, int) error { return nil },
	}
}

// backdate sets every file under dir to an old modification time, so a
// later rewrite shows as a different mtime however fast the test runs.
func backdate(t *testing.T, dir string) map[string]time.Time {
	t.Helper()
	old := time.Now().Add(-48 * time.Hour).Truncate(time.Second)
	times := map[string]time.Time{}
	err := filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		if err := os.Chtimes(path, old, old); err != nil {
			return err
		}
		times[path] = old
		return nil
	})
	if err != nil {
		t.Fatalf("backdating %s: %v", dir, err)
	}
	return times
}

func requireMtimes(t *testing.T, want map[string]time.Time) {
	t.Helper()
	for path, mtime := range want {
		info, err := os.Stat(path)
		if err != nil {
			t.Errorf("%s vanished: %v", path, err)
			continue
		}
		if !info.ModTime().Equal(mtime) {
			t.Errorf("%s was rewritten: mtime %v, want %v", path, info.ModTime(), mtime)
		}
	}
}

func TestConfigInitSecondRunIsUnchanged(t *testing.T) {
	dir := t.TempDir()
	cfg := filepath.Join(dir, "agent.yaml")
	argv := []string{"--config-path", cfg, "--otlp-endpoint", "otlp.example.com:4317", "--tags", "env=test"}

	var out bytes.Buffer
	if code := runConfigInit(argv, &out, testInitEnv()); code != cliexit.OK {
		t.Fatalf("first run exit code = %d, want %d\n%s", code, cliexit.OK, out.String())
	}
	if _, err := os.Stat(filepath.Join(dir, "strategies.d", "10-otlp.yaml")); err != nil {
		t.Fatalf("the first run did not write the OTLP fragment: %v", err)
	}

	mtimes := backdate(t, dir)
	out.Reset()
	if code := runConfigInit(argv, &out, testInitEnv()); code != cliexit.Unchanged {
		t.Fatalf("second run exit code = %d, want %d\n%s", code, cliexit.Unchanged, out.String())
	}
	requireMtimes(t, mtimes)
}

func TestConfigInitUnchangedJSON(t *testing.T) {
	dir := t.TempDir()
	argv := []string{"--config-path", filepath.Join(dir, "agent.yaml"), "--json"}

	var out bytes.Buffer
	if code := runConfigInit(argv, &out, testInitEnv()); code != cliexit.OK {
		t.Fatalf("first run exit code = %d\n%s", code, out.String())
	}
	first := decodeJSONDocument(t, out.String())
	requireHeader(t, first, "senhub.cli.config.init/v1", cliexit.OK)
	if first["changed"] != true || first["created"] != true {
		t.Errorf("first run: changed/created = %v/%v, want true/true", first["changed"], first["created"])
	}

	out.Reset()
	if code := runConfigInit(argv, &out, testInitEnv()); code != cliexit.Unchanged {
		t.Fatalf("second run exit code = %d, want %d\n%s", code, cliexit.Unchanged, out.String())
	}
	second := decodeJSONDocument(t, out.String())
	requireHeader(t, second, "senhub.cli.config.init/v1", cliexit.Unchanged)
	if second["changed"] != false || second["created"] != false {
		t.Errorf("second run: changed/created = %v/%v, want false/false", second["changed"], second["created"])
	}
	if written, _ := second["written"].([]any); len(written) != 0 {
		t.Errorf("second run lists written files: %v", written)
	}
}

func TestConfigInitOkIfUnchanged(t *testing.T) {
	cfg := filepath.Join(t.TempDir(), "agent.yaml")
	var out bytes.Buffer
	if code := runConfigInit([]string{"--config-path", cfg}, &out, testInitEnv()); code != cliexit.OK {
		t.Fatalf("first run exit code = %d", code)
	}
	out.Reset()
	if code := runConfigInit([]string{"--config-path", cfg, "--ok-if-unchanged"}, &out, testInitEnv()); code != cliexit.OK {
		t.Errorf("--ok-if-unchanged exit code = %d, want %d", code, cliexit.OK)
	}
}

// A kept configuration gains a fragment the run asks for: that is a
// change, the way it always was, and exits OK.
func TestConfigInitKeptConfigGainsRequestedFragment(t *testing.T) {
	dir := t.TempDir()
	cfg := filepath.Join(dir, "agent.yaml")
	var out bytes.Buffer
	if code := runConfigInit([]string{"--config-path", cfg}, &out, testInitEnv()); code != cliexit.OK {
		t.Fatalf("first run exit code = %d", code)
	}
	mtimes := backdate(t, dir)

	out.Reset()
	code := runConfigInit([]string{"--config-path", cfg, "--otlp-endpoint", "otlp.example.com:4317"}, &out, testInitEnv())
	if code != cliexit.OK {
		t.Fatalf("exit code = %d, want %d", code, cliexit.OK)
	}
	if _, err := os.Stat(filepath.Join(dir, "strategies.d", "10-otlp.yaml")); err != nil {
		t.Errorf("the requested fragment was not written: %v", err)
	}
	requireMtimes(t, mtimes)
}

func TestConfigInitFailures(t *testing.T) {
	var out bytes.Buffer
	if code := runConfigInit([]string{"--bogus"}, &out, testInitEnv()); code != cliexit.Failure {
		t.Errorf("unknown flag exit code = %d, want %d", code, cliexit.Failure)
	}

	out.Reset()
	busy := testInitEnv()
	busy.checkPort = func(string, int) error { return errors.New("port in use") }
	cfg := filepath.Join(t.TempDir(), "agent.yaml")
	if code := runConfigInit([]string{"--config-path", cfg, "--json"}, &out, busy); code != cliexit.Failure {
		t.Fatalf("busy port exit code = %d, want %d", code, cliexit.Failure)
	}
	requireHeader(t, decodeJSONDocument(t, out.String()), "senhub.cli.config.init/v1", cliexit.Failure)
	if _, err := os.Stat(cfg); err == nil {
		t.Error("a refused init left a configuration behind")
	}
}

func TestConfigSetSecondRunIsUnchanged(t *testing.T) {
	dir := t.TempDir()
	cfg := filepath.Join(dir, "agent.yaml")
	if err := seedConfigTree(&cliArgs.ParsedArgs{ConfigPath: cfg}); err != nil {
		t.Fatalf("seeding: %v", err)
	}
	argv := []string{"http.port", "9080", "--config-path", cfg}

	var out bytes.Buffer
	if code := runConfigSet(argv, &out); code != cliexit.OK {
		t.Fatalf("first run exit code = %d, want %d\n%s", code, cliexit.OK, out.String())
	}
	fragment := filepath.Join(dir, "strategies.d", "00-http.yaml")
	body, err := os.ReadFile(fragment)
	if err != nil || !bytes.Contains(body, []byte("port: 9080")) {
		t.Fatalf("the value was not written: %v\n%s", err, body)
	}

	mtimes := backdate(t, dir)
	out.Reset()
	if code := runConfigSet(argv, &out); code != cliexit.Unchanged {
		t.Fatalf("second run exit code = %d, want %d\n%s", code, cliexit.Unchanged, out.String())
	}
	requireMtimes(t, mtimes)

	out.Reset()
	if code := runConfigSet([]string{"http.port", "9081", "--config-path", cfg}, &out); code != cliexit.OK {
		t.Errorf("a different value exit code = %d, want %d", code, cliexit.OK)
	}
}

func TestConfigSetJSON(t *testing.T) {
	dir := t.TempDir()
	cfg := filepath.Join(dir, "agent.yaml")
	if err := seedConfigTree(&cliArgs.ParsedArgs{ConfigPath: cfg}); err != nil {
		t.Fatalf("seeding: %v", err)
	}
	argv := []string{"--json", "http.bind_address", "0.0.0.0", "--config-path", cfg}

	var out bytes.Buffer
	if code := runConfigSet(argv, &out); code != cliexit.OK {
		t.Fatalf("first run exit code = %d\n%s", code, out.String())
	}
	first := decodeJSONDocument(t, out.String())
	requireHeader(t, first, "senhub.cli.config.set/v1", cliexit.OK)
	if first["changed"] != true || first["key"] != "http.bind_address" || first["value"] != "0.0.0.0" {
		t.Errorf("first report: %v", first)
	}

	out.Reset()
	if code := runConfigSet(argv, &out); code != cliexit.Unchanged {
		t.Fatalf("second run exit code = %d, want %d", code, cliexit.Unchanged)
	}
	second := decodeJSONDocument(t, out.String())
	requireHeader(t, second, "senhub.cli.config.set/v1", cliexit.Unchanged)
	if second["changed"] != false {
		t.Errorf("second report: %v", second)
	}
}

func TestConfigSetFailures(t *testing.T) {
	cfg := filepath.Join(t.TempDir(), "agent.yaml")
	if err := seedConfigTree(&cliArgs.ParsedArgs{ConfigPath: cfg}); err != nil {
		t.Fatalf("seeding: %v", err)
	}
	cases := map[string][]string{
		"unknown key":   {"nope.key", "1", "--config-path", cfg},
		"invalid value": {"http.port", "99999", "--config-path", cfg},
		"missing value": {"http.port", "--config-path", cfg},
		"missing file":  {"http.port", "9080", "--config-path", filepath.Join(t.TempDir(), "absent.yaml")},
	}
	for name, argv := range cases {
		var out bytes.Buffer
		if code := runConfigSet(append([]string{"--json"}, argv...), &out); code != cliexit.Failure {
			t.Errorf("%s: exit code = %d, want %d", name, code, cliexit.Failure)
			continue
		}
		requireHeader(t, decodeJSONDocument(t, out.String()), "senhub.cli.config.set/v1", cliexit.Failure)
	}
}

type installFakeService struct {
	state service.Status
	err   error
}

func (f installFakeService) Status() (service.Status, error) { return f.state, f.err }

func TestInstallStateDecidesNothingToDo(t *testing.T) {
	cfg := filepath.Join(t.TempDir(), "agent.yaml")
	if err := os.WriteFile(cfg, []byte(testAgentYAML), 0o600); err != nil {
		t.Fatalf("writing config: %v", err)
	}
	current := func() bool { return true }
	stale := func() bool { return false }

	cases := []struct {
		name string
		svc  installFakeService
		path string
		bin  func() bool
		want bool
	}{
		{"installed, config, binary current", installFakeService{state: service.StatusRunning}, cfg, current, true},
		{"installed but stopped", installFakeService{state: service.StatusStopped}, cfg, current, true},
		{"service not installed", installFakeService{err: service.ErrNotInstalled}, cfg, current, false},
		{"service manager cannot say", installFakeService{err: errors.New("failed state")}, cfg, current, false},
		{"configuration missing", installFakeService{state: service.StatusRunning}, cfg + ".absent", current, false},
		{"binary differs", installFakeService{state: service.StatusRunning}, cfg, stale, false},
	}
	for _, c := range cases {
		got := detectInstallState(c.svc, c.path, c.bin).alreadyDone()
		if got != c.want {
			t.Errorf("%s: alreadyDone = %v, want %v", c.name, got, c.want)
		}
	}
}

func TestInstallConfigurationStepIsIdempotent(t *testing.T) {
	dir := t.TempDir()
	cfg := filepath.Join(dir, "agent.yaml")
	args := &cliArgs.ParsedArgs{ConfigPath: cfg}

	changed, err := generateConfigurationReport(args, cfg, seedConfigTree)
	if err != nil || !changed {
		t.Fatalf("first run: changed = %v, err = %v; want a created configuration", changed, err)
	}

	mtimes := backdate(t, dir)
	changed, err = generateConfigurationReport(args, cfg, seedConfigTree)
	if err != nil {
		t.Fatalf("second run: %v", err)
	}
	if changed {
		t.Error("second run reported a change on an unchanged configuration")
	}
	requireMtimes(t, mtimes)
}

func TestInstallConfigurationStepNoticesAWrite(t *testing.T) {
	dir := t.TempDir()
	cfg := filepath.Join(dir, "agent.yaml")
	if err := seedConfigTree(&cliArgs.ParsedArgs{ConfigPath: cfg}); err != nil {
		t.Fatalf("seeding: %v", err)
	}
	touch := func(*cliArgs.ParsedArgs) error {
		return os.WriteFile(filepath.Join(dir, "strategies.d", "10-new.yaml"), []byte("otlp:\n  endpoint: x:1\n"), 0o600)
	}
	changed, err := generateConfigurationReport(&cliArgs.ParsedArgs{ConfigPath: cfg}, cfg, touch)
	if err != nil || !changed {
		t.Errorf("a new fragment went unnoticed: changed = %v, err = %v", changed, err)
	}
}

func TestInstallConfigurationStepPropagatesFailure(t *testing.T) {
	cfg := filepath.Join(t.TempDir(), "agent.yaml")
	boom := func(*cliArgs.ParsedArgs) error { return errors.New("disk full") }
	if _, err := generateConfigurationReport(&cliArgs.ParsedArgs{ConfigPath: cfg}, cfg, boom); err == nil {
		t.Error("a failing generator was reported as success")
	}
}
