package logparse

import (
	"testing"
	"time"

	"senhub-agent.go/internal/agent/services/agentstate"
)

// filetail records carry an OTel severity number whatever spelling the
// application used for its level, and keep that spelling as the text (#604).
func TestParseLine_LevelSpellingsAlwaysGetANumber(t *testing.T) {
	cases := []struct {
		name string
		pc   ParserConfig
		line string
		want agentstate.LogSeverity
		text string
	}{
		{"json canonical", ParserConfig{Type: ParserJSON}, `{"level":"error","msg":"x"}`, agentstate.LogSeverityError, "ERROR"},
		{"json verbose", ParserConfig{Type: ParserJSON}, `{"level":"verbose","msg":"x"}`, agentstate.LogSeverityDebug, "verbose"},
		{"json informational", ParserConfig{Type: ParserJSON}, `{"level":"Informational","msg":"x"}`, agentstate.LogSeverityInfo, "Informational"},
		{"json numeric pino", ParserConfig{Type: ParserJSON}, `{"level":50,"msg":"x"}`, agentstate.LogSeverityError, "50"},
		{"logfmt severe", ParserConfig{Type: ParserLogfmt}, `level=severe msg=x`, agentstate.LogSeverityError, "severe"},
		{"raw sniffed", ParserConfig{Type: ParserRaw}, `WARN something happened`, agentstate.LogSeverityWarn, "WARN"},
	}
	for _, c := range cases {
		rec, ok := ParseLine(c.pc, c.line, time.Now(), "ft", "filetail")
		if !ok {
			t.Fatalf("%s: line not parsed", c.name)
		}
		if rec.Severity != c.want || rec.SeverityText != c.text {
			t.Errorf("%s: severity %d %q, want %d %q", c.name, rec.Severity, rec.SeverityText, c.want, c.text)
		}
	}
}
