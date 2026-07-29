package event

import (
	"encoding/json"
	"fmt"
	"strconv"
	"time"

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

// EventMapToDataPoint builds the "event_event" DataPoint from a raw event
// map exactly as the event probe used to — every field stringified as a
// tag, array/object values additionally preserved under _complex_values so
// FormatDataPoint can restore their structure. Timestamp is passed in
// (parsed by the caller). This is the single source of truth for the event
// probe's /event/insert shape, shared by FromEventLog.
func (f *Formatter) EventMapToDataPoint(evt map[string]any, ts time.Time) datapoint.DataPoint {
	eventTags := make([]tags.Tag, 0, len(evt))
	complexValues := make(map[string]interface{})
	for key, value := range evt {
		if key == "timestamp" {
			continue
		}
		eventTags = append(eventTags, tags.Tag{Key: key, Value: fmt.Sprintf("%v", value)})
		switch value.(type) {
		case []interface{}, map[string]interface{}:
			complexValues[key] = value
		}
	}
	if len(complexValues) > 0 {
		if b, err := json.Marshal(complexValues); err == nil {
			eventTags = append(eventTags, tags.Tag{Key: "_complex_values", Value: string(b)})
		}
	}
	return datapoint.DataPoint{
		Name:      "event_event",
		Timestamp: ts,
		Value:     1.0,
		Tags:      eventTags,
	}
}

// FromEventLog converts an event-probe LogRecord (whose structured HTTP
// payload rides LogRecord.Fields) into the same EventDataPoint the event
// probe's DataPoint would have produced — so /event/insert is byte-identical
// whether the event strategy is fed from the metric datapoint path (legacy)
// or the log bus (#294 step 1b). Unlike syslog, the event payload can carry
// arrays/objects, which is why it needs Fields (the flat Attributes map
// could not hold them).
func (f *Formatter) FromEventLog(rec agentstate.LogRecord) event.EventDataPoint {
	return f.FormatDataPoint(f.EventMapToDataPoint(rec.Fields, rec.Timestamp))
}
