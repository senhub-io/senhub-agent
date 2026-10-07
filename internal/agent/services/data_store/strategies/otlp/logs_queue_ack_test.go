package otlp

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	sdklog "go.opentelemetry.io/otel/sdk/log"
	"go.opentelemetry.io/otel/sdk/resource"

	"senhub-agent.go/internal/agent/services/agentstate"
)

// switchExporter is a log exporter whose backend is down until told
// otherwise. allow, when >= 0, lets that many exports through and then
// fails the rest.
type switchExporter struct {
	mu    sync.Mutex
	down  bool
	allow int
	calls int
	got   []sdklog.Record
}

func newSwitchExporter(down bool) *switchExporter { return &switchExporter{down: down, allow: -1} }

func (e *switchExporter) Export(_ context.Context, records []sdklog.Record) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.calls++
	if e.down {
		return errors.New("backend down")
	}
	if e.allow == 0 {
		return errors.New("backend down")
	}
	if e.allow > 0 {
		e.allow--
	}
	e.got = append(e.got, records...)
	return nil
}
func (e *switchExporter) ForceFlush(context.Context) error { return nil }
func (e *switchExporter) Shutdown(context.Context) error   { return nil }
func (e *switchExporter) setDown(d bool) {
	e.mu.Lock()
	e.down = d
	e.mu.Unlock()
}
func (e *switchExporter) attempts() int {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.calls
}
func (e *switchExporter) delivered() []sdklog.Record {
	e.mu.Lock()
	defer e.mu.Unlock()
	return append([]sdklog.Record(nil), e.got...)
}

type fakeClock struct {
	mu sync.Mutex
	t  time.Time
}

func (c *fakeClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.t
}
func (c *fakeClock) Advance(d time.Duration) {
	c.mu.Lock()
	c.t = c.t.Add(d)
	c.mu.Unlock()
}

type ackRig struct {
	dir   string
	exp   *switchExporter
	clock *fakeClock
	q     *logsQueue
	ple   *persistentLogExporter
	pipe  *logsPipeline
	rp    *logsReplayer
}

func newAckRig(t *testing.T, dir string, down bool) *ackRig {
	t.Helper()
	r := &ackRig{dir: dir, exp: newSwitchExporter(down), clock: &fakeClock{t: time.Unix(1700000000, 0)}}
	r.q = newLogsQueue(dir, 0, testModuleLogger(t))
	r.ple = newPersistentLogExporter(r.exp, r.q, testModuleLogger(t))
	r.ple.now = r.clock.Now
	cfg := LogsSignal{BufferSize: 100, BatchSize: 1, BatchTimeout: time.Hour}
	r.pipe = buildLogsPipeline(r.ple, resource.NewSchemaless(), cfg, "test")
	r.rp = newLogsReplayer(r.q, r.pipe, r.ple, testModuleLogger(t))
	return r
}

// emitFlushed emits one event log and exports it synchronously.
func (r *ackRig) emitFlushed(body string) {
	ctx := context.Background()
	r.pipe.emit(ctx, agentstate.LogRecord{
		Timestamp: time.Unix(1700000000, 0), Severity: 9, SeverityText: "INFO",
		Body: body, ProducerProbeName: "syslog",
	})
	_ = r.pipe.provider.ForceFlush(ctx)
}

func (r *ackRig) pendingRecords() int {
	n, _ := r.q.pending()
	return n
}

type counterBase struct{ queued, replayed uint64 }

func baseline() counterBase {
	return counterBase{agentstate.GetOTLPLogsQueuedTotal(), agentstate.GetOTLPLogsReplayedTotal()}
}

