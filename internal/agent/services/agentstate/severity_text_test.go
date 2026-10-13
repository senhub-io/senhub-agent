package agentstate

import "testing"

func TestSeverityFromText(t *testing.T) {
	cases := map[string]LogSeverity{
		"TRACE": LogSeverityTrace, "finest": LogSeverityTrace,
		"debug": LogSeverityDebug, "Verbose": LogSeverityDebug,
		"INFO": LogSeverityInfo, "Information": LogSeverityInfo, "informational": LogSeverityInfo, "notice": LogSeverityInfo,
		"warn": LogSeverityWarn, "WARNING": LogSeverityWarn,
		"error": LogSeverityError, "ERR": LogSeverityError, "severe": LogSeverityError,
		"fatal": LogSeverityFatal, "Critical": LogSeverityFatal, "alert": LogSeverityFatal, "EMERG": LogSeverityFatal,
		"30": LogSeverityInfo, "50": LogSeverityError,
		"": LogSeverityUnspecified, "whatever": LogSeverityUnspecified,
	}
	for in, want := range cases {
		if got := SeverityFromText(in); got != want {
			t.Errorf("SeverityFromText(%q) = %d, want %d", in, got, want)
		}
	}
}

// Syslog and journald share one ladder: every RFC 5424 priority has a
// number and a text.
func TestSyslogLadderCoversEveryPriority(t *testing.T) {
	for pri := 0; pri <= 7; pri++ {
		if SyslogPriorityToSeverity(pri) == LogSeverityUnspecified || SyslogPriorityToText(pri) == "" {
			t.Errorf("priority %d has no severity number or text", pri)
		}
	}
}
