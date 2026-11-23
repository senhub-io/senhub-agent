package otlp

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"go.opentelemetry.io/otel"

	"senhub-agent.go/internal/agent/services/agentstate"
	"senhub-agent.go/internal/agent/services/data_store/transformers"
	"senhub-agent.go/internal/agent/types/datapoint"
	"senhub-agent.go/probesdk/tags"
)

func queueStrategyParams(col *fakeCollector, dir string) map[string]interface{} {
	return map[string]interface{}{
		"endpoint":    col.endpoint,
		"protocol":    col.protocol,
		"compression": "none",
		"timeout":     "2s",
		"tls":         map[string]interface{}{"enabled": false},
		"persistence": map[string]interface{}{"enabled": true, "path": dir},
		"signals": map[string]interface{}{
			"metrics":  map[string]interface{}{"enabled": true, "interval": "1h"},
			"logs":     map[string]interface{}{"enabled": false},
			"entities": map[string]interface{}{"enabled": false},
			"traces":   map[string]interface{}{"enabled": true, "sample_ratio": 1.0, "batch_timeout": "1h"},
		},
	}
}

func startQueueStrategy(t *testing.T, col *fakeCollector, dir string) *OTLPSyncStrategy {
	t.Helper()
	s := newTestStrategy(t, queueStrategyParams(col, dir))
	s.registry = &fakeDefs{defs: map[string]*transformers.ProbeDefinition{"cpu": newCounterDef()}}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := s.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}
	// The relays drain the process-wide receiver rails, which other tests
	// publish to; this test is about the strategy's own exports.
	if s.metricsRelay != nil {
		s.metricsRelay.stop(ctx)
	}
	if s.spansRelay != nil {
		s.spansRelay.stop(ctx)
	}
	return s
}

func cpuPoint() datapoint.DataPoint {
	return datapoint.DataPoint{
		Name: "cpu_user_time", Timestamp: time.Now(), Value: 12,
		Tags: []tags.Tag{{Key: "probe_name", Value: "cpu"}, {Key: "probe_type", Value: "cpu"}},
	}
}

func eventually(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", what)
}

func countFiles(t *testing.T, dir, sub string) int {
	t.Helper()
	entries, err := os.ReadDir(filepath.Join(dir, sub))
	if err != nil {
		return 0
	}
	n := 0
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), wireQueueExt) {
			n++
		}
	}
	return n
}

// A graceful stop while the backend is down writes the store's current
// values to the queue instead of dropping them; the next start replays them.
func TestMetricsQueue_StopWhileDownPersistsStoreThenNextStartReplays(t *testing.T) {
	for _, proto := range wireProtocols {
		t.Run(proto, func(t *testing.T) {
			col := newFakeCollector(t, proto)
			dir := t.TempDir()
			col.setDown(true)

			s := startQueueStrategy(t, col, dir)
			if s.metricsChan == nil {
				t.Fatal("metrics queue not active")
			}
			if err := s.AddDataPoints([]datapoint.DataPoint{cpuPoint()}); err != nil {
				t.Fatal(err)
			}
			if err := s.Shutdown(context.Background()); err != nil {
				t.Fatalf("Shutdown: %v", err)
			}
			if got := countFiles(t, dir, metricsQueueDirName); got == 0 {
				t.Fatal("nothing on disk after a stop with the backend down")
			}
			if got := len(col.receivedMetrics()); got != 0 {
				t.Fatalf("backend acknowledged %d requests while down", got)
			}

			col.setDown(false)
			s2 := startQueueStrategy(t, col, dir)
			defer func() { _ = s2.Shutdown(context.Background()) }()
			eventually(t, "the boot replay", func() bool { return len(col.receivedMetrics()) > 0 })
			eventually(t, "the queue to empty", func() bool { return countFiles(t, dir, metricsQueueDirName) == 0 })
		})
	}
}

