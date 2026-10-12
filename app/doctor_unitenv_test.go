package app

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"senhub-agent.go/internal/agent/services/configuration"
	"senhub-agent.go/internal/agent/services/data_store/strategies/otlp"
)

func writeTestFile(t *testing.T, path, content string, mode os.FileMode) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), mode); err != nil {
		t.Fatal(err)
	}
}

func TestReadServiceEnvironmentUnitAndDropIns(t *testing.T) {
	dir := t.TempDir()
	unit := filepath.Join(dir, "senhub-agent.service")
	envFile := filepath.Join(dir, "token.env")
	dropEnvFile := filepath.Join(dir, "drop.env")

	writeTestFile(t, unit, "[Service]\nEnvironment=PLAIN=one \"QUOTED=two words\"\nEnvironmentFile=-"+filepath.Join(dir, "missing.env")+"\nEnvironmentFile="+envFile+"\n", 0o644)
	writeTestFile(t, envFile, "# comment\nOTLP_TOKEN=\"secret-from-file\"\nPLAIN=overridden\n", 0o600)
	writeTestFile(t, unit+".d/10-extra.conf", "[Service]\nEnvironment=FROM_DROPIN=yes\nEnvironmentFile="+dropEnvFile+"\n", 0o644)
	writeTestFile(t, dropEnvFile, "DROP_FILE=ok\n", 0o600)

	env, unreadable, err := readServiceEnvironment(unit)
	if err != nil {
		t.Fatal(err)
	}
	if len(unreadable) != 0 {
		t.Errorf("unreadable %v, want none (the '-' file is optional)", unreadable)
	}
	want := map[string]string{
		"OTLP_TOKEN":  "secret-from-file",
		"PLAIN":       "overridden",
		"QUOTED":      "two words",
		"FROM_DROPIN": "yes",
		"DROP_FILE":   "ok",
	}
	for k, v := range want {
		if env[k] != v {
			t.Errorf("%s = %q, want %q", k, env[k], v)
		}
	}
}

func TestReadServiceEnvironmentReportsUnreadableFile(t *testing.T) {
	dir := t.TempDir()
	unit := filepath.Join(dir, "senhub-agent.service")
	writeTestFile(t, unit, "[Service]\nEnvironmentFile="+filepath.Join(dir, "absent.env")+"\n", 0o644)
	_, unreadable, err := readServiceEnvironment(unit)
	if err != nil {
		t.Fatal(err)
	}
	if len(unreadable) != 1 {
		t.Errorf("unreadable %v, want the mandatory missing file", unreadable)
	}
}

func tokenFailureDeps(cfgPath string) doctorDeps {
	d := healthyDoctorDeps()
	d.configPath = cfgPath
	d.loadConfig = func(string) (configuration.LocalConfigurationData, error) {
		return configuration.LocalConfigurationData{
			Storage: []configuration.StorageConfig{{Name: "otlp", Params: map[string]interface{}{"endpoint": "collector:4317"}}},
		}, nil
	}
	d.configCheck = func(string) (string, checkOutcome, error) { return "  [OK]   config_version: 3\n", checkOutcome{}, nil }
	d.unsetEnv = configuration.UnsetEnvReferences
	d.outputTest = func(context.Context, string, map[string]interface{}, string, int) []otlp.ConnectionStep {
		if os.Getenv("DOCTOR_TEST_OTLP_TOKEN") == "" {
			return []otlp.ConnectionStep{{Name: "config", Passed: false,
				Error: `Authorization header has scheme "Bearer" but no credential — the token resolved to empty`}}
		}
		return []otlp.ConnectionStep{{Name: "dns", Passed: true}}
	}
	return d
}

func TestDoctorResolvesTheServiceEnvironment(t *testing.T) {
	dir := t.TempDir()
	cfg := filepath.Join(dir, "agent.yaml")
	writeTestFile(t, cfg, "strategies:\n  otlp:\n    headers:\n      Authorization: Bearer ${env:DOCTOR_TEST_OTLP_TOKEN}\n", 0o644)
	unit := filepath.Join(dir, "senhub-agent.service")
	writeTestFile(t, unit, "[Service]\nEnvironmentFile=/nonexistent-never-read.env\n", 0o644)
	writeTestFile(t, unit+".d/token.conf", "[Service]\nEnvironmentFile="+filepath.Join(dir, "token.env")+"\n", 0o644)
	writeTestFile(t, filepath.Join(dir, "token.env"), "DOCTOR_TEST_OTLP_TOKEN=abc123\n", 0o600)

	t.Run("readable unit and env file resolve the token", func(t *testing.T) {
		t.Setenv("DOCTOR_TEST_OTLP_TOKEN", "")
		os.Unsetenv("DOCTOR_TEST_OTLP_TOKEN")
		d := tokenFailureDeps(cfg)
		writeTestFile(t, unit, "[Service]\n", 0o644)
		d.serviceEnv = func() serviceEnvReport { return applyServiceEnvironment(unit) }
		t.Cleanup(func() { os.Unsetenv("DOCTOR_TEST_OTLP_TOKEN") })
		expectLevel(t, runDoctorChecks(d), "outputs.otlp", levelOK)
	})

	t.Run("unreadable env file is a warning that names the variable", func(t *testing.T) {
		os.Unsetenv("DOCTOR_TEST_OTLP_TOKEN")
		d := tokenFailureDeps(cfg)
		d.serviceEnv = func() serviceEnvReport { return serviceEnvReport{Unreadable: []string{"token.env"}} }
		c := expectLevel(t, runDoctorChecks(d), "outputs.otlp", levelWarn)
		if !strings.Contains(c.Message, "not resolvable outside the service context") || !strings.Contains(c.Message, "DOCTOR_TEST_OTLP_TOKEN") {
			t.Errorf("message %q must name the context and the variable", c.Message)
		}
	})

	t.Run("a failure with no env reference stays a failure", func(t *testing.T) {
		d := tokenFailureDeps(cfg)
		d.unsetEnv = func(string) []configuration.EnvReference { return nil }
		d.outputTest = func(context.Context, string, map[string]interface{}, string, int) []otlp.ConnectionStep {
			return []otlp.ConnectionStep{{Name: "tcp", Passed: false, Error: "connection refused"}}
		}
		expectLevel(t, runDoctorChecks(d), "outputs.otlp", levelFail)
	})
}
