package otlp

import (
	"context"
	"testing"
	"time"

	"go.opentelemetry.io/otel/sdk/resource"

	"senhub-agent.go/internal/agent/services/agentstate"
)

// A collector that never answers must not hold the stop: an export already
// blocked in the SDK is cancelled, the batches behind it are not tried, and
// every record the pipeline held ends up on disk well inside the budget.
func TestLogsShutdown_BlockedExporterEverythingOnDiskUnderBudget(t *testing.T) {
	dir := t.TempDir()
	q := newLogsQueue(dir, 0, testModuleLogger(t))
	ple := newPersistentLogExporter(hangingExporter{}, q, testModuleLogger(t))
	cfg := LogsSignal{BufferSize: 1000, BatchSize: 5, BatchTimeout: time.Hour}
	pipe := buildLogsPipeline(ple, resource.NewSchemaless(), cfg, "test")

	const total = 23
	for i := 0; i < total; i++ {
		pipe.emit(context.Background(), agentstate.LogRecord{
			Timestamp: time.Unix(1700000000+int64(i), 0), Severity: 9, Body: "line", ProducerProbeName: "syslog",
		})
	}
	time.Sleep(50 * time.Millisecond) // the first full batch is now blocked in Export

	start := time.Now()
	ple.beginShutdown(logsShutdownFlushBudget)
	ctx, cancel := context.WithTimeout(context.Background(), logsShutdownFlushBudget)
	defer cancel()
	if err := pipe.shutdown(ctx); err != nil {
		t.Fatalf("shutdown: %v", err)
	}
	if elapsed := time.Since(start); elapsed > 500*time.Millisecond {
		t.Errorf("hand-off took %v, want well under %v", elapsed, logsShutdownFlushBudget)
	}
	if got, _ := q.pending(); got != total {
		t.Errorf("records on disk=%d, want all %d", got, total)
	}

	// Next boot, collector answering: everything goes out.
	r2 := newAckRig(t, dir, false)
	n, err := r2.rp.replay()
	if err != nil || n != total {
		t.Errorf("boot replay n=%d err=%v, want %d", n, err, total)
	}
}

// With a collector that answered a moment ago, the stop still delivers
// instead of parking everything on disk.
func TestLogsShutdown_KnownUpBackendIsStillTried(t *testing.T) {
	r := newAckRig(t, t.TempDir(), false)
	r.emitFlushed("warm") // a success: the backend is known up
	cfg := LogsSignal{BufferSize: 100, BatchSize: 50, BatchTimeout: time.Hour}
	r.pipe = buildLogsPipeline(r.ple, resource.NewSchemaless(), cfg, "test")
	r.pipe.emit(context.Background(), agentstate.LogRecord{Timestamp: time.Now(), Severity: 9, Body: "last", ProducerProbeName: "syslog"})

	r.ple.beginShutdown(logsShutdownFlushBudget)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	_ = r.pipe.shutdown(ctx)
	if got := len(r.exp.delivered()); got != 2 {
		t.Errorf("delivered=%d, want 2", got)
	}
	if r.pendingRecords() != 0 {
		t.Errorf("%d records parked on disk although the backend was up", r.pendingRecords())
	}
}
