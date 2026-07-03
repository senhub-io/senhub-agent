package configuration

import (
	"bytes"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rs/zerolog"

	"senhub-agent.go/internal/agent/services/logger"
)

func TestSanitizeParamsForLog_MasksSecretKeys(t *testing.T) {
	in := map[string]interface{}{
		"host":             "example.test",
		"port":             22,
		"user":             "admin",
		"password":         "hunter2",
		"api_token":        "tok-abc",
		"client_secret":    "shhh",
		"db_password":      "p4ss",
		"auth_login":       "svc-acct",
		"contact_email":    "ops@example.test",
		"trusted_user_ids": []int{1, 2},
		"interval":         30,
		"runner_dir":       "/opt/runner",
	}
	out := SanitizeParamsForLog(in)
	for _, k := range []string{"user", "password", "api_token", "client_secret", "db_password", "auth_login", "contact_email", "trusted_user_ids"} {
		if out[k] != "***" {
			t.Errorf("key %q should have been masked; got %v", k, out[k])
		}
	}
	for _, k := range []string{"host", "port", "interval", "runner_dir"} {
		if out[k] == "***" {
			t.Errorf("key %q should NOT have been masked", k)
		}
	}
}

func TestSanitizeParamsForLog_NilIsNil(t *testing.T) {
	if got := SanitizeParamsForLog(nil); got != nil {
		t.Errorf("nil input should return nil, got %v", got)
	}
}

func TestSanitizeParamsForLog_DoesNotMutateInput(t *testing.T) {
	in := map[string]interface{}{
		"user":     "admin",
		"password": "hunter2",
	}
	_ = SanitizeParamsForLog(in)
	if in["user"] != "admin" {
		t.Errorf("input was mutated: user = %v", in["user"])
	}
	if in["password"] != "hunter2" {
		t.Errorf("input was mutated: password = %v", in["password"])
	}
}

func TestSanitizeParamsForLog_CaseInsensitiveMatch(t *testing.T) {
	in := map[string]interface{}{
		"USER":        "ALICE",
		"Password":    "shh",
		"AuthLogin":   "svc",
		"X-API-TOKEN": "tok-abc",
	}
	out := SanitizeParamsForLog(in)
	for k := range in {
		if out[k] != "***" {
			t.Errorf("case variation %q not masked: got %v", k, out[k])
		}
	}
}

// TestSanitizeStorageAndProbesForLog verifies the log-safe list helpers mask
// credential-bearing params while preserving the surrounding shape, and never
// mutate the caller's runtime config.
func TestSanitizeStorageAndProbesForLog(t *testing.T) {
	storage := []StorageConfig{{
		Name: "otlp",
		Params: map[string]interface{}{
			"endpoint":     "collector:4317",
			"bearer_token": "tok-should-not-leak",
		},
	}}
	probes := []ProbeConfig{{
		Name: "db",
		Type: "mysql",
		Params: map[string]interface{}{
			"host":     "localhost",
			"password": "pw-should-not-leak",
		},
	}}

	gotStorage := SanitizeStorageForLog(storage)
	sp := gotStorage[0]["params"].(map[string]interface{})
	if sp["bearer_token"] != "***" {
		t.Errorf("storage bearer_token not masked: %v", sp["bearer_token"])
	}
	if sp["endpoint"] != "collector:4317" {
		t.Errorf("storage endpoint should be preserved: %v", sp["endpoint"])
	}

	gotProbes := SanitizeProbesForLog(probes)
	pp := gotProbes[0]["params"].(map[string]interface{})
	if pp["password"] != "***" {
		t.Errorf("probe password not masked: %v", pp["password"])
	}
	if pp["host"] != "localhost" {
		t.Errorf("probe host should be preserved: %v", pp["host"])
	}

	if storage[0].Params["bearer_token"] != "tok-should-not-leak" {
		t.Errorf("original storage params were mutated")
	}
	if probes[0].Params["password"] != "pw-should-not-leak" {
		t.Errorf("original probe params were mutated")
	}
}

// TestReloadConfiguration_RedactsSecretsInLog is the watcher call-site
// regression: when the config file changes, the "Configuration changes
// detected" log line must NOT echo resolved probe/storage credentials in
// cleartext into shared log infrastructure.
func TestReloadConfiguration_RedactsSecretsInLog(t *testing.T) {
	const probeSecret = "s3cr3t-db-pass"
	const storageSecret = "s3cr3t-bearer"

	dir := t.TempDir()
	configPath := filepath.Join(dir, "agent-config.yaml")
	writeFile(t, configPath, `
config_version: 2
agent:
  key: "k1"
probes:
  - name: db
    type: mysql
    params:
      host: localhost
      password: `+probeSecret+`
storage:
  - name: otlp
    params:
      endpoint: collector:4317
      bearer_token: `+storageSecret+`
`)

	var buf bytes.Buffer
	zl := zerolog.New(&buf)
	base := (*logger.Logger)(&zl)
	ml := logger.NewModuleLogger(base, "configuration.local.test")

	lc := &LocalConfiguration{
		logger:        ml,
		configPath:    configPath,
		eventNotifier: NewEventNotifier(base),
	}
	// Seed an EMPTY previous snapshot so hasConfigurationChanged() fires and
	// the change-detected log line is emitted.
	lc.storeData(LocalConfigurationData{})

	if err := lc.reloadConfiguration(); err != nil {
		t.Fatalf("reloadConfiguration: %v", err)
	}

	out := buf.String()
	if strings.Contains(out, probeSecret) {
		t.Errorf("reload log leaked plaintext probe password:\n%s", out)
	}
	if strings.Contains(out, storageSecret) {
		t.Errorf("reload log leaked plaintext storage bearer_token:\n%s", out)
	}
	if !strings.Contains(out, "***") {
		t.Errorf("reload log should contain masked marker ***; got:\n%s", out)
	}
}
