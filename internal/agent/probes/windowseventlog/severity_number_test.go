package windowseventlog

import (
	"testing"

	"senhub-agent.go/internal/agent/services/agentstate"
)

// Every Windows level, including 0 (LogAlways) and one the OS never
// documents, leaves with an OTel number and the level's own text (#604).
func TestToLogRecord_AlwaysSetsASeverityNumber(t *testing.T) {
	texts := map[int]string{1: "Critical", 2: "Error", 3: "Warning", 4: "Information", 5: "Verbose", 0: "Information", 9: "Information"}
	for level, text := range texts {
		rec := parsedEvent{Level: level, EventID: 1, Message: "m"}.toLogRecord("w", false)
		if rec.Severity == agentstate.LogSeverityUnspecified {
			t.Errorf("level %d: no severity number", level)
		}
		if rec.SeverityText != text {
			t.Errorf("level %d: text %q, want %q", level, rec.SeverityText, text)
		}
	}
}
