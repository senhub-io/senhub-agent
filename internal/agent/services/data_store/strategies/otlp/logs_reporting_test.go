package otlp

import (
	"context"
	"strings"
	"testing"
	"time"

	"senhub-agent.go/internal/agent/services/agentstate"
)

// TestLogsRejectionIsReportedWithTheQueueOff pins that a consumer
// refusing part of a logs batch is counted and logged when no
// dead-letter queue sits in front of the exporter, the way it is with
// the queue on.
func TestLogsRejectionIsReportedWithTheQueueOff(t *testing.T) {
	agentstate.ResetExportRejectedForTest()

	s := newTestStrategy(t, map[string]interface{}{
		"endpoint":    startRejectingLogsServer(t),
		"tls":         map[string]interface{}{"enabled": false},
		"persistence": map[string]interface{}{"enabled": false},
		"logs": map[string]interface{}{
			"enabled":       true,
			"batch_size":    1,
			"batch_timeout": "1h",
		},
	})
	var buf syncBuffer
	s.psReporter = newPartialSuccessReporter(bufferModuleLogger(&buf), "export")

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := s.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}
	t.Cleanup(func() { _ = s.Shutdown(context.Background()) })

	if s.logsQueue != nil {
		t.Fatal("the logs queue is on; the test needs it off")
	}
	s.logs.emit(ctx, agentstate.LogRecord{
		Timestamp:         time.Unix(1_700_000_000, 0),
		Severity:          9,
		SeverityText:      "INFO",
		Body:              "record the consumer partly refuses",
		ProducerProbeName: "syslog",
	})
	_ = s.logs.provider.ForceFlush(ctx)

	var counted uint64
	for _, row := range agentstate.GetExportRejected() {
		if row.Signal == "logs" {
			counted = row.Count
		}
	}
	if counted != 6 {
		t.Errorf("export.rejected{signal=logs} = %d, want 6", counted)
	}
	out := buf.String()
	if !strings.Contains(out, "has_segment") || !strings.Contains(out, `"origin":"export"`) {
		t.Errorf("the rejection was not logged by the export reporter:\n%s", out)
	}
}

// TestEntityRejectionIsReportedWithTheQueueOff pins the entity-event
// path: entity records ride the logs pipeline even when raw logs are
// off, and a consumer refusing them must be counted per record and
// logged once per window, not once per batch.
func TestEntityRejectionIsReportedWithTheQueueOff(t *testing.T) {
	agentstate.ResetExportRejectedForTest()

	s := newTestStrategy(t, map[string]interface{}{
		"endpoint":    startRejectingLogsServer(t),
		"tls":         map[string]interface{}{"enabled": false},
		"persistence": map[string]interface{}{"enabled": false},
		"signals": map[string]interface{}{
			"logs":     map[string]interface{}{"enabled": false},
			"entities": map[string]interface{}{"enabled": true},
		},
	})
	var buf syncBuffer
	s.psReporter = newPartialSuccessReporter(bufferModuleLogger(&buf), "export")

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := s.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}
	t.Cleanup(func() { _ = s.Shutdown(context.Background()) })

	if s.logsQueue != nil || s.logs == nil || s.cfg.Logs.Enabled || !s.cfg.Entities.Enabled {
		t.Fatal("the test needs raw logs off, entities on and the queue off")
	}
	for i := 0; i < 2; i++ {
		scope, rec, err := buildEntityRecord(entityEvent("host", "h-00"+string(rune('1'+i)), "", time.Unix(1_700_000_000, 0)))
		if err != nil {
			t.Fatalf("buildEntityRecord: %v", err)
		}
		s.logs.emitEntityRecord(ctx, scope, rec)
		if err := s.logs.provider.ForceFlush(ctx); err != nil {
			t.Fatalf("ForceFlush: %v", err)
		}
	}

	var counted uint64
	for _, row := range agentstate.GetExportRejected() {
		if row.Signal == "logs" {
			counted = row.Count
		}
	}
	if counted != 12 {
		t.Errorf("export.rejected{signal=logs} = %d, want 12 (two batches of 6)", counted)
	}
	out := buf.String()
	if n := strings.Count(out, "has_segment"); n != 1 {
		t.Errorf("rejection logged %d times, want once per window:\n%s", n, out)
	}
}