// Collector down: batches pile up on disk and stay there, the network is
// not tried per batch, a replay that gets no ack deletes nothing; once the
// collector answers they are all replayed, removed, and the counters agree.
func TestLogsQueueAck_OutageThenRecovery(t *testing.T) {
	r := newAckRig(t, t.TempDir(), true)
	base := baseline()

	r.emitFlushed("a") // the one real attempt: it fails
	if got := r.exp.attempts(); got != 1 {
		t.Fatalf("first export attempts=%d, want 1", got)
	}
	for _, b := range []string{"b", "c", "d"} {
		r.emitFlushed(b) // known down: straight to disk
	}
	if got := r.exp.attempts(); got != 1 {
		t.Errorf("a known-down backend was tried %d times, want only the first failure", got)
	}
	if got := countQueueFiles(t, r.dir); got != 4 {
		t.Fatalf("files=%d, want 4", got)
	}

	// A replay probe with the collector still down must delete nothing.
	n, err := r.rp.replay()
	if err == nil || n != 0 {
		t.Fatalf("replay against a down backend: n=%d err=%v, want 0 and an error", n, err)
	}
	if got := countQueueFiles(t, r.dir); got != 4 {
		t.Errorf("a failed replay removed files: %d left, want 4", got)
	}
	if got := r.exp.attempts(); got != 2 {
		t.Errorf("a failed replay must stop at the first batch: attempts=%d, want 2", got)
	}
	if d := agentstate.GetOTLPLogsReplayedTotal() - base.replayed; d != 0 {
		t.Errorf("replayed_total moved by %d with nothing acknowledged", d)
	}

	r.exp.setDown(false)
	n, err = r.rp.replay()
	if err != nil || n != 4 {
		t.Fatalf("replay after recovery: n=%d err=%v, want 4 and no error", n, err)
	}
	if got := countQueueFiles(t, r.dir); got != 0 {
		t.Errorf("files left after an acknowledged replay: %d", got)
	}
	if got := len(r.exp.delivered()); got != 4 {
		t.Errorf("delivered=%d records, want 4", got)
	}
	for _, rec := range r.exp.delivered() {
		if rec.InstrumentationScope().Name != logsScopeName {
			t.Errorf("replayed record lost its scope: %q", rec.InstrumentationScope().Name)
		}
	}
	dq, dr := agentstate.GetOTLPLogsQueuedTotal()-base.queued, agentstate.GetOTLPLogsReplayedTotal()-base.replayed
	if dq != 4 || dr != 4 {
		t.Errorf("queued_total +%d, replayed_total +%d, want +4 each", dq, dr)
	}
	if r.pendingRecords() != 0 {
		t.Errorf("queue gauge says %d records pending", r.pendingRecords())
	}
}

// A replay that fails part-way keeps the failed batch and every later one.
func TestLogsQueueAck_FailureMidDrainKeepsTheRest(t *testing.T) {
	r := newAckRig(t, t.TempDir(), false)
	for i := 0; i < 4; i++ {
		if err := r.q.enqueue(sampleRecords(2)); err != nil {
			t.Fatal(err)
		}
	}
	r.exp.mu.Lock()
	r.exp.allow = 2
	r.exp.mu.Unlock()

	n, err := r.rp.replay()
	if err == nil {
		t.Fatal("expected the third batch to fail")
	}
	if n != 4 {
		t.Errorf("acknowledged records=%d, want 4 (two batches)", n)
	}
	if got := countQueueFiles(t, r.dir); got != 2 {
		t.Errorf("files left=%d, want 2", got)
	}
	if r.pendingRecords() != 4 {
		t.Errorf("pending records=%d, want 4", r.pendingRecords())
	}
}

// While the backend is known down, a live export is attempted only once per
// probe interval, and the first success marks recovery.
func TestLogsQueueAck_LiveProbeWhileDown(t *testing.T) {
	r := newAckRig(t, t.TempDir(), true)
	var recovered atomic.Int32
	r.ple.setOnRecovered(func() { recovered.Add(1) })

	r.emitFlushed("a")
	r.emitFlushed("b")
	if got := r.exp.attempts(); got != 1 {
		t.Fatalf("attempts=%d, want 1", got)
	}
	r.clock.Advance(logsProbeInterval + time.Second)
	r.emitFlushed("c") // one probe allowed
	if got := r.exp.attempts(); got != 2 {
		t.Fatalf("attempts after the probe interval=%d, want 2", got)
	}
	r.emitFlushed("d")
	if got := r.exp.attempts(); got != 2 {
		t.Errorf("a second attempt in the same interval: %d", got)
	}
	if !r.ple.backendDown() {
		t.Error("backend should still be marked down")
	}

	r.exp.setDown(false)
	r.clock.Advance(logsProbeInterval + time.Second)
	r.emitFlushed("e")
	if r.ple.backendDown() {
		t.Error("a successful export must mark the backend up")
	}
	deadline := time.Now().Add(2 * time.Second)
	for recovered.Load() == 0 && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	if recovered.Load() != 1 {
		t.Errorf("onRecovered fired %d times, want 1", recovered.Load())
	}
}

