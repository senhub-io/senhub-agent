package otlp

import (
	"context"
	"errors"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
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

// TestPersistentLogExporter_FailureCountsBySignal locks the #821 fix: a
// failed logs export must move the per-signal error counter, not only
// land in the dead-letter queue. In production a receiver rejecting
// every logs batch with 400 left export_errors_total flat.
func TestPersistentLogExporter_FailureCountsBySignal(t *testing.T) {
	dir := t.TempDir()
	exp := &controllableExporter{failUntil: 1}
	q := newLogsQueue(dir, 0, testModuleLogger(t))
	ple := newPersistentLogExporter(exp, q, testModuleLogger(t))

	cfg := LogsSignal{BufferSize: 100, BatchSize: 1, BatchTimeout: time.Hour}
	pipe := buildLogsPipeline(ple, resource.NewSchemaless(), cfg, "test")

	before := agentstate.GetOTLPExportErrorsBySignal()["logs"]
	ctx := context.Background()
	pipe.emit(ctx, agentstate.LogRecord{
		Timestamp:         time.Unix(1700000000, 0),
		Severity:          9,
		SeverityText:      "INFO",
		Body:              "count-me",
		ProducerProbeName: "syslog",
	})
	_ = pipe.provider.ForceFlush(ctx)

	if got := agentstate.GetOTLPExportErrorsBySignal()["logs"] - before; got != 1 {
		t.Errorf("logs export-error delta=%d, want 1", got)
	}
}

// rejectingExporter always refuses the payload on its merits, the way a
// receiver answers a malformed batch.
type rejectingExporter struct {
	mu    sync.Mutex
	calls int
}

func (e *rejectingExporter) Export(context.Context, []sdklog.Record) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.calls++
	return status.Error(codes.InvalidArgument, "unsupported attribute type")
}
func (e *rejectingExporter) ForceFlush(context.Context) error { return nil }
func (e *rejectingExporter) Shutdown(context.Context) error   { return nil }
func (e *rejectingExporter) attempts() int {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.calls
}

// TestPersistentLogExporter_RejectedBatchIsNotQueued is #833's answer in
// one behaviour: the dead-letter queue is for outages, not for payloads
// the receiver refuses.
//
// A rejected batch used to be written to disk, replayed at boot and on
// every recovery, rejected again, and written back — occupying space
// that the drop-oldest eviction then takes from batches that could
// still be delivered. An outage's worth of real event logs gets evicted
// to keep re-sending something the receiver has refused every time.
func TestPersistentLogExporter_RejectedBatchIsNotQueued(t *testing.T) {
	dir := t.TempDir()
	exp := &rejectingExporter{}
	q := newLogsQueue(dir, 0, testModuleLogger(t))
	ple := newPersistentLogExporter(exp, q, testModuleLogger(t))

	cfg := LogsSignal{BufferSize: 100, BatchSize: 1, BatchTimeout: time.Hour}
	pipe := buildLogsPipeline(ple, resource.NewSchemaless(), cfg, "test")

	ctx := context.Background()
	pipe.emit(ctx, agentstate.LogRecord{
		Timestamp:         time.Unix(1700000000, 0),
		Severity:          9,
		SeverityText:      "INFO",
		Body:              "the receiver will never take this",
		ProducerProbeName: "syslog",
	})
	_ = pipe.provider.ForceFlush(ctx)

	if exp.attempts() == 0 {
		t.Fatal("the exporter was never called; the test proves nothing")
	}

	q.mu.Lock()
	recs := q.records
	q.mu.Unlock()
	if recs != 0 {
		t.Errorf("a rejected batch was persisted: queue holds %d records", recs)
	}
	if files := countQueueFiles(t, dir); files != 0 {
		t.Errorf("a rejected batch left %d files on disk", files)
	}
}

// TestPersistentLogExporter_OutageIsStillQueued is the other half: the
// classification must not turn the dead-letter queue off. An unreachable
// backend is exactly what it exists for.
func TestPersistentLogExporter_OutageIsStillQueued(t *testing.T) {
	dir := t.TempDir()
	exp := &controllableExporter{failUntil: 1}
	q := newLogsQueue(dir, 0, testModuleLogger(t))
	ple := newPersistentLogExporter(exp, q, testModuleLogger(t))

	cfg := LogsSignal{BufferSize: 100, BatchSize: 1, BatchTimeout: time.Hour}
	pipe := buildLogsPipeline(ple, resource.NewSchemaless(), cfg, "test")

	ctx := context.Background()
	pipe.emit(ctx, agentstate.LogRecord{
		Timestamp:         time.Unix(1700000000, 0),
		Severity:          9,
		SeverityText:      "INFO",
		Body:              "keep me for the replay",
		ProducerProbeName: "syslog",
	})
	_ = pipe.provider.ForceFlush(ctx)

	q.mu.Lock()
	recs := q.records
	q.mu.Unlock()
	if recs == 0 {
		t.Error("an ordinary outage was not persisted — the dead-letter queue no longer does its job")
	}
}

// The logs rail is sparse: a queued batch used to wait for the next
// record on that same rail before it was retried, which on a quiet host
// is minutes — long enough for a consumer to expire the whole host and
// bring it back. The retry now runs on its own clock. Pins #845.
func TestLogsReplayerRetriesWithoutNewRecords(t *testing.T) {
	dir := t.TempDir()
	q := newLogsQueue(dir, 0, testModuleLogger(t))
	if err := q.enqueue(sampleRecords(3)); err != nil {
		t.Fatalf("enqueue: %v", err)
	}
	if n, waited := q.pending(); n != 3 || waited <= 0 {
		t.Fatalf("the queue must report what waits and for how long, got %d records waiting %v", n, waited)
	}

	drained := make(chan int, 4)
	r := newLogsReplayer(q, nil, testModuleLogger(t))
	// The pipeline is not exercised here: what is pinned is that a drain
	// happens at all without a new record arriving.
	r.running.Store(true)
	go func() {
		for {
			select {
			case <-r.quit:
				return
			case <-r.wake:
			}
			n := q.drain(func([]persistedLogRecord) {})
			drained <- n
		}
	}()
	r.kick()

	select {
	case n := <-drained:
		if n != 3 {
			t.Errorf("the queued records must be retried, got %d", n)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("nothing retried the queued batch")
	}
	close(r.quit)

	if n, waited := q.pending(); n != 0 || waited != 0 {
		t.Errorf("an empty queue reports nothing waiting, got %d records waiting %v", n, waited)
	}
}

// The delay must grow and stop growing, so a backend that stays down is
// retried without turning into a loop.
func TestReplayDelaysAreBounded(t *testing.T) {
	delay := replayFirstDelay
	for i := 0; i < 20; i++ {
		delay *= 2
		if delay > replayMaxDelay {
			delay = replayMaxDelay
		}
	}
	if delay != replayMaxDelay {
		t.Errorf("the delay must settle at the cap, got %v", delay)
	}
	if replayFirstDelay >= replayMaxDelay {
		t.Error("the first retry must come well before the cap")
	}
}
