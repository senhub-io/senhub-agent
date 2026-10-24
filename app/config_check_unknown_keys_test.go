package app

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"senhub-agent.go/internal/agent/services/configuration"
)

func writeLayoutFiles(t *testing.T, files map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	for name, body := range files {
		p := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

// An unknown key outside params, on a probe entry or in a storage block,
// is dropped by the loader and the agent runs without it. Like an unread
// key inside params it is a warning (exit 1), never an error and never
// silence.
func TestConfigCheckWarnsOnUnknownKeysOutsideParams(t *testing.T) {
	cases := []struct {
		name    string
		storage string // params of the http strategy, indented 6 spaces
		probe   string // extra probe-level lines, indented 4 spaces
		want    string // empty: no finding expected
	}{
		{"valid", "      port: 18080\n      prtg:\n        check_result_channel: true\n", "    custom_tags: {a: b}\n", ""},
		{"probe level typo", "      port: 18080\n", "    intervall: 30\n", `Probe "cpu": key "intervall" is not read at the probe level`},
		{"http level", "      port: 18080\n      bogus: 1\n", "", `param "bogus" is not read by the http strategy`},
		{"prtg sub-block", "      port: 18080\n      prtg:\n        bogus: 1\n", "", `param "prtg.bogus" is not read by the http strategy`},
		{"prometheus sub-block", "      port: 18080\n      prometheus:\n        bogus: 1\n", "", `param "prometheus.bogus" is not read by the http strategy`},
		{"tls sub-block", "      port: 18080\n      tls:\n        bogus: 1\n", "", `param "tls.bogus" is not read by the http strategy`},
	}
	for _, c := range cases {
		storage := "storage:\n  - name: http\n    params:\n" + c.storage
		probes := "probes:\n  - name: cpu\n    type: cpu\n" + c.probe
		legacy := writeLayoutFiles(t, map[string]string{"agent-config.yaml": parityConfig(storage, probes)})

		var strat strings.Builder
		for _, l := range strings.Split(strings.TrimRight(c.storage, "\n"), "\n") {
			strat.WriteString(strings.TrimPrefix(l, "    ") + "\n")
		}
		multi := writeLayoutFiles(t, map[string]string{
			"agent.yaml":                fmt.Sprintf("config_version: %d\nagent:\n  key: %s\n", configuration.CurrentConfigVersion, parityKey),
			"strategies.d/00-http.yaml": "http:\n" + strat.String(),
			"probes.d/00-cpu.yaml":      "- name: cpu\n  type: cpu\n" + strings.ReplaceAll(strings.ReplaceAll(c.probe, "\n    ", "\n  "), "    ", "  "),
		})

		for layout, path := range map[string]string{
			"legacy":     filepath.Join(legacy, "agent-config.yaml"),
			"multi-file": filepath.Join(multi, "agent.yaml"),
		} {
			var outcome checkOutcome
			out := captureStdout(t, func() { outcome = checkConfig(path) })
			if outcome.errors != 0 || outcome.loadErr != nil {
				t.Errorf("%s/%s: want no error, got %d (%v):\n%s", c.name, layout, outcome.errors, outcome.loadErr, out)
			}
			if c.want == "" {
				if strings.Contains(out, "is not read") || outcome.exitCode() != 0 {
					t.Errorf("%s/%s: valid configuration reported a finding (exit %d):\n%s", c.name, layout, outcome.exitCode(), out)
				}
				continue
			}
			if !strings.Contains(out, "[WARN]") || !strings.Contains(out, c.want) {
				t.Errorf("%s/%s: missing warning %q:\n%s", c.name, layout, c.want, out)
			}
			if outcome.exitCode() != 1 {
				t.Errorf("%s/%s: exit %d, want 1:\n%s", c.name, layout, outcome.exitCode(), out)
			}
		}
	}
}
