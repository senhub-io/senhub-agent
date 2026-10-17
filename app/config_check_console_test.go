package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"senhub-agent.go/internal/agent/services/configuration"
)

func consoleStorage(block string) string {
	return "storage:\n  - name: http\n    params:\n      port: 18080\n" + block
}

func TestConfigCheckReportsTheConsoleMode(t *testing.T) {
	cases := []struct {
		name       string
		block      string
		wantMode   string
		wantErrors int
	}{
		{"default", "", "Console mode: full", 0},
		{"read only", "      console:\n        read_only: true\n", "Console mode: read-only", 0},
		{"no exec", "      console:\n        allow_exec: false\n", "Console mode: no-exec", 0},
		{"misspelt", "      console:\n        readonly: true\n", "", 1},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "agent-config.yaml")
			if err := os.WriteFile(path, []byte(parityConfig(consoleStorage(tc.block), parityProbes)), 0o600); err != nil {
				t.Fatal(err)
			}
			var outcome checkOutcome
			out := captureStdout(t, func() { outcome = checkConfig(path) })
			if tc.wantMode != "" && !strings.Contains(out, tc.wantMode) {
				t.Errorf("no %q line:\n%s", tc.wantMode, out)
			}
			if outcome.errors != tc.wantErrors {
				t.Errorf("%d error(s), want %d:\n%s", outcome.errors, tc.wantErrors, out)
			}
		})
	}
}

func TestDoctorStatesTheConsoleMode(t *testing.T) {
	withBlock := func(block map[string]interface{}) doctorDeps {
		d := healthyDoctorDeps()
		d.loadConfig = func(string) (configuration.LocalConfigurationData, error) {
			return configuration.LocalConfigurationData{Storage: []configuration.StorageConfig{
				{Name: "http", Params: map[string]interface{}{"port": 8080, "console": block}},
			}}, nil
		}
		return d
	}
	c := expectLevel(t, runDoctorChecks(withBlock(map[string]interface{}{"read_only": true})), "config.console", levelOK)
	if !strings.Contains(c.Message, "read-only") {
		t.Errorf("message %q does not name the mode", c.Message)
	}
	c = expectLevel(t, runDoctorChecks(withBlock(map[string]interface{}{"allow_exec": false})), "config.console", levelOK)
	if !strings.Contains(c.Message, "no-exec") {
		t.Errorf("message %q does not name the mode", c.Message)
	}
	expectLevel(t, runDoctorChecks(withBlock(map[string]interface{}{"read_only": "yes"})), "config.console", levelFail)
}
