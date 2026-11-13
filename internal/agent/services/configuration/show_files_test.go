package configuration

import (
	"bytes"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"

	"senhub-agent.go/internal/agent/services/configuration/secret"
)

func writeTestFile(t *testing.T, root, rel, content string) {
	t.Helper()
	path := filepath.Join(root, rel)
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0600); err != nil {
		t.Fatal(err)
	}
}

func setMemorySecret(t *testing.T, name, value string) {
	t.Helper()
	mp := secret.NewMemoryProvider()
	if err := mp.Set(name, secret.New(value)); err != nil {
		t.Fatal(err)
	}
	secret.SetProvider(mp)
	t.Cleanup(func() { secret.SetProvider(nil) })
}

func writeMultiFileTree(t *testing.T, root string) {
	t.Helper()
	writeTestFile(t, root, "agent.yaml", `config_version: 3
agent:
  key: inline-agent-key-0123
`)
	writeTestFile(t, root, "probes.d/01-a.yaml", `- name: a
  type: cpu
  params:
    host: ${env:SHOWFILES_HOST}
    password: InlinePassw0rd
    token_ref: ${secret:probe-a.password}
`)
	writeTestFile(t, root, "probes.d/.hidden.yaml", "- name: hidden\n  type: cpu\n")
	writeTestFile(t, root, "probes.d/02-off.yaml.disabled", "- name: off\n  type: cpu\n")
	writeTestFile(t, root, "strategies.d/10-otlp.yaml", `otlp:
  endpoint: https://collector.example:4318
  headers:
    Authorization: Bearer ${secret:probe-a.password}
`)
}

func TestRedactedConfigFilesMultiFile(t *testing.T) {
	tmp := t.TempDir()
	setMemorySecret(t, "probe-a.password", "S3cr3t-From-Store")
	t.Setenv("SHOWFILES_HOST", "db.example")
	writeMultiFileTree(t, tmp)

	files, err := RedactedConfigFiles(filepath.Join(tmp, "agent.yaml"), nil)
	if err != nil {
		t.Fatal(err)
	}

	want := []string{"agent.yaml", "probes.d/01-a.yaml", "strategies.d/10-otlp.yaml"}
	if len(files) != len(want) {
		t.Fatalf("got %d files %#v, want names %v", len(files), files, want)
	}
	for i, name := range want {
		if files[i].Name != name {
			t.Errorf("file %d name = %q, want %q", i, files[i].Name, name)
		}
	}

	bodyByName := map[string][]byte{}
	for _, f := range files {
		bodyByName[f.Name] = f.Body
	}

	for name, body := range bodyByName {
		if bytes.Contains(body, []byte("inline-agent-key-0123")) ||
			bytes.Contains(body, []byte("InlinePassw0rd")) ||
			bytes.Contains(body, []byte("S3cr3t-From-Store")) {
			t.Errorf("%s body leaked a secret: %s", name, body)
		}
	}

	probeBody := bodyByName["probes.d/01-a.yaml"]
	if !bytes.Contains(probeBody, []byte("db.example")) {
		t.Errorf("probes body should contain resolved non-secret db.example: %s", probeBody)
	}
	if !bytes.Contains(probeBody, []byte("***")) {
		t.Errorf("probes body should contain redacted secret marker: %s", probeBody)
	}

	otlpBody := bodyByName["strategies.d/10-otlp.yaml"]
	if !bytes.Contains(otlpBody, []byte("https://collector.example:4318")) {
		t.Errorf("strategies body should contain endpoint: %s", otlpBody)
	}
}

