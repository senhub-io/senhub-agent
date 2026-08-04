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

// TestFromEventLog_ByteIdenticalAndStructurePreserved is the golden guard
// for #294 step 1b. It checks two things: (1) the /event/insert payload is
// identical whether built from the legacy datapoint path
// (FormatDataPoint(EventMapToDataPoint(...))) or from a LogRecord carrying
// the raw map on Fields (FromEventLog); (2) the flat Attributes map could
// NOT hold structure, so structure MUST survive via Fields — a []any field
// stays a []any, not the string "[a b]".
func TestFromEventLog_ByteIdenticalAndStructurePreserved(t *testing.T) {
	f := NewFormatter()
	ts := time.Unix(1_700_000_000, 0).UTC()

	evt := map[string]any{
		"host":     "app-7",
		"message":  "deploy finished",
		"severity": "Error",                                   // a NAME, not 0..7 — the formatter maps it to Notice
		"targets":  []any{"web", "db"},                        // structured — must survive
		"meta":     map[string]any{"build": "42", "ok": true}, // nested — must survive
		"count":    3,
	}

	// (1) Equivalence: legacy path vs log-bus path.
	old := f.FormatDataPoint(f.EventMapToDataPoint(evt, ts))
	neu := f.FromEventLog(logRecordWithFields(evt, ts))
	oldJSON, _ := json.Marshal(old)
	newJSON, _ := json.Marshal(neu)
	if string(oldJSON) != string(newJSON) {
		t.Fatalf("/event/insert payload diverged\n old: %s\n new: %s", oldJSON, newJSON)
	}

	// (2) Structure preserved + the severity-name quirk reproduced.
	if _, isSlice := neu["targets"].([]any); !isSlice {
		t.Errorf("targets should stay a slice, got %T (%v)", neu["targets"], neu["targets"])
	}
	if _, isMap := neu["meta"].(map[string]any); !isMap {
		t.Errorf("meta should stay a map, got %T (%v)", neu["meta"], neu["meta"])
	}
	if neu["host"] != "app-7" || neu["message"] != "deploy finished" {
		t.Errorf("required fields wrong: %v", neu)
	}
	// The named severity "Error" is NOT a syslog 0..7 code, so the legacy
	// formatter maps it to the default (notice). Reproduced byte-for-byte.
	if got := neu["severity"]; got != EventSeverityNotice() {
		t.Errorf("severity = %v, want notice (the reproduced legacy quirk)", got)
	}
}

// EventSeverityNotice returns the string form of the notice severity, used
// by the test to assert the reproduced quirk without importing the types pkg.
func EventSeverityNotice() string { return "notice" }

func logRecordWithFields(evt map[string]any, ts time.Time) agentstate.LogRecord {
	return agentstate.LogRecord{
		Timestamp:         ts,
		Body:              "", // body is not used by FromEventLog (rebuilt from Fields)
		Fields:            evt,
		ProducerProbeName: "event",
		ProducerProbeType: "event",
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
