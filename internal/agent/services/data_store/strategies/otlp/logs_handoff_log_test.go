package otlp

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"go.opentelemetry.io/otel/sdk/resource"

	"senhub-agent.go/internal/agent/services/agentstate"
	"senhub-agent.go/internal/agent/services/logger"
)

func captureLogger(buf *bytes.Buffer) *logger.ModuleLogger {
	zl := zerolog.New(buf)
	return logger.NewModuleLogger(&zl, "test.handoff")
}

// What the operator reads at a clean stop must match what happened to the
// records: kept on disk is an Info line, never an error; a warning is for
// records that were really dropped.
func TestReportLogsHandoff_MessagesMatchOutcome(t *testing.T) {
	cases := []struct {
		name     string
		kept     int
		lost     int64
		err      error
		want     []string
		wantNot  []string
		wantInfo bool
	}{
		{"kept, SDK deadline", 24, 0, context.DeadlineExceeded, []string{`"level":"info"`, "24 log records kept on disk for the next start"}, []string{"failed", `"level":"warn"`, "error"}, true},
		{"nothing pending", 0, 0, nil, nil, []string{"failed", "kept on disk", "warn"}, false},
		{"records lost", 5, 3, context.DeadlineExceeded, []string{`"level":"warn"`, `"records_lost":3`}, []string{"kept on disk for the next start"}, false},
		{"real error", 0, 0, errors.New("boom"), []string{`"level":"warn"`, "shutdown failed"}, nil, false},
	}
	for _, c := range cases {
		var buf bytes.Buffer
		reportLogsHandoff(captureLogger(&buf), c.kept, c.lost, c.err)
		out := buf.String()
		for _, w := range c.want {
			if !strings.Contains(out, w) {
				t.Errorf("%s: output %q lacks %q", c.name, out, w)
			}
		}
		for _, w := range c.wantNot {
			if strings.Contains(out, w) {
				t.Errorf("%s: output %q must not contain %q", c.name, out, w)
			}
		}
	}
}

// End to end on the exporter: a blocked collector at stop leaves every
// record on disk and the exporter reports none as lost.
func TestLogsShutdown_BlockedExporterReportsNothingLost(t *testing.T) {
	q := newLogsQueue(t.TempDir(), 0, testModuleLogger(t))
	ple := newPersistentLogExporter(hangingExporter{}, q, testModuleLogger(t))
	cfg := LogsSignal{BufferSize: 100, BatchSize: 50, BatchTimeout: time.Hour}
	pipe := buildLogsPipeline(ple, resource.NewSchemaless(), cfg, "test")
	for i := 0; i < 7; i++ {
		pipe.emit(context.Background(), agentstate.LogRecord{Timestamp: time.Now(), Severity: 9, Body: "l", ProducerProbeName: "syslog"})
	}
	before, _ := q.pending()
	ple.beginShutdown(logsShutdownFlushBudget)
	ctx, cancel := context.WithTimeout(context.Background(), logsShutdownFlushBudget)
	defer cancel()
	err := pipe.shutdown(ctx)
	after, _ := q.pending()

	var buf bytes.Buffer
	reportLogsHandoff(captureLogger(&buf), after-before, ple.lostRecords.Load(), err)
	if !strings.Contains(buf.String(), "7 log records kept on disk for the next start") || strings.Contains(buf.String(), "warn") {
		t.Errorf("unexpected hand-off log: %q", buf.String())
	}
}
