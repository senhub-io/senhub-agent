package event

import (
	"encoding/json"
	"strconv"
	"testing"
	"time"

	"senhub-agent.go/internal/agent/services/agentstate"
	"senhub-agent.go/internal/agent/tags"
	"senhub-agent.go/internal/agent/types/datapoint"
)

// syslogInput mirrors the fields the syslog probe extracts from one message.
type syslogInput struct {
	facility int
	severity int
	priority int
	host     string
	message  string
	tag      string
	client   string
}

// datapointFrom builds the DataPoint exactly as syslogProbe.handleLogParts
// does (Name/Value/Tags) — the legacy /event/insert source.
func datapointFrom(in syslogInput, ts time.Time) datapoint.DataPoint {
	return datapoint.DataPoint{
		Name:      "syslog_event",
		Timestamp: ts,
		Value:     float64(in.severity),
		Tags: []tags.Tag{
			{Key: "facility", Value: strconv.Itoa(in.facility)},
			{Key: "severity", Value: strconv.Itoa(in.severity)},
			{Key: "host", Value: in.host},
			{Key: "message", Value: in.message},
			{Key: "tag", Value: in.tag},
			{Key: "client", Value: in.client},
			{Key: "priority", Value: strconv.Itoa(in.priority)},
		},
	}
}

// logRecordFrom builds the LogRecord exactly as syslogProbe.handleLogParts
// does — the new log-bus source.
func logRecordFrom(in syslogInput, ts time.Time) agentstate.LogRecord {
	return agentstate.LogRecord{
		Timestamp:    ts,
		Severity:     agentstate.SyslogPriorityToSeverity(in.severity),
		SeverityText: agentstate.SyslogPriorityToText(in.severity),
		Body:         in.message,
		Attributes: map[string]string{
			"syslog.facility":      strconv.Itoa(in.facility),
			"syslog.severity_code": strconv.Itoa(in.severity),
			"syslog.priority":      strconv.Itoa(in.priority),
			"syslog.hostname":      in.host,
			"syslog.appname":       in.tag,
			"syslog.client":        in.client,
		},
		ProducerProbeName: "syslog",
		ProducerProbeType: "syslog",
	}
}

// TestFromSyslogLog_ByteIdenticalToDataPointPath is the golden equivalence
// guard for #294 step 1a: the /event/insert JSON must be identical whether
// the event strategy is fed from the legacy DataPoint path or the new log
// bus. If the syslog probe ever stops carrying a field on the LogRecord that
// the DataPoint had, this fails.
func TestFromSyslogLog_ByteIdenticalToDataPointPath(t *testing.T) {
	f := NewFormatter()
	ts := time.Unix(1_700_000_000, 0).UTC()

	cases := []syslogInput{
		{facility: 1, severity: 6, priority: 14, host: "web01", message: "started", tag: "sshd", client: "10.0.0.1"},
		{facility: 0, severity: 0, priority: 0, host: "h", message: "emergency & <special> chars", tag: "kernel", client: "192.0.2.9"},
		{facility: 4, severity: 3, priority: 35, host: "db-2", message: "error: connection refused", tag: "", client: ""},
		{facility: 23, severity: 7, priority: 191, host: "edge", message: "debug line", tag: "app", client: "203.0.113.5"},
	}

	for _, in := range cases {
		t.Run(in.message, func(t *testing.T) {
			old := f.FormatDataPoint(datapointFrom(in, ts))
			neu := f.FromSyslogLog(logRecordFrom(in, ts))

			oldJSON, err := json.Marshal(old)
			if err != nil {
				t.Fatalf("marshal old: %v", err)
			}
			newJSON, err := json.Marshal(neu)
			if err != nil {
				t.Fatalf("marshal new: %v", err)
			}
			if string(oldJSON) != string(newJSON) {
				t.Errorf("/event/insert payload diverged\n old: %s\n new: %s", oldJSON, newJSON)
			}
		})
	}
}