// Shutdown while the backend is down: what the SDK still holds in memory
// ends up on disk, and the next boot replays it once the backend answers.
func TestLogsQueueAck_ShutdownWhileDownThenRestart(t *testing.T) {
	for _, knownDown := range []bool{false, true} {
		dir := t.TempDir()
		r := newAckRig(t, dir, true)
		if knownDown {
			r.emitFlushed("earlier") // learns the backend is down
		}
		before := r.pendingRecords()

		// Held in the SDK batch: BatchSize 1 would flush, so use a pipeline
		// that only exports on shutdown.
		cfg := LogsSignal{BufferSize: 100, BatchSize: 50, BatchTimeout: time.Hour}
		r.pipe = buildLogsPipeline(r.ple, resource.NewSchemaless(), cfg, "test")
		ctx := context.Background()
		for _, body := range []string{"m1", "m2", "m3"} {
			r.pipe.emit(ctx, agentstate.LogRecord{
				Timestamp: time.Unix(1700000001, 0), Severity: 9, Body: body, ProducerProbeName: "syslog",
			})
		}
		if r.pendingRecords() != before {
			t.Fatalf("records reached the disk before the shutdown")
		}

		r.ple.beginShutdown(exporterShutdownBudget / 2)
		sctx, cancel := context.WithTimeout(ctx, time.Second)
		_ = r.pipe.shutdown(sctx)
		cancel()
		if got := r.pendingRecords(); got != before+3 {
			t.Fatalf("knownDown=%v: pending=%d after shutdown, want %d", knownDown, got, before+3)
		}

		// Restart: a new queue over the same directory, backend answering.
		r2 := newAckRig(t, dir, false)
		if r2.pendingRecords() != before+3 {
			t.Fatalf("restart sees %d records, want %d", r2.pendingRecords(), before+3)
		}
		n, err := r2.rp.replay()
		if err != nil || n != before+3 {
			t.Fatalf("restart replay n=%d err=%v, want %d", n, err, before+3)
		}
		if got := countQueueFiles(t, dir); got != 0 {
			t.Errorf("files left after the restart replay: %d", got)
		}
	}
}

// The retry clock backs off while the backend stays down, a batch queued
// meanwhile does not restart it, and nothing is attempted the moment a batch
// is queued. Once the backend answers the queue empties by itself.
func TestLogsQueueAck_BackoffLoop(t *testing.T) {
	r := newAckRig(t, t.TempDir(), true)
	r.rp.firstDelay = 60 * time.Millisecond
	r.rp.maxDelay = 480 * time.Millisecond
	r.ple.setOnQueued(r.rp.kick)
	r.rp.start()
	defer r.rp.stop()

	r.emitFlushed("a")
	for i := 0; i < 200; i++ {
		r.rp.kick()
	}
	time.Sleep(20 * time.Millisecond)
	if got := r.exp.attempts(); got != 1 {
		t.Fatalf("a queued batch triggered an immediate retry: attempts=%d", got)
	}

	// Probes at ~60, ~180, ~420 ms after the batch: at most 3 more by 450 ms.
	time.Sleep(430 * time.Millisecond)
	if got := r.exp.attempts(); got < 3 || got > 5 {
		t.Errorf("attempts after 450ms of outage=%d, want a bounded, backed-off 3 to 5", got)
	}

	r.exp.setDown(false)
	deadline := time.Now().Add(3 * time.Second)
	for r.pendingRecords() != 0 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if r.pendingRecords() != 0 {
		t.Fatal("the loop never replayed after the backend came back")
	}
	if got := countQueueFiles(t, r.dir); got != 0 {
		t.Errorf("files left: %d", got)
	}
}

// Stopping the replayer aborts a replay in flight instead of waiting for the
// export timeout.
func TestLogsQueueAck_StopAbortsReplay(t *testing.T) {
	r := newAckRig(t, t.TempDir(), false)
	if err := r.q.enqueue(sampleRecords(1)); err != nil {
		t.Fatal(err)
	}
	r.rp.stop()
	n, err := r.rp.replay()
	if err == nil || n != 0 {
		t.Errorf("replay after stop: n=%d err=%v, want 0 and an error", n, err)
	}
	if got := countQueueFiles(t, r.dir); got != 1 {
		t.Errorf("a cancelled replay removed the file: %d left", got)
	}
}

// Retention still applies to what an outage left behind.
func TestLogsQueueAck_RetentionStillDrops(t *testing.T) {
	r := newAckRig(t, t.TempDir(), true)
	r.q.now = r.clock.Now
	r.q.setMaxAge(time.Hour)
	r.emitFlushed("old")
	r.clock.Advance(2 * time.Hour)
	r.emitFlushed("new")
	if got := r.q.sweepAged(); got != 1 {
		t.Errorf("age sweep dropped %d records, want 1", got)
	}
	if got := countQueueFiles(t, r.dir); got != 1 {
		t.Errorf("files=%d, want only the young one", got)
	}
}
