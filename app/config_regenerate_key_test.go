package app

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"senhub-agent.go/internal/agent/services/configuration"
	"senhub-agent.go/internal/cliexit"
)

const oldTestKey = "11111111-1111-4111-8111-111111111111"

func writeKeyedConfig(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "agent.yaml")
	if err := os.WriteFile(path, []byte("agent:\n  key: \""+oldTestKey+"\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

var keyLine = regexp.MustCompile(`key: "(.+)"`)

func readAgentKey(t *testing.T, path string) string {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	m := keyLine.FindSubmatch(raw)
	if m == nil {
		t.Fatalf("no agent key in %s:\n%s", path, raw)
	}
	return string(m[1])
}

func TestRunConfigRegenerateKeyYes(t *testing.T) {
	path := writeKeyedConfig(t)
	var out, errOut strings.Builder
	code := runConfigRegenerateKey([]string{"--yes", "--config-path", path},
		strings.NewReader(""), false, &out, &errOut)
	if code != cliexit.OK {
		t.Fatalf("code = %d, stderr: %s", code, errOut.String())
	}
	newKey := readAgentKey(t, path)
	if newKey == oldTestKey {
		t.Fatal("key unchanged")
	}
	if !strings.Contains(out.String(), configuration.AgentInstanceID(newKey)) {
		t.Fatalf("output lacks the new instance id:\n%s", out.String())
	}
	if strings.Contains(out.String(), oldTestKey) {
		t.Fatalf("output leaks the old key:\n%s", out.String())
	}
}

func TestRunConfigRegenerateKeyNonInteractive(t *testing.T) {
	path := writeKeyedConfig(t)
	var out, errOut strings.Builder
	code := runConfigRegenerateKey([]string{"--config-path", path},
		strings.NewReader(""), false, &out, &errOut)
	if code != cliexit.Failure {
		t.Fatalf("code = %d, want Failure", code)
	}
	if got := readAgentKey(t, path); got != oldTestKey {
		t.Fatalf("file changed: %s", got)
	}
}

func TestRunConfigRegenerateKeyDeclined(t *testing.T) {
	path := writeKeyedConfig(t)
	var out, errOut strings.Builder
	code := runConfigRegenerateKey([]string{"--config-path", path},
		strings.NewReader("n\n"), true, &out, &errOut)
	if code != cliexit.OK {
		t.Fatalf("code = %d, want OK", code)
	}
	if !strings.Contains(out.String(), "Cancelled.") {
		t.Fatalf("output: %s", out.String())
	}
	if got := readAgentKey(t, path); got != oldTestKey {
		t.Fatalf("file changed: %s", got)
	}
}

func TestRunConfigRegenerateKeyJSON(t *testing.T) {
	path := writeKeyedConfig(t)
	var out, errOut strings.Builder
	code := runConfigRegenerateKey([]string{"--yes", "--config-path", path, "--json"},
		strings.NewReader(""), false, &out, &errOut)
	if code != cliexit.OK {
		t.Fatalf("code = %d, stderr: %s", code, errOut.String())
	}
	var doc struct {
		Schema            string `json:"schema"`
		ServiceInstanceID string `json:"service_instance_id"`
		AgentKey          string `json:"agent_key"`
	}
	if err := json.Unmarshal([]byte(out.String()), &doc); err != nil {
		t.Fatalf("invalid JSON: %v\n%s", err, out.String())
	}
	if doc.ServiceInstanceID == "" || doc.AgentKey == "" {
		t.Fatalf("fields missing: %+v", doc)
	}
	if doc.ServiceInstanceID != configuration.AgentInstanceID(doc.AgentKey) {
		t.Fatalf("instance id does not match the key: %+v", doc)
	}
	if strings.Contains(out.String(), oldTestKey) {
		t.Fatalf("output leaks the old key:\n%s", out.String())
	}
}