func TestTracesQueue_StopWhileDownPersistsSpansThenNextStartReplays(t *testing.T) {
	for _, proto := range wireProtocols {
		t.Run(proto, func(t *testing.T) {
			col := newFakeCollector(t, proto)
			dir := t.TempDir()
			col.setDown(true)

			s := startQueueStrategy(t, col, dir)
			if s.tracesChan == nil || s.traces == nil {
				t.Fatal("traces queue not active")
			}
			_, span := s.traces.tracer.Start(context.Background(), "queued-while-down")
			span.End()
			if err := s.Shutdown(context.Background()); err != nil {
				t.Fatalf("Shutdown: %v", err)
			}
			if got := countFiles(t, dir, tracesQueueDirName); got == 0 {
				t.Fatal("no span on disk after a stop with the backend down")
			}

			col.setDown(false)
			s2 := startQueueStrategy(t, col, dir)
			defer func() { _ = s2.Shutdown(context.Background()) }()
			eventually(t, "the span to be replayed", func() bool {
				for _, req := range col.receivedTraces() {
					for _, rs := range req.GetResourceSpans() {
						for _, ss := range rs.GetScopeSpans() {
							for _, sp := range ss.GetSpans() {
								if sp.GetName() == "queued-while-down" {
									return true
								}
							}
						}
					}
				}
				return false
			})
		})
	}
	otel.SetTracerProvider(otel.GetTracerProvider())
}

type lockedBuffer struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (l *lockedBuffer) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.b.Write(p)
}

func (l *lockedBuffer) String() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.b.String()
}

// A state directory the service cannot write turns every queue off with one
// warning, and the signals keep their SDK retry.
func TestQueues_UnwritableDirOneWarningNoQueue(t *testing.T) {
	clearQueueEnv(t)
	blocker := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(blocker, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("STATE_DIRECTORY", blocker)

	params := queueStrategyParams(&fakeCollector{protocol: "grpc", endpoint: "127.0.0.1:65000"}, "")
	params["persistence"] = map[string]interface{}{"enabled": true}
	params["signals"].(map[string]interface{})["logs"] = map[string]interface{}{"enabled": true}

	var buf lockedBuffer
	zl := zerolog.New(&buf)
	s, ok := NewOTLPSyncStrategy(&fakeAgentConfig{key: "test-key-12345678-abcdef"}, params, &zl).(*OTLPSyncStrategy)
	if !ok {
		t.Fatal("constructor")
	}
	if err := s.Start(context.Background()); err != nil {
		t.Fatalf("Start must not fail: %v", err)
	}
	defer func() { _ = s.Shutdown(context.Background()) }()

	if s.logsQueue != nil || s.metricsChan != nil || s.tracesChan != nil {
		t.Error("a queue is active on an unwritable directory")
	}
	if n := strings.Count(buf.String(), "OTLP queues disabled"); n != 1 {
		t.Errorf("warning count = %d, want exactly 1\n%s", n, buf.String())
	}
	if cfg := metricsExportConfig(func() Config { c := s.cfg; c.Persistence.Enabled = false; return c }()); !cfg.Retry.Enabled {
		t.Error("a signal without a queue must keep its SDK retry")
	}
}

// A relayed batch the collector did not take is kept with the application's
// own Resource and replayed through the exporter's transport unchanged.
func TestMetricsRelay_FailedBatchKeptAndReplayedWithItsResource(t *testing.T) {
	for _, proto := range wireProtocols {
		t.Run(proto, func(t *testing.T) {
			col := newFakeCollector(t, proto)
			rig := newWireRig(t, col, t.TempDir(), "metrics", 0)

			cfg := relayTestConfig(col.endpoint, proto)
			cfg.Compression = "none"
			relay, err := newMetricsRelay(cfg, nil, testModuleLogger(t))
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = relay.forwarder.close() }()
			relay.queue = rig.ch

			col.setDown(true)
			relay.export(relayMetricBatch("checkout-api", "app.requests"), 1)
			relay.export(relayMetricBatch("checkout-api", "app.requests"), 1)
			if rig.files() != 2 {
				t.Fatalf("kept %d relayed batches, want 2", rig.files())
			}
			if n := col.attemptCount(); n != 1 {
				t.Errorf("backend attempted %d times, want 1 (the second batch skips a known-down backend)", n)
			}

			col.setDown(false)
			if n, err := rig.rp.replay(); err != nil || n != 2 {
				t.Fatalf("replay = %d, %v", n, err)
			}
			for _, req := range col.receivedMetrics() {
				got := ""
				for _, kv := range req.GetResourceMetrics()[0].GetResource().GetAttributes() {
					if kv.GetKey() == "service.name" {
						got = kv.GetValue().GetStringValue()
					}
				}
				if got != "checkout-api" {
					t.Errorf("replayed request carries service.name %q, want the application's own", got)
				}
			}
		})
	}
}

