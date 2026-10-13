package agentstate

import (
	"strconv"
	"strings"
)

// SeverityFromText maps the level word a log source wrote to its OTel
// SeverityNumber, so a record is never left without one because its source
// spelled the level in its own way. The text itself is left to the caller:
// SeverityText stays what the source wrote. An unknown word yields
// LogSeverityUnspecified.
//
// Numeric levels use the Bunyan/Pino scale (10 trace ... 60 fatal).
func SeverityFromText(text string) LogSeverity {
	t := strings.ToLower(strings.TrimSpace(text))
	switch t {
	case "trace", "finest", "finer", "trace2", "trace3", "trace4":
		return LogSeverityTrace
	case "debug", "dbg", "fine", "verbose", "debug2", "debug3", "debug4":
		return LogSeverityDebug
	case "info", "information", "informational", "notice", "config", "log", "info2", "info3", "info4":
		return LogSeverityInfo
	case "warn", "warning", "warn2", "warn3", "warn4":
		return LogSeverityWarn
	case "error", "err", "severe", "failure", "error2", "error3", "error4":
		return LogSeverityError
	case "fatal", "critical", "crit", "alert", "emerg", "emergency", "panic", "fatal2", "fatal3", "fatal4":
		return LogSeverityFatal
	}
	if n, err := strconv.Atoi(t); err == nil {
		switch {
		case n >= 60:
			return LogSeverityFatal
		case n >= 50:
			return LogSeverityError
		case n >= 40:
			return LogSeverityWarn
		case n >= 30:
			return LogSeverityInfo
		case n >= 20:
			return LogSeverityDebug
		case n >= 10:
			return LogSeverityTrace
		}
	}
	return LogSeverityUnspecified
}
