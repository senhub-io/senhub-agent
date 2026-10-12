package otlpreceiver

import (
	"testing"

	logspb "go.opentelemetry.io/proto/otlp/logs/v1"

	"senhub-agent.go/internal/agent/services/agentstate"
)

// The relay keeps the sender's number and text untouched (#604).
func TestLogRecordToInternal_KeepsTheSendersSeverity(t *testing.T) {
	rec := logRecordToInternal(&logspb.LogRecord{SeverityNumber: logspb.SeverityNumber(17), SeverityText: "boom"}, nil, "r")
	if rec.Severity != agentstate.LogSeverityError || rec.SeverityText != "boom" {
		t.Errorf("severity %d %q, want the sender's 17 and \"boom\"", rec.Severity, rec.SeverityText)
	}
	// A sender that wrote only text keeps it; the OTLP output derives the
	// number from it on the way out.
	text := logRecordToInternal(&logspb.LogRecord{SeverityText: "WARN"}, nil, "r")
	if text.SeverityText != "WARN" {
		t.Errorf("text %q, want the sender's", text.SeverityText)
	}
}

func TestFillSeverityNumbers_AddsOnlyWhatIsAbsent(t *testing.T) {
	recs := []*logspb.LogRecord{
		{SeverityText: "WARNING"},
		{SeverityText: "CRITICAL"},
		{SeverityText: "warn", SeverityNumber: logspb.SeverityNumber(17)},
		{SeverityText: "whatever"},
		{},
	}
	rls := []*logspb.ResourceLogs{{ScopeLogs: []*logspb.ScopeLogs{{LogRecords: recs}}}}
	fillSeverityNumbers(rls)

	want := []logspb.SeverityNumber{
		logspb.SeverityNumber(agentstate.LogSeverityWarn),
		logspb.SeverityNumber(agentstate.LogSeverityFatal),
		17, 0, 0,
	}
	wantText := []string{"WARNING", "CRITICAL", "warn", "whatever", ""}
	for i, r := range recs {
		if r.SeverityNumber != want[i] || r.SeverityText != wantText[i] {
			t.Errorf("record %d: %d %q, want %d %q", i, r.SeverityNumber, r.SeverityText, want[i], wantText[i])
		}
	}
}
