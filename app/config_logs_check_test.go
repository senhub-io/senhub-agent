package app

import (
	"strings"
	"testing"

	"senhub-agent.go/internal/agent/services/configuration"
)

func TestReportLogsProblemsNamesTheBadRule(t *testing.T) {
	cfg := &configuration.LogsConfig{Redact: &configuration.LogsRedactConfig{Rules: []configuration.LogsRedactRuleSpec{
		{Name: "good", Pattern: `\d+`, Replacement: "#"},
		{Name: "broken", Pattern: "(", Replacement: "x"},
	}}}
	var errs int
	out := captureStdout(t, func() { errs = reportLogsProblems(cfg) })
	if errs != 1 || !strings.Contains(out, `"broken"`) || strings.Contains(out, `"good"`) {
		t.Fatalf("errors=%d output=%q, want one error naming broken only", errs, out)
	}
	if n := reportLogsProblems(nil); n != 0 {
		t.Fatalf("absent block reported %d errors", n)
	}
}
