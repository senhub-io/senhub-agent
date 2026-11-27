package app

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestConfigCheckWarnsWhenTheSplitIsBlocked(t *testing.T) {
	agentYAML := parityConfig("storage:\n  - name: http\n    params:\n      port: 18080\n", "probes:\n  - name: cpu\n    type: cpu\n")

	t.Run("operator fragment beside monolithic file", func(t *testing.T) {
		dir := writeLayoutFiles(t, map[string]string{
			"agent.yaml":            agentYAML,
			"probes.d/10-mine.yaml": "- name: mine\n  type: cpu\n",
		})
		path := filepath.Join(dir, "agent.yaml")
		var outcome checkOutcome
		out := captureStdout(t, func() { outcome = checkConfig(path) })
		found := false
		for _, line := range strings.Split(out, "\n") {
			if strings.Contains(line, "[WARN]") && strings.Contains(line, "10-mine.yaml") {
				found = true
			}
		}
		if !found {
			t.Fatalf("want a [WARN] line mentioning 10-mine.yaml, got:\n%s", out)
		}
		if outcome.warnings < 1 {
			t.Fatalf("want at least 1 warning, got %d", outcome.warnings)
		}
		if outcome.errors != 0 {
			t.Fatalf("want 0 errors, got %d", outcome.errors)
		}
	})

	t.Run("monolithic file with no fragment", func(t *testing.T) {
		dir := writeLayoutFiles(t, map[string]string{
			"agent.yaml": agentYAML,
		})
		path := filepath.Join(dir, "agent.yaml")
		out := captureStdout(t, func() { _ = checkConfig(path) })
		if !strings.Contains(out, "[INFO]") || !strings.Contains(out, "single-file layout") {
			t.Fatalf("want an [INFO] line about the single-file layout, got:\n%s", out)
		}
		for _, line := range strings.Split(out, "\n") {
			if strings.Contains(line, "[WARN]") && strings.Contains(line, "split") {
				t.Fatalf("unexpected [WARN] split line: %s", line)
			}
		}
	})
}
