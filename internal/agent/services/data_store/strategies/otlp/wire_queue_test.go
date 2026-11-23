package otlp

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"go.opentelemetry.io/otel/sdk/instrumentation"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"

	"senhub-agent.go/internal/agent/services/agentstate"
	"senhub-agent.go/internal/agent/services/data_store/otelmapper"
)

var wireProtocols = []string{"grpc", "http"}

func wireTestConfig(col *fakeCollector, dir string) Config {
	cfg := defaultConfig()
	cfg.Protocol = col.protocol
	cfg.Endpoint = col.endpoint
	cfg.TLS.Enabled = false
	cfg.Compression = "gzip"
	cfg.Timeout = 2 * time.Second
	cfg.Metrics.Enabled = true
	cfg.Traces.Enabled = true
	cfg.Persistence.Path = dir
	return cfg
}

// wireRig is one signal's exporter, queue and replayer wired as the strategy
// wires them, with the replay driven by the test instead of a clock.
type wireRig struct {
	t      *testing.T
	col    *fakeCollector
	signal string
	q      *wireQueue
	ch     *wireChannel
	rp     *wireReplayer
	mexp   sdkmetric.Exporter
	sexp   sdktrace.SpanExporter
}

func newWireRig(t *testing.T, col *fakeCollector, dir, signal string, maxBytes int64) *wireRig {
	t.Helper()
	return newWireRigCfg(t, col, dir, signal, maxBytes, nil)
}

func newWireRigCfg(t *testing.T, col *fakeCollector, dir, signal string, maxBytes int64, mutate func(*Config)) *wireRig {
	t.Helper()
	cfg := wireTestConfig(col, dir)
	if mutate != nil {
		mutate(&cfg)
	}
	r := &wireRig{t: t, col: col, signal: signal}
	log := testModuleLogger(t)
	hints := newHintSinks()
	bctx := withHintSinks(context.Background(), hints)
	if signal == "metrics" {
		base, err := buildMetricExporter(bctx, cfg)
		if err != nil {
			t.Fatal(err)
		}
		r.q = newWireQueue(metricsQueueKind, dir, maxBytes, log)
		r.ch = newWireChannel("metrics", r.q, true, false, log)
		r.ch.hints = hints.metrics
		r.mexp = newPersistentMetricExporter(base, r.ch)
	} else {
		base, err := buildTraceExporter(bctx, cfg)
		if err != nil {
			t.Fatal(err)
		}
		r.q = newWireQueue(tracesQueueKind, dir, maxBytes, log)
		r.ch = newWireChannel("traces", r.q, false, true, log)
		r.ch.hints = hints.traces
		r.sexp = newPersistentSpanExporter(base, r.ch)
	}
	r.ch.probeInterval = time.Millisecond
	r.rp = newWireReplayer(r.ch, log)
	t.Cleanup(func() {
		if r.mexp != nil {
			_ = r.mexp.Shutdown(context.Background())
		}
		if r.sexp != nil {
			_ = r.sexp.Shutdown(context.Background())
		}
	})
	return r
}

var wireBase = time.Unix(1_700_000_000, 0)

func (r *wireRig) pushMetric(i int, series int) error {
	return r.pushMetricCtx(context.Background(), i, series)
}

func (r *wireRig) pushMetricCtx(ctx context.Context, i int, series int) error {
	recs := make([]otelmapper.OtelRecord, 0, series)
	for s := 0; s < series; s++ {
		recs = append(recs, otelmapper.OtelRecord{
			Name: "test.gauge", Unit: "1", Type: "gauge", Value: float64(i),
			Attributes: map[string]string{"series": fmt.Sprintf("s-%04d", s), "pad": strings.Repeat("x", 40)},
		})
	}
	rm := buildResourceMetrics(recs, resource.NewSchemaless(), "", wireBase, wireBase.Add(time.Duration(i)*time.Second))
	return r.mexp.Export(ctx, rm)
}

func (r *wireRig) pushSpan(name string) error {
	stub := tracetest.SpanStub{
		Name:                 name,
		StartTime:            wireBase,
		EndTime:              wireBase.Add(time.Second),
		Resource:             resource.NewSchemaless(),
		InstrumentationScope: instrumentation.Scope{Name: "test"},
	}
	return r.sexp.ExportSpans(context.Background(), []sdktrace.ReadOnlySpan{stub.Snapshot()})
}

func (r *wireRig) files() int { return len(r.q.files()) }

func metricTime(i int) int64 { return wireBase.Add(time.Duration(i) * time.Second).UnixNano() }

