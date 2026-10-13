package otlp

import (
	"context"
	"testing"
	"time"

	"go.opentelemetry.io/otel/log"

	"senhub-agent.go/internal/agent/services/agentstate"
)

// A record that reaches the OTLP output with a text and no number leaves
// with the number the text means; the text is untouched, and a number the
// source set is never overridden (#604).
func TestLogsPipeline_EmitDerivesTheNumberFromTheText(t *testing.T) {
	pipe, exp := newTestLogsPipeline(t, 10, 1024, 10*time.Millisecond)
	defer pipe.shutdown(context.Background())

	pipe.emit(context.Background(), agentstate.LogRecord{Timestamp: time.Now(), SeverityText: "ERROR", Body: "text only"})
	pipe.emit(context.Background(), agentstate.LogRecord{Timestamp: time.Now(), Severity: agentstate.LogSeverityWarn, SeverityText: "ERROR", Body: "both"})

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) && len(exp.snapshot()) < 2 {
		time.Sleep(20 * time.Millisecond)
	}
	got := exp.snapshot()
	if len(got) != 2 {
		t.Fatalf("got %d records, want 2", len(got))
	}
	want := map[string]log.Severity{"text only": log.SeverityError1, "both": log.SeverityWarn1}
	for _, r := range got {
		if r.Severity() != want[r.Body().AsString()] || r.SeverityText() != "ERROR" {
			t.Errorf("%q: severity %v text %q", r.Body().AsString(), r.Severity(), r.SeverityText())
		}
	}
}
