package app

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kardianos/service"

	"senhub-agent.go/internal/agent/cliArgs"
	"senhub-agent.go/internal/agent/services/configuration/secret"
	"senhub-agent.go/internal/cliexit"
)

// decodeJSONDocument parses out as the single JSON object a --json command
// owes its caller, failing on any stray byte around it.
func decodeJSONDocument(t *testing.T, out string) map[string]any {
	t.Helper()
	dec := json.NewDecoder(strings.NewReader(out))
	var doc map[string]any
	if err := dec.Decode(&doc); err != nil {
		t.Fatalf("stdout is not a JSON object: %v\n%s", err, out)
	}
	if dec.More() {
		t.Fatalf("stdout carries more than one JSON document:\n%s", out)
	}
	return doc
}

// requireHeader pins the part of the document every command shares.
func requireHeader(t *testing.T, doc map[string]any, schema string, code int) {
	t.Helper()
	if doc["schema"] != schema {
		t.Errorf("schema = %v, want %s", doc["schema"], schema)
	}
	if got := int(doc["exit_code"].(float64)); got != code {
		t.Errorf("exit_code = %d, want %d", got, code)
	}
	if doc["status"] != cliexit.Name(code) {
		t.Errorf("status = %v, want %s", doc["status"], cliexit.Name(code))
	}
	wantOK := code != cliexit.Failure
	if doc["ok"] != wantOK {
		t.Errorf("ok = %v, want %v", doc["ok"], wantOK)
	}
}

func copyExampleConfig(t *testing.T, name string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("..", "examples", name))
	if err != nil {
		t.Fatalf("reading example: %v", err)
	}
	path := filepath.Join(t.TempDir(), "agent-config.yaml")
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatalf("writing config: %v", err)
	}
	return path
}

func TestExtractJSONFlag(t *testing.T) {
	rest, on := extractJSONFlag([]string{"--json", "path.yaml", "--raw"})
	if !on || len(rest) != 2 || rest[0] != "path.yaml" || rest[1] != "--raw" {
		t.Errorf("extractJSONFlag = %v, %v", rest, on)
	}
	if _, on := extractJSONFlag([]string{"path.yaml"}); on {
		t.Error("no --json given, but the flag was reported")
	}
}

func TestVersionJSON(t *testing.T) {
	var out bytes.Buffer
	code := runVersion(true, &out)
	if code != cliexit.OK {
		t.Fatalf("exit code = %d, want %d", code, cliexit.OK)
	}
	doc := decodeJSONDocument(t, out.String())
	requireHeader(t, doc, "senhub.cli.version/v1", cliexit.OK)
	for _, field := range []string{"version", "commit"} {
		if _, ok := doc[field]; !ok {
			t.Errorf("missing field %q in %v", field, doc)
		}
	}
}

func TestConfigCheckJSONWarning(t *testing.T) {
	path := copyExampleConfig(t, "example-config-free-tier.yaml")
	var out bytes.Buffer
	code := runConfigCheck([]string{"--json", path}, &out)
	if code != cliexit.Warning {
		t.Fatalf("exit code = %d, want %d (the free-tier example warns)\n%s", code, cliexit.Warning, out.String())
	}
	doc := decodeJSONDocument(t, out.String())
	requireHeader(t, doc, "senhub.cli.config.check/v1", cliexit.Warning)
	if doc["errors"].(float64) != 0 || doc["warnings"].(float64) < 1 {
		t.Errorf("counts = errors %v warnings %v", doc["errors"], doc["warnings"])
	}
	findings, ok := doc["findings"].([]any)
	if !ok || len(findings) == 0 {
		t.Fatalf("findings missing or empty: %v", doc["findings"])
	}
	sawWarn := false
	for _, f := range findings {
		entry := f.(map[string]any)
		if entry["level"] == "" || entry["message"] == "" {
			t.Errorf("incomplete finding %v", entry)
		}
		if entry["level"] == "warn" {
			sawWarn = true
		}
	}
	if !sawWarn {
		t.Error("no finding at level warn")
	}
}

func TestConfigCheckJSONLoadFailure(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "absent.yaml")
	var out bytes.Buffer
	code := runConfigCheck([]string{missing, "--json"}, &out)
	if code != cliexit.Failure {
		t.Fatalf("exit code = %d, want %d", code, cliexit.Failure)
	}
	doc := decodeJSONDocument(t, out.String())
	requireHeader(t, doc, "senhub.cli.config.check/v1", cliexit.Failure)
	if msg, _ := doc["error"].(string); msg == "" {
		t.Errorf("a failed check carries no error message: %v", doc)
	}
}

