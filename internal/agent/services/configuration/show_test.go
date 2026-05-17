package configuration

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestShowConfig_Resolved verifies that ${env:..} references are
// substituted in ShowResolved mode and that the marshaled output
// reflects the resolved value.
func TestShowConfig_Resolved(t *testing.T) {
	t.Setenv("SENHUB_SHOW_TEST_PORT", "9999")
	dir := t.TempDir()
	configPath := filepath.Join(dir, "agent.yaml")
	writeFile(t, configPath, `
config_version: 2
agent:
  key: "${env:SENHUB_SHOW_TEST_PORT}"
`)

	data, err := LoadForShow(configPath, ShowResolved, loaderTestLogger(t))
	if err != nil {
		t.Fatalf("LoadForShow: %v", err)
	}
	if data.Agent.Key != "9999" {
		t.Errorf("resolved key = %q, want 9999", data.Agent.Key)
	}

	out, err := MarshalSortedYAML(&data)
	if err != nil {
		t.Fatalf("MarshalSortedYAML: %v", err)
	}
	if !strings.Contains(string(out), `key: "9999"`) {
		t.Errorf("marshaled output should include resolved key; got:\n%s", out)
	}
}

// TestShowConfig_Raw verifies that ${env:..} references stay literal
// in ShowRaw mode — the operator wants to audit the layout as
// written, not as boot would see it.
func TestShowConfig_Raw(t *testing.T) {
	t.Setenv("SENHUB_SHOW_TEST_RAW", "would-be-resolved")
	dir := t.TempDir()
	configPath := filepath.Join(dir, "agent.yaml")
	writeFile(t, configPath, `
config_version: 2
agent:
  key: "${env:SENHUB_SHOW_TEST_RAW}"
`)

	data, err := LoadForShow(configPath, ShowRaw, loaderTestLogger(t))
	if err != nil {
		t.Fatalf("LoadForShow: %v", err)
	}
	if data.Agent.Key != "${env:SENHUB_SHOW_TEST_RAW}" {
		t.Errorf("raw key should be unsubstituted, got %q", data.Agent.Key)
	}
}

// TestShowConfig_Redact verifies that:
//   - values that came from ${file:..} are masked with "***"
//   - fields whose YAML name matches the secret regex are masked
//   - the surrounding structure (non-secret fields, probe layout)
//     is preserved
func TestShowConfig_Redact(t *testing.T) {
	dir := t.TempDir()
	// File-backed secret. The redact pass must mask its resolved
	// value because the operator's intent was "load from a file" —
	// which by convention means "secret".
	secretFile := filepath.Join(dir, "db_password")
	writeFile(t, secretFile, "hunter2-from-disk")

	configPath := filepath.Join(dir, "agent.yaml")
	writeFile(t, configPath, `
config_version: 2
agent:
  key: "agent-key-12345"
  mode: offline
probes:
  - name: db
    type: mysql
    params:
      host: localhost
      password: "${file:`+secretFile+`}"
      api_token: "raw-token-no-ref"
storage:
  - name: http
    params:
      bind_address: 127.0.0.1
`)

	data, err := LoadForShow(configPath, ShowRedact, loaderTestLogger(t))
	if err != nil {
		t.Fatalf("LoadForShow: %v", err)
	}

	// agent.key — name matches secret regex → masked
	if data.Agent.Key != "***" {
		t.Errorf("agent.key should be masked; got %q", data.Agent.Key)
	}
	// agent.mode — not a secret name → unchanged
	if data.Agent.Mode != "offline" {
		t.Errorf("agent.mode should be preserved; got %q", data.Agent.Mode)
	}

	// probe.params.password — name matches AND was file-backed →
	// masked.
	pwd := data.Probes[0].Params["password"]
	if pwd != "***" {
		t.Errorf("probe.params.password should be masked; got %v", pwd)
	}
	// probe.params.api_token — name matches "token" → masked even
	// without a file reference.
	tok := data.Probes[0].Params["api_token"]
	if tok != "***" {
		t.Errorf("probe.params.api_token should be masked; got %v", tok)
	}
	// probe.params.host — not a secret name, no file ref → preserved.
	host := data.Probes[0].Params["host"]
	if host != "localhost" {
		t.Errorf("probe.params.host should be preserved; got %v", host)
	}
	// probe.name — name matches /name/ which does NOT match the
	// secret regex. Preserved.
	if data.Probes[0].Name != "db" {
		t.Errorf("probe.name should be preserved; got %q", data.Probes[0].Name)
	}
	// Storage layout still intact.
	if len(data.Storage) != 1 || data.Storage[0].Name != "http" {
		t.Errorf("storage entry should survive redact; got %+v", data.Storage)
	}
}

