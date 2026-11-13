package app

import (
	"runtime"
	"testing"
)

func TestReportMultilineProblemsFlagsTheFilebeatSpelling(t *testing.T) {
	block := func(negate bool) map[string]interface{} {
		return map[string]interface{}{"multiline": map[string]interface{}{
			"pattern": `^\d{4}-\d{2}-\d{2} `,
			"negate":  negate,
			"match":   "after",
		}}
	}
	if got := reportMultilineProblems("prtg-logs", block(true)); got != 1 {
		t.Fatalf("negate: true on a leading timestamp: %d warnings, want 1", got)
	}
	if got := reportMultilineProblems("prtg-logs", block(false)); got != 0 {
		t.Fatalf("negate: false: %d warnings, want 0", got)
	}
	if got := reportMultilineProblems("cpu", map[string]interface{}{}); got != 0 {
		t.Fatalf("no multiline block: %d warnings, want 0", got)
	}
}

func TestReportFiletailProtectedHomeNamesTheFix(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("ProtectHome is a systemd setting")
	}
	under := map[string]interface{}{"paths": []interface{}{"/var/log/app.log", "/home/app/logs/*.log"}}
	if got := reportFiletailProtectedHome("app-logs", under); got != 1 {
		t.Fatalf("one path under /home: %d warnings, want 1", got)
	}
	clear := map[string]interface{}{"paths": []interface{}{"/var/log/app.log"}}
	if got := reportFiletailProtectedHome("app-logs", clear); got != 0 {
		t.Fatalf("no path under /home: %d warnings, want 0", got)
	}
}