func droppedBy(reason string) uint64 { return agentstate.GetOTLPDroppedByReason()[reason] }

// An outage, then recovery: the failed pushes stay on disk, nothing is
// sent while the backend is known down, and replay delivers them oldest
// first and ahead of the live push that arrived after them.
func TestMetricsQueue_OutageThenRecovery(t *testing.T) {
	for _, proto := range wireProtocols {
		t.Run(proto, func(t *testing.T) {
			col := newFakeCollector(t, proto)
			rig := newWireRig(t, col, t.TempDir(), "metrics", 0)

			col.setDown(true)
			for i := 1; i <= 5; i++ {
				err := rig.pushMetric(i, 1)
				if err == nil {
					t.Fatalf("push %d: want an error while the backend is down", i)
				}
			}
			if got := rig.files(); got != 5 {
				t.Fatalf("queued files = %d, want 5", got)
			}
			if n := col.attemptCount(); n != 1 {
				t.Errorf("backend attempted %d times while known down, want 1 (ordered queue sends nothing behind a backlog)", n)
			}

			col.setDown(false)
			if err := rig.pushMetric(6, 1); err == nil || !strings.Contains(err.Error(), errExportQueued.Error()) {
				t.Fatalf("live push behind a backlog must be queued, got %v", err)
			}
			if got := len(col.receivedMetrics()); got != 0 {
				t.Fatalf("live push overtook the backlog: %d requests delivered", got)
			}

			n, err := rig.rp.replay()
			if err != nil || n != 6 {
				t.Fatalf("replay = %d, %v; want 6 points", n, err)
			}
			want := []int64{metricTime(1), metricTime(2), metricTime(3), metricTime(4), metricTime(5), metricTime(6)}
			got := col.metricTimes()
			if fmt.Sprint(got) != fmt.Sprint(want) {
				t.Errorf("delivery order = %v, want %v", got, want)
			}
			if rig.files() != 0 {
				t.Errorf("%d files left after a fully acknowledged replay", rig.files())
			}

			if err := rig.pushMetric(7, 1); err != nil {
				t.Fatalf("live push after the backlog drained: %v", err)
			}
			if got := len(col.receivedMetrics()); got != 7 {
				t.Errorf("delivered = %d, want 7", got)
			}
		})
	}
}

// A file leaves the disk only once the backend acknowledged that exact
// request: a replay cut short by a second outage keeps what was not acked.
func TestMetricsQueue_DeletionOnlyAfterAck(t *testing.T) {
	for _, proto := range wireProtocols {
		t.Run(proto, func(t *testing.T) {
			col := newFakeCollector(t, proto)
			rig := newWireRig(t, col, t.TempDir(), "metrics", 0)
			col.setDown(true)
			for i := 1; i <= 5; i++ {
				_ = rig.pushMetric(i, 1)
			}

			col.setDown(false)
			col.acknowledgeOnly(2)
			n, err := rig.rp.replay()
			if err == nil || n != 2 {
				t.Fatalf("replay = %d, %v; want 2 acknowledged then a failure", n, err)
			}
			if got := rig.files(); got != 3 {
				t.Fatalf("files after a partial replay = %d, want 3 (the unacknowledged ones)", got)
			}

			col.acknowledgeOnly(-1)
			rig.ch.healthy.Store(true)
			if n, err := rig.rp.replay(); err != nil || n != 3 {
				t.Fatalf("second replay = %d, %v; want 3", n, err)
			}
			want := []int64{metricTime(1), metricTime(2), metricTime(3), metricTime(4), metricTime(5)}
			if got := col.metricTimes(); fmt.Sprint(got) != fmt.Sprint(want) {
				t.Errorf("delivered %v, want each exactly once in order %v", got, want)
			}
		})
	}
}

// The agent restarts during the outage: a new instance finds the files and
// replays them.
func TestMetricsQueue_RestartDuringOutage(t *testing.T) {
	for _, proto := range wireProtocols {
		t.Run(proto, func(t *testing.T) {
			col := newFakeCollector(t, proto)
			dir := t.TempDir()
			first := newWireRig(t, col, dir, "metrics", 0)
			col.setDown(true)
			for i := 1; i <= 3; i++ {
				_ = first.pushMetric(i, 3)
			}
			_ = first.mexp.Shutdown(context.Background())

			second := newWireRig(t, col, dir, "metrics", 0)
			if recs, _ := second.q.pending(); recs != 9 {
				t.Fatalf("recovered %d data points, want 9", recs)
			}
			col.setDown(false)
			if n, err := second.rp.replay(); err != nil || n != 9 {
				t.Fatalf("replay on the new instance = %d, %v", n, err)
			}
			want := []int64{metricTime(1), metricTime(2), metricTime(3)}
			if got := col.metricTimes(); fmt.Sprint(got) != fmt.Sprint(want) {
				t.Errorf("delivered %v, want %v", got, want)
			}
			if second.files() != 0 {
				t.Errorf("files left: %d", second.files())
			}
		})
	}
}

