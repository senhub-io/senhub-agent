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
