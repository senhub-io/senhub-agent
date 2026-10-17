package app

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"senhub-agent.go/internal/agent/services/configuration"
)

const checkGoldenPath = "testdata/check_config.golden"

func checkGoldenCases() map[string]string {
	header := fmt.Sprintf("config_version: %d\nagent:\n  key: %s\n", configuration.CurrentConfigVersion, parityKey)
	return map[string]string{
		"1-kitchen-sink": header + `  license: not-a-license
probes:
  - name: cpu
    type: cpu
  - name: cpu
    type: cpu
  - type: cpu
  - name: notype
  - name: bogus
    type: nonesuch_type
  - name: badlog
    type: cpu
    log_strategies: [http]
  - name: off
    type: memory
    enabled: false
  - name: routed
    type: memory
    log_strategies: [otlp, event]
  - name: tail-a
    type: filetail
    params:
      paths: [/var/log/golden.log]
  - name: tail-b
    type: filetail
    params:
      paths: [/var/log/golden.log]
storage:
  - name: http
    params:
      port: 18080
      bogus_param: 1
  - name: nonesuch
  - name: otlp
    params:
      endpoint: ${env:GOLDEN_UNSET_ENDPOINT}
      staleness_ttl: 0
auto_update:
  enabled: true
  url: https://example.invalid/releases/latest
`,
		"2-valid": header + `probes:
  - name: cpu
    type: cpu
storage:
  - name: http
    params:
      port: 18080
`,
		"3-nothing": header + "storage: []\n",
		"4-bad-version": strings.Replace(header, fmt.Sprintf("config_version: %d", configuration.CurrentConfigVersion), "config_version: 9999", 1) + `probes: []
storage:
  - name: http
  - params:
      port: 1
auto_update:
  url: ftp://example.invalid
`,
		"5-yaml-error": header + "probes: [\n",
	}
}

func TestCheckConfigGoldenOutput(t *testing.T) {
	names := []string{"1-kitchen-sink", "2-valid", "3-nothing", "4-bad-version", "5-yaml-error"}
	cases := checkGoldenCases()
	warn := checkAutoUpdateWritability()

	var sb strings.Builder
	for _, name := range names {
		dir := t.TempDir()
		path := filepath.Join(dir, "agent-config.yaml")
		if err := os.WriteFile(path, []byte(cases[name]), 0o600); err != nil {
			t.Fatal(err)
		}
		var out checkOutcome
		text := captureStdout(t, func() { out = checkConfig(path) })
		warnings := out.warnings
		if warn != "" {
			text = strings.Replace(text, "  [WARN] "+warn+"\n", "", 1)
			warnings--
		}
		text = strings.ReplaceAll(text, dir, "<dir>")
		text = strings.ReplaceAll(text, "/private<dir>", "<dir>")
		loadErr := "<nil>"
		if out.loadErr != nil {
			loadErr = strings.ReplaceAll(out.loadErr.Error(), dir, "<dir>")
			loadErr = strings.ReplaceAll(loadErr, "/private<dir>", "<dir>")
		}
		fmt.Fprintf(&sb, "===== %s =====\n%s-- errors=%d warnings=%d loadErr=%s\n", name, text, out.errors, warnings, loadErr)
	}
	got := sb.String()

	if os.Getenv("UPDATE_GOLDEN") != "" {
		if err := os.MkdirAll(filepath.Dir(checkGoldenPath), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(checkGoldenPath, []byte(got), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(checkGoldenPath)
	if err != nil {
		t.Fatalf("reading golden: %v", err)
	}
	if got != string(want) {
		t.Errorf("checkConfig output changed.\n--- got ---\n%s\n--- want ---\n%s", got, want)
	}
}
