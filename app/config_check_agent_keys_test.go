package app

import (
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"senhub-agent.go/internal/agent/services/configuration"
)

func TestCheckConfig_WarnsOnUnknownAgentKeys(t *testing.T) {
	cases := []struct {
		name        string
		extra       string
		wantWarning string
	}{
		{"update_check_interval", "  update_check_interval: 60\n", `key "agent.update_check_interval" is not read`},
		{"licence typo", "  licence: abc\n", `key "agent.licence" is not read`},
		{"valid", "  global_tags:\n    site: paris\n", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			path := writeLayoutFiles(t, map[string]string{
				"agent.yaml":                fmt.Sprintf("config_version: %d\nagent:\n  key: %s\n", configuration.CurrentConfigVersion, parityKey) + tc.extra,
				"strategies.d/00-http.yaml": "http:\n  port: 18080\n",
				"probes.d/00-cpu.yaml":      "- name: cpu\n  type: cpu\n",
			})
			var outcome checkOutcome
			out := captureStdout(t, func() {
				outcome = checkConfig(filepath.Join(path, "agent.yaml"))
			})
			if tc.wantWarning == "" {
				if n := configWarnings(out); n != 0 {
					t.Errorf("configWarnings = %d, want 0; output:\n%s", n, out)
				}
				return
			}
			if !strings.Contains(out, tc.wantWarning) {
				t.Errorf("output missing %q; output:\n%s", tc.wantWarning, out)
			}
			if outcome.exitCode() != 1 {
				t.Errorf("exitCode = %d, want 1", outcome.exitCode())
			}
			if outcome.errors != 0 {
				t.Errorf("errors = %d, want 0", outcome.errors)
			}
		})
	}
}
