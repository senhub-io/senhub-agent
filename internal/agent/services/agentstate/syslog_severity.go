package agentstate

// The syslog severity ladder, once.
//
// RFC 5424 defines eight severities. The agent needs each of them in
// three vocabularies: the OTel SeverityNumber the log rail carries, the
// OTel SeverityText that goes with it, and the lowercase name the legacy
// /event/insert payload uses. Those three used to live in three separate
// hand-maintained tables — one here, one in formats/event, one in the
// event probe — and they had already drifted: an unparseable severity
// became "notice" on the legacy rail and Unspecified on the OTel rail,
// so the same record arrived with two different severities depending on
// which sink read it (#294).
//
// One table, three views. A ninth severity is not coming; the cost of
// keeping this in sync was never worth the three copies.

// syslogSeverity is one rung of the ladder.
type syslogSeverity struct {
	// OTel is the SeverityNumber from the OTel logs data model.
	OTel LogSeverity
	// Text is the OTel SeverityText that accompanies it.
	Text string
	// EventName is the lowercase name the legacy /event/insert payload
	// carries ("emerg", "err", "warning", …).
	EventName string
	// EventProbeName is the uppercase name the event probe accepts on
	// its HTTP surface ("EMERG", "ERR", "WARNING", …). It is a distinct
	// spelling of the same rung, not a distinct concept.
	EventProbeName string
}

// syslogSeverities is indexed by the RFC 5424 severity code, 0..7.
var syslogSeverities = [8]syslogSeverity{
	0: {OTel: 24, Text: "FATAL4", EventName: "emerg", EventProbeName: "EMERG"},
	1: {OTel: 23, Text: "FATAL3", EventName: "alert", EventProbeName: "ALERT"},
	2: {OTel: 22, Text: "FATAL2", EventName: "crit", EventProbeName: "CRIT"},
	3: {OTel: LogSeverityError, Text: "ERROR", EventName: "err", EventProbeName: "ERR"},
	4: {OTel: LogSeverityWarn, Text: "WARN", EventName: "warning", EventProbeName: "WARNING"},
	5: {OTel: 10, Text: "INFO2", EventName: "notice", EventProbeName: "NOTICE"},
	6: {OTel: LogSeverityInfo, Text: "INFO", EventName: "info", EventProbeName: "INFO"},
	7: {OTel: LogSeverityDebug, Text: "DEBUG", EventName: "debug", EventProbeName: "DEBUG"},
}

// validSyslogCode reports whether pri is one of the eight defined codes.
func validSyslogCode(pri int) bool { return pri >= 0 && pri < len(syslogSeverities) }

// SyslogPriorityToSeverity maps an RFC 5424 severity code to its OTel
// SeverityNumber.
//
// Out-of-range inputs return Unspecified rather than panicking — the
// path has to stay resilient to malformed syslog messages, and
// Unspecified is the honest answer: the agent does not know how severe
// this is, and inventing a level would put a guess in an operator's
// alert rule.
func SyslogPriorityToSeverity(pri int) LogSeverity {
	if !validSyslogCode(pri) {
		return LogSeverityUnspecified
	}
	return syslogSeverities[pri].OTel
}

// SyslogPriorityToText returns the OTel SeverityText for the same
// mapping. Empty string for out-of-range inputs.
func SyslogPriorityToText(pri int) string {
	if !validSyslogCode(pri) {
		return ""
	}
	return syslogSeverities[pri].Text
}

// SyslogPriorityToEventName returns the lowercase name the legacy
// /event/insert payload carries. Out-of-range inputs return "" — the
// caller decides what an unknown severity means on its own rail rather
// than inheriting a default invented here.
func SyslogPriorityToEventName(pri int) string {
	if !validSyslogCode(pri) {
		return ""
	}
	return syslogSeverities[pri].EventName
}

// EventProbeSeverityToOTel maps a severity name accepted on the event
// probe's HTTP surface ("EMERG", "ERR", …) to its OTel SeverityNumber.
// Reports false for a name that is not one of the eight.
func EventProbeSeverityToOTel(name string) (LogSeverity, bool) {
	for _, s := range syslogSeverities {
		if s.EventProbeName == name {
			return s.OTel, true
		}
	}
	return LogSeverityUnspecified, false
}

// EventProbeSeverityNames returns the eight names the event probe
// accepts, so its validation set is the ladder rather than a second
// list that has to be kept in step with it.
func EventProbeSeverityNames() []string {
	names := make([]string, 0, len(syslogSeverities))
	for _, s := range syslogSeverities {
		names = append(names, s.EventProbeName)
	}
	return names
}