func TestRedactedConfigFilesParityWithConfigShow(t *testing.T) {
	tmp := t.TempDir()
	setMemorySecret(t, "probe-a.password", "S3cr3t-From-Store")
	t.Setenv("SHOWFILES_HOST", "db.example")
	writeMultiFileTree(t, tmp)

	configPath := filepath.Join(tmp, "agent.yaml")
	files, err := RedactedConfigFiles(configPath, nil)
	if err != nil {
		t.Fatal(err)
	}
	merged, err := LoadForShow(configPath, ShowRedact, nil)
	if err != nil {
		t.Fatal(err)
	}

	var probeBody, strategyBody []byte
	for _, f := range files {
		switch f.Name {
		case "probes.d/01-a.yaml":
			probeBody = f.Body
		case "strategies.d/10-otlp.yaml":
			strategyBody = f.Body
		}
	}
	if probeBody == nil || strategyBody == nil {
		t.Fatalf("missing expected file bodies; files=%#v", files)
	}

	var probes []ProbeConfig
	if err := yaml.Unmarshal(probeBody, &probes); err != nil {
		t.Fatalf("unmarshal probe body: %v", err)
	}

	var paramMap map[string]StorageConfigParams
	if err := yaml.Unmarshal(strategyBody, &paramMap); err != nil {
		t.Fatalf("unmarshal strategy body: %v", err)
	}

	mergedProbeBytes, err := MarshalSortedYAML(merged.Probes)
	if err != nil {
		t.Fatal(err)
	}
	var mergedProbes []ProbeConfig
	if err := yaml.Unmarshal(mergedProbeBytes, &mergedProbes); err != nil {
		t.Fatal(err)
	}

	if !reflect.DeepEqual(probes, mergedProbes) {
		t.Errorf("probe file redaction differs from config show:\n got: %#v\nwant: %#v", probes, mergedProbes)
	}

	if len(merged.Storage) != 1 {
		t.Fatalf("merged storage length = %d, want 1", len(merged.Storage))
	}
	paramsBytes, err := MarshalSortedYAML(merged.Storage[0].Params)
	if err != nil {
		t.Fatal(err)
	}
	var mergedParams StorageConfigParams
	if err := yaml.Unmarshal(paramsBytes, &mergedParams); err != nil {
		t.Fatal(err)
	}

	if !reflect.DeepEqual(paramMap["otlp"], mergedParams) {
		t.Errorf("strategy file redaction differs from config show:\n got: %#v\nwant: %#v", paramMap["otlp"], mergedParams)
	}
}

func TestRedactedConfigFilesLegacy(t *testing.T) {
	tmp := t.TempDir()
	path := filepath.Join(tmp, "agent-config.yaml")
	writeTestFile(t, tmp, "agent-config.yaml", `agent:
  key: k-legacy-secret
storage:
  - name: http
    params:
      api_key: abc-secret
probes:
  - name: p
    type: cpu
    params:
      password: pw-secret
`)

	files, err := RedactedConfigFiles(path, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 1 {
		t.Fatalf("got %d files, want 1: %#v", len(files), files)
	}
	if files[0].Name != "agent-config.yaml" {
		t.Errorf("name = %q, want agent-config.yaml", files[0].Name)
	}

	data, err := LoadForShow(path, ShowRedact, nil)
	if err != nil {
		t.Fatal(err)
	}
	wantBody, err := MarshalSortedYAML(&data)
	if err != nil {
		t.Fatal(err)
	}

	if !bytes.Equal(files[0].Body, wantBody) {
		t.Errorf("legacy body differs from config show:\n got: %s\nwant: %s", files[0].Body, wantBody)
	}
	for _, s := range []string{"k-legacy-secret", "abc-secret"} {
		if bytes.Contains(files[0].Body, []byte(s)) {
			t.Errorf("body leaked %q: %s", s, files[0].Body)
		}
	}
}

func TestRedactedConfigFilesEnvironment(t *testing.T) {
	tmp := t.TempDir()
	writeTestFile(t, tmp, "agent.yaml", "config_version: 3\nagent:\n  key: inline\n")
	if err := os.MkdirAll(filepath.Join(tmp, "probes.d"), 0755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("SENHUB_PROBE_ENVSHOW_TYPE", "showfiles_test_type")
	t.Setenv("SENHUB_PROBE_ENVSHOW_PASSWORD", "EnvPassw0rd")

	files, err := RedactedConfigFiles(filepath.Join(tmp, "agent.yaml"), nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(files) == 0 {
		t.Fatal("no files returned")
	}
	last := files[len(files)-1]
	if last.Name != "environment" {
		t.Fatalf("last entry = %q, want environment", last.Name)
	}
	lower := strings.ToLower(string(last.Body))
	if !strings.Contains(lower, "envshow") {
		t.Errorf("environment body should contain probe name envshow: %s", last.Body)
	}
	if strings.Contains(string(last.Body), "EnvPassw0rd") {
		t.Errorf("environment body leaked env secret: %s", last.Body)
	}
}

func TestRedactedConfigFilesStrategyTwoKeysError(t *testing.T) {
	tmp := t.TempDir()
	writeTestFile(t, tmp, "agent.yaml", "config_version: 3\n")
	writeTestFile(t, tmp, "strategies.d/bad.yaml", "otlp:\n  endpoint: a\nhttp:\n  url: b\n")

	_, err := RedactedConfigFiles(filepath.Join(tmp, "agent.yaml"), nil)
	if err == nil {
		t.Fatal("expected error for two top-level keys")
	}
	if !strings.Contains(err.Error(), "expected exactly one top-level strategy key") {
		t.Errorf("error %q should contain the exact loadStrategiesD message", err)
	}
}

func TestRedactedConfigFilesMissingConfigError(t *testing.T) {
	tmp := t.TempDir()
	missing := filepath.Join(tmp, "missing.yaml")

	_, err := RedactedConfigFiles(missing, nil)
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), missing) {
		t.Errorf("error %q should mention %q", err, missing)
	}
}