func TestConfigCheckJSONBadArguments(t *testing.T) {
	var out bytes.Buffer
	code := runConfigCheck([]string{"--json", "--bogus"}, &out)
	if code != cliexit.Failure {
		t.Fatalf("exit code = %d, want %d", code, cliexit.Failure)
	}
	doc := decodeJSONDocument(t, out.String())
	requireHeader(t, doc, "senhub.cli.config.check/v1", cliexit.Failure)
}

func TestCheckOutcomeExitCode(t *testing.T) {
	cases := []struct {
		name string
		in   checkOutcome
		want int
	}{
		{"clean", checkOutcome{}, cliexit.OK},
		{"warnings", checkOutcome{warnings: 2}, cliexit.Warning},
		{"errors", checkOutcome{errors: 1, warnings: 2}, cliexit.Failure},
		{"load failure", checkOutcome{loadErr: errors.New("boom")}, cliexit.Failure},
	}
	for _, c := range cases {
		if got := c.in.exitCode(); got != c.want {
			t.Errorf("%s: exit code %d, want %d", c.name, got, c.want)
		}
	}
}

func TestParseCheckFindings(t *testing.T) {
	text := "Checking configuration: x\n\n  [OK]   a thing\n  [WARN] careful\n         [ERROR] nested\n  [OFF]  disabled\ncontinuation line\n"
	got := parseCheckFindings(text)
	levels := []string{"ok", "warn", "error", "off"}
	if len(got) != len(levels) {
		t.Fatalf("got %d findings, want %d: %v", len(got), len(levels), got)
	}
	for i, l := range levels {
		if got[i].Level != l {
			t.Errorf("finding %d level = %q, want %q", i, got[i].Level, l)
		}
	}
}

func TestConfigShowJSONRedactsByDefault(t *testing.T) {
	path := copyExampleConfig(t, "example-config-free-tier.yaml")
	var out bytes.Buffer
	code := runConfigShow([]string{"--json", path}, &out)
	if code != cliexit.OK {
		t.Fatalf("exit code = %d, want %d\n%s", code, cliexit.OK, out.String())
	}
	doc := decodeJSONDocument(t, out.String())
	requireHeader(t, doc, "senhub.cli.config.show/v1", cliexit.OK)
	if doc["mode"] != "redact" {
		t.Errorf("mode = %v, want redact", doc["mode"])
	}
	if _, ok := doc["config"].(map[string]any); !ok {
		t.Fatalf("config is not an object: %T", doc["config"])
	}
	if strings.Contains(out.String(), "test-agent-free-tier-12345") {
		t.Errorf("the agent key appears in the redacted JSON:\n%s", out.String())
	}
}

func TestConfigShowJSONResolvedIsExplicit(t *testing.T) {
	path := copyExampleConfig(t, "example-config-free-tier.yaml")
	var out bytes.Buffer
	if code := runConfigShow([]string{"--resolved", "--json", path}, &out); code != cliexit.OK {
		t.Fatalf("exit code = %d\n%s", code, out.String())
	}
	doc := decodeJSONDocument(t, out.String())
	if doc["mode"] != "resolved" {
		t.Errorf("mode = %v, want resolved", doc["mode"])
	}
	if !strings.Contains(out.String(), "test-agent-free-tier-12345") {
		t.Error("--resolved must show the value in the clear, as it does in text mode")
	}
}

func TestConfigShowJSONFailure(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "absent.yaml")
	var out bytes.Buffer
	code := runConfigShow([]string{"--json", missing}, &out)
	if code != cliexit.Failure {
		t.Fatalf("exit code = %d, want %d", code, cliexit.Failure)
	}
	doc := decodeJSONDocument(t, out.String())
	requireHeader(t, doc, "senhub.cli.config.show/v1", cliexit.Failure)
	if msg, _ := doc["error"].(string); msg == "" {
		t.Errorf("no error message: %v", doc)
	}
}

func TestConfigShowTextUnknownFlagFails(t *testing.T) {
	var out bytes.Buffer
	if code := runConfigShow([]string{"--bogus"}, &out); code != cliexit.Failure {
		t.Errorf("exit code = %d, want %d", code, cliexit.Failure)
	}
}

