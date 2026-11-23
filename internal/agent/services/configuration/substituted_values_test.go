package configuration

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rs/zerolog"

	"senhub-agent.go/internal/agent/services/configuration/secret"
	"senhub-agent.go/internal/agent/services/extension"
	"senhub-agent.go/internal/agent/services/logger"
)

func resetSubstituted(t *testing.T) {
	t.Helper()
	substituted.rotate(nil)
	substituted.rotate(nil)
	t.Cleanup(func() {
		substituted.rotate(nil)
		substituted.rotate(nil)
	})
}

func useMemorySecrets(t *testing.T, kv map[string]string) {
	t.Helper()
	mp := secret.NewMemoryProvider()
	for k, v := range kv {
		if err := mp.Set(k, secret.New(v)); err != nil {
			t.Fatalf("seeding secret %s: %v", k, err)
		}
	}
	secret.SetProvider(mp)
	t.Cleanup(func() { secret.SetProvider(nil) })
}

func reloadLog(t *testing.T, yamlText string) string {
	t.Helper()
	configPath := filepath.Join(t.TempDir(), "agent-config.yaml")
	writeFile(t, configPath, yamlText)
	var buf bytes.Buffer
	zl := zerolog.New(&buf)
	base := (*logger.Logger)(&zl)
	lc := &LocalConfiguration{
		logger:        logger.NewModuleLogger(base, "configuration.local.test"),
		configPath:    configPath,
		eventNotifier: NewEventNotifier(base),
	}
	lc.storeData(LocalConfigurationData{})
	if err := lc.reloadConfiguration(); err != nil {
		t.Fatalf("reloadConfiguration: %v", err)
	}
	return buf.String()
}

func TestSubstitutedValuesNeverReachTheLogs(t *testing.T) {
	const plain = "plain-visible-host.example"
	const keyed = "keyed-password-value"

	cases := []struct {
		name   string
		secret string
		ref    func(t *testing.T) string
	}{
		{"secret", "opamp-token-0123", func(t *testing.T) string {
			useMemorySecrets(t, map[string]string{"opamp.token": "opamp-token-0123"})
			return "${secret:opamp.token}"
		}},
		{"env", "env-value-4567", func(t *testing.T) string {
			t.Setenv("LOGSUBST_VAL", "env-value-4567")
			return "${env:LOGSUBST_VAL}"
		}},
		{"file", "file-value-89ab", func(t *testing.T) string {
			p := filepath.Join(t.TempDir(), "v.txt")
			if err := os.WriteFile(p, []byte("file-value-89ab\n"), 0o600); err != nil {
				t.Fatal(err)
			}
			return "${file:" + p + "}"
		}},
		{"env with unused default", "env-dflt-cdef", func(t *testing.T) string {
			t.Setenv("LOGSUBST_VAL2", "env-dflt-cdef")
			return "${env:LOGSUBST_VAL2:-fallback}"
		}},
	}
	shapes := []struct {
		name string
		yaml func(ref string) string
	}{
		{"neutral key", func(ref string) string { return "      x_ref: \"" + ref + "\"\n" }},
		{"nested map", func(ref string) string {
			return "      outer:\n        inner:\n          x_ref: \"" + ref + "\"\n"
		}},
		{"nested list", func(ref string) string {
			return "      items:\n        - label: \"" + ref + "\"\n"
		}},
		{"inside a URL", func(ref string) string {
			return "      x_ref: \"https://svc.example/api/" + ref + "/data\"\n"
		}},
	}

	for _, c := range cases {
		for _, sh := range shapes {
			t.Run(c.name+"/"+sh.name, func(t *testing.T) {
				resetSubstituted(t)
				ref := c.ref(t)
				body := sh.yaml(ref)
				out := reloadLog(t, "config_version: 2\nagent:\n  key: \"k1\"\nprobes:\n  - name: p1\n    type: http\n    params:\n      host: "+plain+"\n      password: "+keyed+"\n"+body+
					"storage:\n  - name: otlp\n    params:\n      endpoint: collector:4317\n"+body)
				if !strings.Contains(out, "Configuration changes detected") {
					t.Fatalf("the change-detected line was not logged:\n%s", out)
				}
				if strings.Contains(out, c.secret) {
					t.Errorf("a substituted value reached the log:\n%s", out)
				}
				if !strings.Contains(out, plain) {
					t.Errorf("a plain value must stay visible:\n%s", out)
				}
				if strings.Contains(out, keyed) {
					t.Errorf("a recognised key must stay masked:\n%s", out)
				}
			})
		}
	}
}

