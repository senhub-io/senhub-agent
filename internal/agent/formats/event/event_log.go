package event

import (
	"strconv"

	"senhub-agent.go/internal/agent/services/agentstate"
	"senhub-agent.go/internal/agent/tags"
	"senhub-agent.go/internal/agent/types/datapoint"
	"senhub-agent.go/internal/agent/types/event"
)

// FromSyslogLog converts a syslog LogRecord (published on the agent log bus)
// into the exact same EventDataPoint the syslog probe's DataPoint would have
// produced through FormatDataPoint — so the /event/insert payload is
// byte-identical whether the event strategy is fed from the metric datapoint
// path (legacy) or from the log bus (#294 step 1a).
//
// It works by RECONSTRUCTING the DataPoint the probe built (same Name, Value,
// Timestamp and tag set) from the LogRecord's attributes, then delegating to
// FormatDataPoint. Reusing the one formatter guarantees identical formatting;
// the only thing this function must get right is field parity — which the
// equivalence test pins against the probe's real construction.
//
// Only syslog records are handled here: their payload is flat and fully
// carried by the LogRecord. The event (HTTP) probe's payloads can carry
// structured values that the flat log model does not preserve, so they are
// NOT migrated through this path (see #294 step 1b).
func (f *Formatter) FromSyslogLog(rec agentstate.LogRecord) event.EventDataPoint {
	severityCode := rec.Attributes["syslog.severity_code"]

	// Value mirrors the probe's DataPoint.Value = float64(severity).
	var value float64
	if n, err := strconv.Atoi(severityCode); err == nil {
		value = float64(n)
	}

	dp := datapoint.DataPoint{
		Name:      "syslog_event",
		Timestamp: rec.Timestamp,
		Value:     value,
		Tags: []tags.Tag{
			{Key: "facility", Value: rec.Attributes["syslog.facility"]},
			{Key: "severity", Value: severityCode},
			{Key: "host", Value: rec.Attributes["syslog.hostname"]},
			{Key: "message", Value: rec.Body},
			{Key: "tag", Value: rec.Attributes["syslog.appname"]},
			{Key: "client", Value: rec.Attributes["syslog.client"]},
			{Key: "priority", Value: rec.Attributes["syslog.priority"]},
		},
	}
	return f.FormatDataPoint(dp)
}