func TestTracesQueue_OutageRecoveryAndRestart(t *testing.T) {
	for _, proto := range wireProtocols {
		t.Run(proto, func(t *testing.T) {
			col := newFakeCollector(t, proto)
			dir := t.TempDir()
			first := newWireRig(t, col, dir, "traces", 0)
			first.ch.probeInterval = time.Hour
			col.setDown(true)
			if err := first.pushSpan("span-1"); err == nil {
				t.Fatal("the export that finds the backend down must report it")
			}
			for _, name := range []string{"span-2", "span-3"} {
				if err := first.pushSpan(name); err != nil {
					t.Fatalf("%s: kept on disk, the span processor has nothing to report: %v", name, err)
				}
			}
			if first.files() != 3 {
				t.Fatalf("queued = %d, want 3", first.files())
			}
			if n := col.attemptCount(); n != 1 {
				t.Errorf("backend attempted %d times while known down, want 1", n)
			}

			second := newWireRig(t, col, dir, "traces", 0)
			if recs, _ := second.q.pending(); recs != 3 {
				t.Fatalf("recovered %d spans, want 3", recs)
			}
			col.acknowledgeOnly(1)
			col.setDown(false)
			if n, err := second.rp.replay(); err == nil || n != 1 {
				t.Fatalf("replay = %d, %v; want one ack then a failure", n, err)
			}
			if second.files() != 2 {
				t.Fatalf("files after a cut replay = %d, want 2", second.files())
			}
			col.acknowledgeOnly(-1)
			second.ch.healthy.Store(true)
			if n, err := second.rp.replay(); err != nil || n != 2 {
				t.Fatalf("replay = %d, %v; want 2", n, err)
			}
			if got := col.spanNames(); fmt.Sprint(got) != "[span-1 span-2 span-3]" {
				t.Errorf("delivered %v, want the three spans once each, in order", got)
			}
		})
	}
}

// The size cap evicts the oldest requests and counts them, in data points,
// under the signal's own reason.
func TestWireQueue_SizeCapEvictsOldestAndCounts(t *testing.T) {
	col := newFakeCollector(t, "grpc")
	rig := newWireRig(t, col, t.TempDir(), "metrics", 6<<10)
	col.setDown(true)
	before := droppedBy("metrics_queue_full")
	const pushes, series = 40, 20
	for i := 1; i <= pushes; i++ {
		_ = rig.pushMetric(i, series)
	}
	dropped := droppedBy("metrics_queue_full") - before
	if dropped == 0 {
		t.Fatal("nothing evicted past a 6 KiB cap")
	}
	if dropped%series != 0 {
		t.Errorf("dropped = %d data points, want a multiple of %d (whole requests)", dropped, series)
	}
	left, _ := rig.q.pending()
	if uint64(left)+dropped != pushes*series {
		t.Errorf("queued %d + dropped %d != produced %d", left, dropped, pushes*series)
	}
	var size int64
	for _, name := range rig.q.files() {
		info, err := os.Stat(filepath.Join(rig.q.dir, name))
		if err != nil {
			t.Fatal(err)
		}
		size += info.Size()
	}
	if size > 6<<10 {
		t.Errorf("queue holds %d bytes, over the cap", size)
	}

	col.setDown(false)
	rig.ch.healthy.Store(true)
	if _, err := rig.rp.replay(); err != nil {
		t.Fatal(err)
	}
	got := col.metricTimes()
	if len(got) == 0 || got[len(got)-1] != metricTime(pushes) {
		t.Errorf("the newest request must survive eviction, delivered %v", got)
	}
	if got[0] == metricTime(1) {
		t.Errorf("the oldest request must have been evicted first")
	}
}