func TestSanitizeProbeParamsForLogAfterLoad(t *testing.T) {
	resetSubstituted(t)
	useMemorySecrets(t, map[string]string{"opamp.token": "opamp-token-0123"})
	t.Setenv("LOGSUBST_PORT", "9443")
	t.Setenv("LOGSUBST_DEFAULTED_UNSET", "")
	os.Unsetenv("LOGSUBST_DEFAULTED_UNSET")

	configPath := filepath.Join(t.TempDir(), "agent-config.yaml")
	writeFile(t, configPath, `config_version: 2
agent:
  key: "k1"
probes:
  - name: p1
    type: http
    params:
      x_ref: "${secret:opamp.token}"
      host: "plain.example"
      region: "${env:LOGSUBST_DEFAULTED_UNSET:-eu-west-1}"
      mixed: "Bearer ${secret:opamp.token} on ${env:LOGSUBST_PORT}"
`)
	data, err := LoadFromDisk(configPath, nil)
	if err != nil {
		t.Fatal(err)
	}
	out := SanitizeProbesForLog(data.Probes)
	params := out[0]["params"].(map[string]interface{})
	if params["x_ref"] != "***" {
		t.Errorf("x_ref = %v, want ***", params["x_ref"])
	}
	if params["host"] != "plain.example" {
		t.Errorf("host = %v, want it visible", params["host"])
	}
	if params["region"] != "eu-west-1" {
		t.Errorf("a ${env:X:-default} default is not a secret and stays visible; got %v", params["region"])
	}
	if got, _ := params["mixed"].(string); strings.Contains(got, "opamp-token-0123") || strings.Contains(got, "9443") {
		t.Errorf("mixed = %q, want both substituted values masked", got)
	}
}

func TestSubstitutedValuesTwoGenerationsOnly(t *testing.T) {
	resetSubstituted(t)
	rec := func(v string) map[string]struct{} { return map[string]struct{}{v: {}} }

	substituted.rotate(rec("first-secret-1"))
	substituted.rotate(rec("second-secret-2"))
	if got := maskSubstituted("a first-secret-1 b second-secret-2"); strings.Contains(got, "secret-") {
		t.Errorf("the previous generation must stay masked for the old/new log pair: %q", got)
	}
	substituted.rotate(rec("third-secret-3"))
	if got := maskSubstituted("first-secret-1"); got != "first-secret-1" {
		t.Errorf("a value two loads old is dropped, got %q", got)
	}
	if n := len(substituted.ordered); n != 2 {
		t.Errorf("tracked values = %d, want 2 (bounded by two generations)", n)
	}
}

func TestSubstitutedValuesShortValuesNotMasked(t *testing.T) {
	resetSubstituted(t)
	rec := &recorder{}
	rec.add("on")
	if len(rec.values) != 0 {
		t.Error("a value under the minimum length must not be recorded")
	}
	substituted.rotate(rec.values)
	if got := maskSubstituted("config on"); got != "config on" {
		t.Errorf("got %q", got)
	}
}

func TestRecordSubstitutedMasksExternallyResolvedValues(t *testing.T) {
	resetSubstituted(t)
	RecordSubstituted("externally-resolved-9")
	out := SanitizeParamsForLog(map[string]interface{}{"x_ref": "pre externally-resolved-9 post"})
	if got := out["x_ref"]; got != "pre *** post" {
		t.Errorf("x_ref = %v", got)
	}
}

func TestSensorStartLogUsesTheSanitizer(t *testing.T) {
	src, err := os.ReadFile(filepath.Join("..", "sensor", "sensor.go"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(src), `Any("probe_params", configuration.SanitizeProbeParamsForLog(`) {
		t.Error(`the "Starting new probe" line must log SanitizeProbeParamsForLog output`)
	}
}

func formatParams(m map[string]interface{}) string {
	var b bytes.Buffer
	zl := zerolog.New(&b)
	zl.Info().Any("p", m).Msg("x")
	return b.String()
}