func TestSpansRelay_FailedBatchKeptAndReplayed(t *testing.T) {
	col := newFakeCollector(t, "grpc")
	rig := newWireRig(t, col, t.TempDir(), "traces", 0)
	relay, err := newSpansRelay(relayTestConfig(col.endpoint, "grpc"), nil, testModuleLogger(t))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = relay.forwarder.close() }()
	relay.queue = rig.ch

	col.setDown(true)
	relay.export(relaySpanBatch("relayed-span"), 1)
	if rig.files() != 1 {
		t.Fatalf("kept %d relayed batches, want 1", rig.files())
	}
	col.setDown(false)
	if n, err := rig.rp.replay(); err != nil || n != 1 {
		t.Fatalf("replay = %d, %v", n, err)
	}
	if got := col.spanNames(); len(got) != 1 || got[0] != "relayed-span" {
		t.Errorf("delivered %v", got)
	}
}

// With endpoint failover the dry encoding pass must not put an endpoint in
// cooldown, and a replay goes to whichever endpoint is up.
func TestMetricsQueue_ReplayFollowsFailover(t *testing.T) {
	primary := newFakeCollector(t, "grpc")
	standby := newFakeCollector(t, "grpc")
	dir := t.TempDir()
	cfg := wireTestConfig(primary, dir)
	cfg.FallbackEndpoints = []string{standby.endpoint}
	cfg.FailoverCooldown = time.Hour
	exp, err := buildFailoverMetricExporter(context.Background(), cfg, testModuleLogger(t))
	if err != nil {
		t.Fatal(err)
	}
	q := newWireQueue(metricsQueueKind, dir, 0, testModuleLogger(t))
	ch := newWireChannel("metrics", q, true, false, testModuleLogger(t))
	rig := &wireRig{t: t, col: primary, signal: "metrics", q: q, ch: ch, mexp: newPersistentMetricExporter(exp, ch)}
	rig.rp = newWireReplayer(ch, testModuleLogger(t))
	t.Cleanup(func() { _ = rig.mexp.Shutdown(context.Background()) })

	// Both down: the request is kept and no endpoint was marked down by the
	// encoding pass of the second push.
	primary.setDown(true)
	standby.setDown(true)
	_ = rig.pushMetric(1, 1)
	_ = rig.pushMetric(2, 1)
	if rig.files() != 2 {
		t.Fatalf("files = %d, want 2", rig.files())
	}

	standby.setDown(false)
	ch.healthy.Store(true)
	if n, err := rig.rp.replay(); err != nil || n != 2 {
		t.Fatalf("replay = %d, %v; want both delivered to the standby", n, err)
	}
	if got := len(standby.receivedMetrics()); got != 2 {
		t.Errorf("standby received %d, want 2", got)
	}
	if got := len(primary.receivedMetrics()); got != 0 {
		t.Errorf("primary received %d while down", got)
	}
	_ = agentstate.GetOTLPQueueStats("metrics")
}

