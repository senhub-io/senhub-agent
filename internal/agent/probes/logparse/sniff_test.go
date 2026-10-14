package logparse

import (
	"testing"
	"time"

	"senhub-agent.go/internal/agent/services/agentstate"
)

func TestSniffSeverityReadsTheLevelWordAtTheHead(t *testing.T) {
	cases := map[string]agentstate.LogSeverity{
		"2026-09-07T15:31:52Z INFO tick 25 replica=x":                                    agentstate.LogSeverityInfo,
		"[26-09-07 17:15:55.460] SquashTM - 13 ERROR [main] [] --- boom":                 agentstate.LogSeverityError,
		"2026/09/07 19:55:26 [error] 9#9: *80 open() failed":                             agentstate.LogSeverityError,
		"[2026-09-07 19:54:27 +0000] [1] [INFO] Starting gunicorn 23.0.0":                agentstate.LogSeverityInfo,
		"WARNING: sun.misc.Unsafe::objectFieldOffset will be removed":                    agentstate.LogSeverityWarn,
		"1:M 07 Sep 2026 19:36:30.123 * Ready to accept connections":                     agentstate.LogSeverityUnspecified,
		"the user reported an error yesterday":                                           agentstate.LogSeverityUnspecified,
		"":                                                                               agentstate.LogSeverityUnspecified,
		"level=notice msg=hello":                                                         agentstate.LogSeverityInfo,
		`ts=2026-10-12T14:00:00Z lvl="error" msg="boom"`:                                 agentstate.LogSeverityError,
		"2026-10-12T14:00:00Z warn simulated warning":                                    agentstate.LogSeverityWarn,
		"2026-10-12 14:00:00 error simulated error":                                      agentstate.LogSeverityError,
		"error: connection refused":                                                      agentstate.LogSeverityError,
		"request levelled=error by the user":                                             agentstate.LogSeverityUnspecified,
		"I0907 10:00:00.123456 12345 klog.go:42] message":                                agentstate.LogSeverityInfo,
		"W0907 10:00:00.123456 12345 klog.go:42] message":                                agentstate.LogSeverityWarn,
		"E0907 10:00:00.123456 12345 klog.go:42] message":                                agentstate.LogSeverityError,
		"F0907 10:00:00.123456 12345 klog.go:42] message":                                agentstate.LogSeverityFatal,
		"2026-09-07T10:00:00Z INF starting":                                              agentstate.LogSeverityInfo,
		"2026-09-07T10:00:00Z WRN disk usage high":                                       agentstate.LogSeverityWarn,
		"2026-09-07T10:00:00Z ERR x":                                                     agentstate.LogSeverityError,
		"2026-09-07 10:00:00 [WRN] Disk usage above 80%":                                 agentstate.LogSeverityWarn,
		"2026-09-07 10:00:00 [wrn] Disk usage above 80%":                                 agentstate.LogSeverityWarn,
		"2026-09-07 10:00:00 [InF] Starting":                                             agentstate.LogSeverityInfo,
		"2026-09-07 10:00:00 DBG x":                                                      agentstate.LogSeverityDebug,
		"2026-09-07 10:00:00 TRC x":                                                      agentstate.LogSeverityTrace,
		"2026-09-07 10:00:00 FTL x":                                                      agentstate.LogSeverityFatal,
		"2026-09-07 10:00:00 CRT x":                                                      agentstate.LogSeverityFatal,
		"Sep 07, 2026 10:00:00 AM com.example.Main main SEVERE: something failed":        agentstate.LogSeverityError,
		"Sep 07, 2026 10:00:00 AM com.example.Main main FINE: something":                 agentstate.LogSeverityDebug,
		"Sep 07, 2026 10:00:00 AM com.example.Main main FINER: something":                agentstate.LogSeverityTrace,
		"Sep 07, 2026 10:00:00 AM com.example.Main main FINEST: something":               agentstate.LogSeverityTrace,
		"Sep 07, 2026 10:00:00 AM com.example.Main main CONFIG: something":               agentstate.LogSeverityInfo,
		"2026-09-07 10:00:00 UTC [12345]: LOG:  statement: SELECT 1":                     agentstate.LogSeverityInfo,
		"2026-09-07T10:00:00.123456Z 0 [Note] [MY-010914] [Server] Aborted connection":   agentstate.LogSeverityInfo,
		"2026-09-07T10:00:00.123456Z 0 [System] [MY-010914] [Server] Aborted connection": agentstate.LogSeverityInfo,
		"[core:error] something":                                                         agentstate.LogSeverityError,
		"[ssl:warn] something":                                                           agentstate.LogSeverityWarn,
		"[mpm_event:notice] something":                                                   agentstate.LogSeverityInfo,
		`{"level":"warn","msg":"disk almost full"}`:                                      agentstate.LogSeverityWarn,
		`{"severity":"ERROR"}`:                                                           agentstate.LogSeverityError,
		`{"lvl":"dbg","msg":"x"}`:                                                        agentstate.LogSeverityDebug,
		"Info about the deploy is in the wiki":                                           agentstate.LogSeverityInfo,
		"LOG in to continue":                                                             agentstate.LogSeverityUnspecified,
		"I0907 is a ticket number":                                                       agentstate.LogSeverityUnspecified,
		"FINE weather":                                                                   agentstate.LogSeverityUnspecified,
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
