package logparse

import (
	"testing"
	"time"

	"senhub-agent.go/internal/agent/services/agentstate"
)

func TestSniffSeverityReadsTheLevelWordAtTheHead(t *testing.T) {
	cases := map[string]agentstate.LogSeverity{
		"2026-09-07T15:31:52Z INFO tick 25 replica=x":                     agentstate.LogSeverityInfo,
		"[26-09-07 17:15:55.460] SquashTM - 13 ERROR [main] [] --- boom":  agentstate.LogSeverityError,
		"2026/09/07 19:55:26 [error] 9#9: *80 open() failed":              agentstate.LogSeverityError,
		"[2026-09-07 19:54:27 +0000] [1] [INFO] Starting gunicorn 23.0.0": agentstate.LogSeverityInfo,
		"WARNING: sun.misc.Unsafe::objectFieldOffset will be removed":     agentstate.LogSeverityWarn,
		"1:M 07 Sep 2026 19:36:30.123 * Ready to accept connections":      agentstate.LogSeverityUnspecified,
		"the user reported an error yesterday":                            agentstate.LogSeverityUnspecified,
		"":                                                                agentstate.LogSeverityUnspecified,
	}
	for line, want := range cases {
		if got, _ := SniffSeverity(line); got != want {
			t.Errorf("%q: severity %v, want %v", line, got, want)
		}
	}
	rec, _ := ParseLine(ParserConfig{Type: ParserRaw}, "2026-09-07 10:00:00 WARN disk almost full", time.Now(), "p", "filetail")
	if rec.Severity != agentstate.LogSeverityWarn || rec.SeverityText != "WARN" {
		t.Errorf("a raw line must carry the sniffed severity, got %v %q", rec.Severity, rec.SeverityText)
	}
	rec, _ = ParseLine(ParserConfig{Type: ParserJSON}, `{"level":"debug","msg":"ERROR-looking text"}`, time.Now(), "p", "filetail")
	if rec.Severity != agentstate.LogSeverityDebug {
		t.Errorf("a parsed level must not be overridden by a word in the message, got %v", rec.Severity)
	}
}