func TestWireQueue_AgeCapCountsUnderItsOwnReason(t *testing.T) {
	for _, tc := range []struct {
		kind   wireQueueKind
		reason string
	}{{metricsQueueKind, "metrics_dropped_by_age"}, {tracesQueueKind, "traces_dropped_by_age"}} {
		t.Run(tc.kind.signal, func(t *testing.T) {
			q := newWireQueue(tc.kind, t.TempDir(), 0, testModuleLogger(t))
			clock := &fakeClock{t: time.Now()}
			q.now = clock.Now
			q.setMaxAge(24 * time.Hour)
			if err := q.enqueue([]byte("old-request"), 7); err != nil {
				t.Fatal(err)
			}
			clock.Advance(23 * time.Hour)
			if err := q.enqueue([]byte("young-request"), 3); err != nil {
				t.Fatal(err)
			}
			clock.Advance(2 * time.Hour)
			before := droppedBy(tc.reason)
			if got := q.sweepAged(); got != 7 {
				t.Fatalf("swept %d units, want 7", got)
			}
			if got := droppedBy(tc.reason) - before; got != 7 {
				t.Errorf("%s counted %d, want 7", tc.reason, got)
			}
			if recs, _ := q.pending(); recs != 3 {
				t.Errorf("pending = %d, want the young request's 3", recs)
			}
		})
	}
}

// A file written by the logs queue before this change is still read.
func TestLogsQueue_LegacyFileStillRead(t *testing.T) {
	dir := t.TempDir()
	qdir := filepath.Join(dir, logsQueueDirName)
	if err := os.MkdirAll(qdir, 0o750); err != nil {
		t.Fatal(err)
	}
	legacy := `{"version":1,"saved_at":"2026-01-02T03:04:05.000000006Z","records":[` +
		`{"ts":1767323045000000000,"sev":9,"sev_text":"INFO","body":"first","attrs":{"k":"v"}},` +
		`{"ts":1767323046000000000,"obs_ts":1767323046000000001,"sev":17,"body":"second"}]}`
	if err := os.WriteFile(filepath.Join(qdir, "log-00000000000000000007.json"), []byte(legacy), 0o640); err != nil {
		t.Fatal(err)
	}

	q := newLogsQueue(dir, 0, testModuleLogger(t))
	if recs, _ := q.pending(); recs != 2 {
		t.Fatalf("recovered %d records, want 2", recs)
	}
	if err := q.enqueue([]persistedLogRecord{{TimestampUnixNano: 1, Body: "new"}}); err != nil {
		t.Fatal(err)
	}
	if names := q.files(); len(names) != 2 || names[0] != "log-00000000000000000007.json" || names[1] != "log-00000000000000000008.json" {
		t.Fatalf("file names = %v; sequence must continue after the legacy file", names)
	}
	var bodies []string
	n, err := q.drainAcked(func(recs []persistedLogRecord) error {
		for _, r := range recs {
			bodies = append(bodies, r.Body)
		}
		return nil
	})
	if err != nil || n != 3 || fmt.Sprint(bodies) != "[first second new]" {
		t.Errorf("drain = %d, %v, %v", n, err, bodies)
	}
}

// A queue file from a newer release, or a corrupt one, is dropped rather
// than wedging the replay.
func TestWireQueue_UnreadableFileDoesNotWedge(t *testing.T) {
	q := newWireQueue(metricsQueueKind, t.TempDir(), 0, testModuleLogger(t))
	if err := os.MkdirAll(q.dir, 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(q.dir, "metric-00000000000000000001.q"), []byte("garbage-not-a-queue-file"), 0o640); err != nil {
		t.Fatal(err)
	}
	q.recover()
	if err := q.enqueue([]byte("good"), 2); err != nil {
		t.Fatal(err)
	}
	var got []string
	n, err := q.drainAcked(func(req []byte, _ int) error { got = append(got, string(req)); return nil })
	if err != nil || n != 2 || fmt.Sprint(got) != "[good]" {
		t.Errorf("drain = %d, %v, %v", n, err, got)
	}
	if len(q.files()) != 0 {
		t.Errorf("files left: %v", q.files())
	}
}

// A receiver that refuses a request on its merits does not get it queued.
func TestMetricsQueue_RefusedRequestIsNotQueued(t *testing.T) {
	for _, proto := range wireProtocols {
		t.Run(proto, func(t *testing.T) {
			col := newFakeCollector(t, proto)
			rig := newWireRig(t, col, t.TempDir(), "metrics", 0)
			col.setReject(true)
			before := droppedBy("receiver_rejected")
			if err := rig.pushMetric(1, 2); err == nil {
				t.Fatal("want the refusal back")
			}
			if rig.files() != 0 {
				t.Errorf("a refused request was queued")
			}
			if got := droppedBy("receiver_rejected") - before; got != 2 {
				t.Errorf("receiver_rejected counted %d, want 2 points", got)
			}
			if rig.ch.backendDown() {
				t.Errorf("a refusal says the backend answers; it must not be marked down")
			}
		})
	}
}
