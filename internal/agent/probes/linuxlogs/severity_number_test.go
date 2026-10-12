package linuxlogs

import (
	"testing"

	"senhub-agent.go/internal/agent/services/agentstate"
)

// Every journald PRIORITY gets a number, and an entry with none is
// informational rather than the emergency that Atoi's zero would mean (#604).
func TestParseEntry_AlwaysSetsASeverityNumber(t *testing.T) {
	for _, p := range []string{"0", "1", "2", "3", "4", "5", "6", "7"} {
		rec := parseEntry(journalEntry{Priority: p, Message: "m"}, "j")
		if rec.Severity == agentstate.LogSeverityUnspecified || rec.SeverityText == "" {
			t.Errorf("priority %s: severity %d %q", p, rec.Severity, rec.SeverityText)
		}
	}
	for _, p := range []string{"", "abc"} {
		rec := parseEntry(journalEntry{Priority: p, Message: "m"}, "j")
		if rec.Severity != agentstate.LogSeverityInfo || rec.SeverityText != "INFO" {
			t.Errorf("priority %q: severity %d %q, want INFO", p, rec.Severity, rec.SeverityText)
		}
	}
}