func TestWireQueueConfig_KeysAndEnv(t *testing.T) {
	clearQueueEnv(t)
	for _, k := range []string{"SENHUB_METRIC_QUEUE", "SENHUB_METRIC_QUEUE_RETENTION", "SENHUB_METRIC_QUEUE_MAX_BYTES",
		"SENHUB_TRACE_QUEUE", "SENHUB_TRACE_QUEUE_RETENTION", "SENHUB_TRACE_QUEUE_MAX_BYTES"} {
		t.Setenv(k, "")
	}
	cfg := parseWith(t, nil)
	p := cfg.Persistence
	if !p.MetricsQueueEnabled || !p.TracesQueueEnabled || p.MetricsQueueMaxAge != 24*time.Hour || p.TracesQueueMaxAge != 24*time.Hour ||
		p.MetricsQueueMaxBytes != 0 || p.TracesQueueMaxBytes != 0 {
		t.Fatalf("defaults: %+v", p)
	}
	if p.metricsQueuePath() == "" || p.tracesQueuePath() == "" {
		t.Error("queues must be on by default")
	}

	cfg = parseWith(t, map[string]interface{}{"persistence": map[string]interface{}{
		"metrics_queue_max_bytes": 1024, "metrics_queue_max_age": "2h",
		"traces_queue_max_bytes": 2048, "traces_queue_max_age": "0",
	}})
	p = cfg.Persistence
	if p.MetricsQueueMaxBytes != 1024 || p.MetricsQueueMaxAge != 2*time.Hour || p.TracesQueueMaxBytes != 2048 || p.TracesQueueMaxAge != 0 {
		t.Errorf("file keys not read: %+v", p)
	}

	t.Setenv("SENHUB_METRIC_QUEUE", "false")
	t.Setenv("SENHUB_METRIC_QUEUE_RETENTION", "3h")
	t.Setenv("SENHUB_METRIC_QUEUE_MAX_BYTES", "8MiB")
	t.Setenv("SENHUB_TRACE_QUEUE_MAX_BYTES", "1MiB")
	cfg = parseWith(t, map[string]interface{}{"persistence": map[string]interface{}{"metrics_queue_max_bytes": 1}})
	p = cfg.Persistence
	if p.metricsQueuePath() != "" || p.tracesQueuePath() == "" {
		t.Errorf("SENHUB_METRIC_QUEUE=false must switch off the metrics queue only: %q %q", p.metricsQueuePath(), p.tracesQueuePath())
	}
	if p.MetricsQueueMaxAge != 3*time.Hour || p.MetricsQueueMaxBytes != 8<<20 || p.TracesQueueMaxBytes != 1<<20 {
		t.Errorf("env did not win: %+v", p)
	}

	disabled := parseWith(t, map[string]interface{}{"persistence": map[string]interface{}{"enabled": false}}).Persistence
	if disabled.metricsQueuePath() != "" || disabled.tracesQueuePath() != "" {
		t.Error("persistence.enabled=false must switch every queue off")
	}
}

func TestWireQueueConfig_InvalidRejected(t *testing.T) {
	for k, v := range map[string]string{
		"SENHUB_METRIC_QUEUE": "maybe", "SENHUB_METRIC_QUEUE_RETENTION": "soon", "SENHUB_METRIC_QUEUE_MAX_BYTES": "big",
		"SENHUB_TRACE_QUEUE": "maybe", "SENHUB_TRACE_QUEUE_RETENTION": "soon", "SENHUB_TRACE_QUEUE_MAX_BYTES": "big",
	} {
		t.Run(k, func(t *testing.T) {
			clearQueueEnv(t)
			t.Setenv(k, v)
			if _, err := ParseConfig(map[string]interface{}{"endpoint": "x:1"}); err == nil || !strings.Contains(err.Error(), k) {
				t.Errorf("want error naming %s, got %v", k, err)
			}
		})
	}
	if _, err := ParseConfig(map[string]interface{}{"endpoint": "x:1", "persistence": map[string]interface{}{"traces_queue_max_age": "-1h"}}); err == nil {
		t.Error("a negative retention must be rejected")
	}
}