func TestSecretStatusJSON(t *testing.T) {
	p := secret.NewMemoryProvider()
	if err := p.Set("db_password", secret.New("hunter2")); err != nil {
		t.Fatalf("seeding secret: %v", err)
	}
	var out bytes.Buffer
	code := runSecretStatus(p, "/etc/senhub-agent", true, &out)
	if code != cliexit.OK {
		t.Fatalf("exit code = %d", code)
	}
	doc := decodeJSONDocument(t, out.String())
	requireHeader(t, doc, "senhub.cli.secret.status/v1", cliexit.OK)
	if doc["backend"] != "memory" || doc["store"] != "/etc/senhub-agent" || doc["secrets"].(float64) != 1 {
		t.Errorf("unexpected report: %v", doc)
	}
	if strings.Contains(out.String(), "db_password") || strings.Contains(out.String(), "hunter2") {
		t.Errorf("secret status must not list names or values:\n%s", out.String())
	}
}

func TestSecretStatusTextUnchanged(t *testing.T) {
	var out bytes.Buffer
	code := runSecretStatus(secret.NewMemoryProvider(), "/cfg", false, &out)
	if code != cliexit.OK {
		t.Fatalf("exit code = %d", code)
	}
	if want := "backend: memory\nstore:   /cfg\nsecrets: 0\n"; out.String() != want {
		t.Errorf("text output = %q, want %q", out.String(), want)
	}
}

func TestReportFailureJSON(t *testing.T) {
	var out bytes.Buffer
	code := reportFailure("secret.status", true, &out, errors.New("backend gone"))
	if code != cliexit.Failure {
		t.Fatalf("exit code = %d", code)
	}
	doc := decodeJSONDocument(t, out.String())
	requireHeader(t, doc, "senhub.cli.secret.status/v1", cliexit.Failure)
	if doc["error"] != "backend gone" {
		t.Errorf("error = %v", doc["error"])
	}
}

// fakeService answers only Status; every other method would panic on the
// nil embedded interface, which is the point: the status command must not
// call them.
type fakeService struct {
	service.Service
	state service.Status
	err   error
}

func (f fakeService) Status() (service.Status, error) { return f.state, f.err }

func TestStatusJSONStoppedService(t *testing.T) {
	var out bytes.Buffer
	code := runStatus(fakeService{state: service.StatusStopped}, &cliArgs.ParsedArgs{}, true, &out)
	if code != cliexit.Warning {
		t.Fatalf("exit code = %d, want %d", code, cliexit.Warning)
	}
	doc := decodeJSONDocument(t, out.String())
	requireHeader(t, doc, "senhub.cli.status/v1", cliexit.Warning)
	svc := doc["service"].(map[string]any)
	if svc["state"] != "stopped" || svc["manager_available"] != true {
		t.Errorf("service = %v", svc)
	}
	if doc["source"] != "none" {
		t.Errorf("source = %v, want none", doc["source"])
	}
	if _, present := doc["agent"]; present {
		t.Error("a stopped service must not carry an agent block")
	}
}

func TestStatusJSONRunningWithoutDaemon(t *testing.T) {
	args := &cliArgs.ParsedArgs{ConfigPath: filepath.Join(t.TempDir(), "absent.yaml")}
	var out bytes.Buffer
	code := runStatus(fakeService{state: service.StatusRunning}, args, true, &out)
	if code != cliexit.Warning {
		t.Fatalf("exit code = %d, want %d: the daemon cannot be asked", code, cliexit.Warning)
	}
	doc := decodeJSONDocument(t, out.String())
	requireHeader(t, doc, "senhub.cli.status/v1", cliexit.Warning)
	if doc["source"] != "local" {
		t.Errorf("source = %v, want local", doc["source"])
	}
	agent, ok := doc["agent"].(map[string]any)
	if !ok {
		t.Fatalf("agent block missing: %v", doc)
	}
	for _, k := range []string{"health", "probes", "agent", "performance"} {
		if _, ok := agent[k]; !ok {
			t.Errorf("agent block lacks %q", k)
		}
	}
}

func TestStatusJSONNoServiceManager(t *testing.T) {
	args := &cliArgs.ParsedArgs{ConfigPath: filepath.Join(t.TempDir(), "absent.yaml")}
	var out bytes.Buffer
	code := runStatus(fakeService{err: errors.New("no init system")}, args, true, &out)
	if code != cliexit.Warning {
		t.Fatalf("exit code = %d, want %d", code, cliexit.Warning)
	}
	doc := decodeJSONDocument(t, out.String())
	svc := doc["service"].(map[string]any)
	if svc["manager_available"] != false || svc["state"] != "unknown" || svc["detail"] == "" {
		t.Errorf("service = %v", svc)
	}
}
