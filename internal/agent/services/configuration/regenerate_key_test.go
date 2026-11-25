package configuration

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"

	"senhub-agent.go/internal/agent/services/instanceid"
)

func TestNewAgentKey(t *testing.T) {
	a, err := NewAgentKey()
	if err != nil {
		t.Fatal(err)
	}
	if !instanceid.IsRandomKey(a) {
		t.Fatalf("%s is not a random UUID v4", a)
	}
	b, err := NewAgentKey()
	if err != nil {
		t.Fatal(err)
	}
	if a == b {
		t.Fatal("two generated keys are identical")
	}
}

func TestReplaceAgentKeyPlaintext(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "agent.yaml")
	in := `# operator comment, must survive
agent:
  key: "old-plain-key"
  other: keep-me
tags:
  env: prod
`
	if err := os.WriteFile(path, []byte(in), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := ReplaceAgentKey(path, "new-key"); err != nil {
		t.Fatal(err)
	}
	raw, _ := os.ReadFile(path)
	out := string(raw)
	if !strings.Contains(out, "key: \"new-key\"") {
		t.Fatalf("key not replaced:\n%s", out)
	}
	if strings.Contains(out, "old-plain-key") {
		t.Fatalf("old key still present:\n%s", out)
	}
	if !strings.Contains(out, "# operator comment, must survive") || !strings.Contains(out, "keep-me") || !strings.Contains(out, "env: prod") {
		t.Fatalf("comments or sibling keys lost:\n%s", out)
	}
	var doc map[string]any
	if err := yaml.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	agent := doc["agent"].(map[string]any)
	if agent["key"] != "new-key" || agent["other"] != "keep-me" {
		t.Fatalf("parsed values: %v", agent)
	}
}

func TestReplaceAgentKeySealed(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "agent.yaml")
	in := "agent:\n  key: ${secret:agent.key}\n"
	if err := os.WriteFile(path, []byte(in), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := ReplaceAgentKey(path, "new-key"); err != nil {
		t.Fatal(err)
	}
	raw, _ := os.ReadFile(path)
	if !strings.Contains(string(raw), "key: \"new-key\"") {
		t.Fatalf("sealed reference not replaced:\n%s", raw)
	}
}

func TestReplaceAgentKeyMissingBlock(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "agent.yaml")
	if err := os.WriteFile(path, []byte("tags:\n  env: prod\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := ReplaceAgentKey(path, "new-key"); err == nil {
		t.Fatal("expected an error without an agent block")
	}
	if err := os.WriteFile(path, []byte("agent:\n  other: 1\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := ReplaceAgentKey(path, "new-key"); err == nil {
		t.Fatal("expected an error without agent.key")
	}
}

func TestReplaceAdminKey(t *testing.T) {
	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, "strategies.d"), 0o755); err != nil {
		t.Fatal(err)
	}
	configPath := filepath.Join(dir, "agent.yaml")
	if err := os.WriteFile(configPath, []byte("agent:\n  key: \"k\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	frag := filepath.Join(dir, "strategies.d", "http.yaml")
	if err := os.WriteFile(frag, []byte("http:\n  port: 8080\n  admin_key: \"old-admin\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := ReplaceAdminKey(configPath, "new-admin"); err != nil {
		t.Fatal(err)
	}
	raw, _ := os.ReadFile(frag)
	out := string(raw)
	if !strings.Contains(out, "admin_key: \"new-admin\"") || strings.Contains(out, "old-admin") || !strings.Contains(out, "port: 8080") {
		t.Fatalf("admin_key not replaced cleanly:\n%s", out)
	}
}
