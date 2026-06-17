package otlp

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	sdklog "go.opentelemetry.io/otel/sdk/log"
	"go.opentelemetry.io/otel/sdk/resource"

	"senhub-agent.go/internal/agent/cliArgs"
	"senhub-agent.go/internal/agent/services/agentstate"
	"senhub-agent.go/internal/agent/services/logger"
)

func testModuleLogger(t *testing.T) *logger.ModuleLogger {
	t.Helper()
	return logger.NewModuleLogger(logger.NewLogger(&cliArgs.ParsedArgs{}), "test.logsqueue")
}

func countQueueFiles(t *testing.T, dir string) int {
	t.Helper()
	entries, err := os.ReadDir(filepath.Join(dir, logsQueueDirName))
	if err != nil {
		return 0
	}
	n := 0
	for _, e := range entries {
		if !e.IsDir() && filepath.Ext(e.Name()) == ".json" {
			n++
		}
	}
	return n
}

func sampleRecords(n int) []persistedLogRecord {
	out := make([]persistedLogRecord, n)
	for i := range out {
		out[i] = persistedLogRecord{
			TimestampUnixNano: int64(1700000000000000000 + i),
			SeverityNumber:    9,
			SeverityText:      "INFO",
			Body:              "event line",
			Attributes:        map[string]string{"senhub.probe.name": "syslog"},
		}
	}
	return out
}

func TestLogsQueue_EnqueueDrainRemove(t *testing.T) {
	dir := t.TempDir()
	q := newLogsQueue(dir, 0, testModuleLogger(t))

	if err := q.enqueue(sampleRecords(2)); err != nil {
		t.Fatalf("enqueue: %v", err)
	}
	if err := q.enqueue(sampleRecords(1)); err != nil {
		t.Fatalf("enqueue: %v", err)
	}
	if got := countQueueFiles(t, dir); got != 2 {
		t.Fatalf("queue files=%d, want 2", got)
	}

	var got []persistedLogRecord
	n := q.drain(func(recs []persistedLogRecord) { got = append(got, recs...) })
	if n != 3 {
		t.Errorf("drained=%d, want 3", n)
	}
	if len(got) != 3 {
		t.Errorf("emitted=%d, want 3", len(got))
	}
	if files := countQueueFiles(t, dir); files != 0 {
		t.Errorf("queue files after drain=%d, want 0", files)
	}
}

func TestLogsQueue_EvictOnSizeCap(t *testing.T) {
	dir := t.TempDir()
	// Tiny cap so the second enqueue forces eviction of the first batch.
	q := newLogsQueue(dir, 200, testModuleLogger(t))

	before := agentstate.GetOTLPDroppedByReason()["logs_queue_full"]
	for i := 0; i < 6; i++ {
		if err := q.enqueue(sampleRecords(3)); err != nil {
			t.Fatalf("enqueue %d: %v", i, err)
		}
	}
	// With a 200-byte cap and ~hundreds of bytes per batch, only the most
	// recent batch(es) survive; older ones are evicted.
	if files := countQueueFiles(t, dir); files >= 6 {
		t.Errorf("expected eviction to keep the queue small, got %d files", files)
	}
	after := agentstate.GetOTLPDroppedByReason()["logs_queue_full"]
	if after <= before {
		t.Errorf("expected logs_queue_full drops to increase (%d -> %d)", before, after)
	}
}

func TestLogsQueue_RecoverExistingDir(t *testing.T) {
	dir := t.TempDir()
	q1 := newLogsQueue(dir, 0, testModuleLogger(t))
	if err := q1.enqueue(sampleRecords(4)); err != nil {
		t.Fatalf("enqueue: %v", err)
	}

	// A fresh queue over the same dir must see the residue.
	q2 := newLogsQueue(dir, 0, testModuleLogger(t))
	q2.mu.Lock()
	recs := q2.records
	q2.mu.Unlock()
	if recs != 4 {
		t.Errorf("recovered records=%d, want 4", recs)
	}
}

// controllableExporter is a fake sdklog.Exporter that fails its first
// failUntil Export calls, then succeeds and captures records.
type controllableExporter struct {
	mu        sync.Mutex
	failUntil int
	calls     int
	got       []sdklog.Record
}

func (e *controllableExporter) Export(_ context.Context, records []sdklog.Record) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.calls++
	if e.calls <= e.failUntil {
		return errors.New("backend down")
	}
	e.got = append(e.got, records...)
	return nil
}
func (e *controllableExporter) ForceFlush(context.Context) error { return nil }
func (e *controllableExporter) Shutdown(context.Context) error   { return nil }
func (e *controllableExporter) captured() int {
	e.mu.Lock()
	defer e.mu.Unlock()
	return len(e.got)
}

// TestPersistentLogExporter_PersistThenReplay drives a real pipeline so
// records carry the logsScopeName scope (not settable by hand). A failed
// export persists the record to the queue; once the backend is healthy a
// replay re-emits it and the queue empties.
func TestPersistentLogExporter_PersistThenReplay(t *testing.T) {
	dir := t.TempDir()
	exp := &controllableExporter{failUntil: 1}
	q := newLogsQueue(dir, 0, testModuleLogger(t))
	ple := newPersistentLogExporter(exp, q, testModuleLogger(t))

	cfg := LogsSignal{BufferSize: 100, BatchSize: 1, BatchTimeout: time.Hour}
	pipe := buildLogsPipeline(ple, resource.NewSchemaless(), cfg, "test")
	replayer := newLogsReplayer(q, pipe, testModuleLogger(t))

	ctx := context.Background()
	pipe.emit(ctx, agentstate.LogRecord{
		Timestamp:         time.Unix(1700000000, 0),
		Severity:          9,
		SeverityText:      "INFO",
		Body:              "disk-or-bust",
		ProducerProbeName: "syslog",
	})
	// ForceFlush calls Export synchronously → first call fails → persisted.
	_ = pipe.provider.ForceFlush(ctx)

	q.mu.Lock()
	recs := q.records
	q.mu.Unlock()
	if recs == 0 {
		t.Fatalf("expected the failed export to be persisted, queue empty")
	}

	// Backend now healthy; replay re-emits, then flush exports it.
	replayer.replay()
	_ = pipe.provider.ForceFlush(ctx)

	if exp.captured() == 0 {
		t.Errorf("replayed record was not exported after recovery")
	}
	if files := countQueueFiles(t, dir); files != 0 {
		t.Errorf("queue not drained after replay: %d files", files)
	}
}
