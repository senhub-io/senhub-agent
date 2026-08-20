package agentstate

import "testing"

// TestSyslogLadderIsComplete pins every rung in all three vocabularies.
// The point of one table is that this is the only place the mapping is
// asserted; the point of asserting it here is that a typo in a rung is
// otherwise invisible until an operator's alert fires on the wrong
// severity.
func TestSyslogLadderIsComplete(t *testing.T) {
	cases := []struct {
		code      int
		otel      LogSeverity
		text      string
		eventName string
		probeName string
	}{
		{0, 24, "FATAL4", "emerg", "EMERG"},
		{1, 23, "FATAL3", "alert", "ALERT"},
		{2, 22, "FATAL2", "crit", "CRIT"},
		{3, LogSeverityError, "ERROR", "err", "ERR"},
		{4, LogSeverityWarn, "WARN", "warning", "WARNING"},
		{5, 10, "INFO2", "notice", "NOTICE"},
		{6, LogSeverityInfo, "INFO", "info", "INFO"},
		{7, LogSeverityDebug, "DEBUG", "debug", "DEBUG"},
	}

	for _, c := range cases {
		if got := SyslogPriorityToSeverity(c.code); got != c.otel {
			t.Errorf("SyslogPriorityToSeverity(%d) = %d, want %d", c.code, got, c.otel)
		}
		if got := SyslogPriorityToText(c.code); got != c.text {
			t.Errorf("SyslogPriorityToText(%d) = %q, want %q", c.code, got, c.text)
		}
		if got := SyslogPriorityToEventName(c.code); got != c.eventName {
			t.Errorf("SyslogPriorityToEventName(%d) = %q, want %q", c.code, got, c.eventName)
		}
		got, ok := EventProbeSeverityToOTel(c.probeName)
		if !ok {
			t.Errorf("EventProbeSeverityToOTel(%q) reported the name as unknown", c.probeName)
			continue
		}
		if got != c.otel {
			t.Errorf("EventProbeSeverityToOTel(%q) = %d, want %d", c.probeName, got, c.otel)
		}
	}
}

// TestOutOfRangeSyslogCodeIsUnspecified pins what the ladder itself
// answers for an input outside 0..7: nothing. Each rail decides what an
// unknown severity means on its own terms — the legacy /event/insert
// rail substitutes "notice", a wire-format behaviour its consumers
// depend on, while the OTel rail carries Unspecified. Keeping the
// substitution at the caller is what lets one table serve both without
// the ladder inventing a level.
func TestOutOfRangeSyslogCodeIsUnspecified(t *testing.T) {
	for _, code := range []int{-1, 8, 99} {
		if got := SyslogPriorityToSeverity(code); got != LogSeverityUnspecified {
			t.Errorf("SyslogPriorityToSeverity(%d) = %d, want Unspecified", code, got)
		}
		if got := SyslogPriorityToText(code); got != "" {
			t.Errorf("SyslogPriorityToText(%d) = %q, want empty", code, got)
		}
		if got := SyslogPriorityToEventName(code); got != "" {
			t.Errorf("SyslogPriorityToEventName(%d) = %q, want empty", code, got)
		}
	}
}

// TestEventProbeSeverityNamesMatchTheLadder keeps the probe's accepted
// set derived rather than duplicated: it used to be a separate map that
// had to be kept in step with the mapping beside it.
func TestEventProbeSeverityNamesMatchTheLadder(t *testing.T) {
	names := EventProbeSeverityNames()
	if len(names) != len(syslogSeverities) {
		t.Fatalf("EventProbeSeverityNames returned %d names, want %d", len(names), len(syslogSeverities))
	}
	for _, n := range names {
		if _, ok := EventProbeSeverityToOTel(n); !ok {
			t.Errorf("accepted name %q has no rung on the ladder", n)
		}
	}
	if _, ok := EventProbeSeverityToOTel("NOT-A-SEVERITY"); ok {
		t.Error("an unknown name was accepted")
	}
}