// TestShowConfig_MarshalSortedYAML asserts that map keys come out
// in alphabetical order regardless of insertion order. This is the
// whole reason MarshalSortedYAML exists — diffability.
func TestShowConfig_MarshalSortedYAML(t *testing.T) {
	type S struct {
		Map map[string]string `yaml:"map"`
	}
	v := S{Map: map[string]string{"z": "1", "a": "2", "m": "3"}}
	out, err := MarshalSortedYAML(&v)
	if err != nil {
		t.Fatal(err)
	}
	// Find positions of a:, m:, z: in the output.
	s := string(out)
	posA := strings.Index(s, "a:")
	posM := strings.Index(s, "m:")
	posZ := strings.Index(s, "z:")
	if !(posA < posM && posM < posZ) {
		t.Errorf("map keys not sorted (positions a=%d m=%d z=%d):\n%s", posA, posM, posZ, s)
	}
}

// TestShowConfig_RedactPreservesEnvRefValues verifies that values
// coming from ${env:..} (NOT ${file:..}) on non-secret fields are
// NOT masked. We want operators to see resolved env values; only
// file-backed values and secret-named fields are sensitive.
func TestShowConfig_RedactPreservesEnvRefValues(t *testing.T) {
	t.Setenv("SENHUB_SHOW_ENV_VAL", "visible")
	dir := t.TempDir()
	configPath := filepath.Join(dir, "agent.yaml")
	writeFile(t, configPath, `
config_version: 2
agent:
  mode: "${env:SENHUB_SHOW_ENV_VAL}"
`)
	data, err := LoadForShow(configPath, ShowRedact, loaderTestLogger(t))
	if err != nil {
		t.Fatalf("LoadForShow: %v", err)
	}
	if data.Agent.Mode != "visible" {
		t.Errorf("env-resolved non-secret field should NOT be masked; got %q", data.Agent.Mode)
	}
}

// TestShowConfig_DeterministicOutput is the diffability guarantee:
// two runs of the same input must produce byte-identical output.
func TestShowConfig_DeterministicOutput(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "agent.yaml")
	writeFile(t, configPath, `
config_version: 2
agent:
  key: "k1"
`)
	writeFile(t, filepath.Join(dir, "probes.d", "01.yaml"), `
- {name: cpu, type: cpu, params: {interval: 30, threshold: 80, smoothing: ema}}
`)
	for i := 0; i < 5; i++ {
		data, err := LoadForShow(configPath, ShowResolved, loaderTestLogger(t))
		if err != nil {
			t.Fatalf("iter %d LoadForShow: %v", i, err)
		}
		out, err := MarshalSortedYAML(&data)
		if err != nil {
			t.Fatalf("iter %d MarshalSortedYAML: %v", i, err)
		}
		if i == 0 {
			os.Setenv("__show_first", string(out))
			continue
		}
		if got := os.Getenv("__show_first"); got != string(out) {
			t.Fatalf("iter %d output differs from iter 0:\ndiff first 200 bytes:\n--first--\n%.200s\n--current--\n%.200s", i, got, string(out))
		}
	}
}
