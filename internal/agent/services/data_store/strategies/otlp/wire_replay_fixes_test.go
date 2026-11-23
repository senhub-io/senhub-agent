package otlp

import (
	"context"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
	"testing"
)

// An exporter that answers success without sending (an empty batch it
// skips) must not get a stored request acknowledged.
type silentExporter struct{ sdkmetric.Exporter }

func (silentExporter) Temporality(sdkmetric.InstrumentKind) metricdata.Temporality {
	return metricdata.CumulativeTemporality
}
func (silentExporter) Aggregation(sdkmetric.InstrumentKind) sdkmetric.Aggregation {
	return sdkmetric.AggregationDefault{}
}
func (silentExporter) Export(context.Context, *metricdata.ResourceMetrics) error { return nil }
func (silentExporter) ForceFlush(context.Context) error                          { return nil }
func (silentExporter) Shutdown(context.Context) error                            { return nil }

func TestMetricsReplay_SilentSuccessIsNotAnAck(t *testing.T) {
	q := newWireQueue(metricsQueueKind, t.TempDir(), 0, testModuleLogger(t))
	ch := newWireChannel("metrics", q, true, false, testModuleLogger(t))
	newPersistentMetricExporter(silentExporter{}, ch)
	rp := newWireReplayer(ch, testModuleLogger(t))
	if err := q.enqueue([]byte("stored"), 3); err != nil {
		t.Fatal(err)
	}
	if n, err := rp.replay(); err == nil || n != 0 {
		t.Fatalf("replay = %d, %v; want a failure", n, err)
	}
	if len(q.files()) != 1 {
		t.Error("the stored request was deleted although nothing was sent")
	}
}

func TestHTTPReplay_FollowsConfiguredCompression(t *testing.T) {
	for _, tc := range []struct{ compression, want string }{{"none", ""}, {"gzip", "gzip"}} {
		t.Run(tc.compression, func(t *testing.T) {
			col := newFakeCollector(t, "http")
			rig := newWireRigCfg(t, col, t.TempDir(), "metrics", 0, func(c *Config) { c.Compression = tc.compression })
			col.setDown(true)
			_ = rig.pushMetric(1, 1)
			col.setDown(false)
			col.mu.Lock()
			col.encodings = nil
			col.mu.Unlock()
			rig.ch.healthy.Store(true)
			if n, err := rig.rp.replay(); err != nil || n != 1 {
				t.Fatalf("replay = %d, %v", n, err)
			}
			col.mu.Lock()
			defer col.mu.Unlock()
			if len(col.encodings) != 1 || col.encodings[0] != tc.want {
				t.Errorf("Content-Encoding on replay = %q, want %q", col.encodings, tc.want)
			}
		})
	}
}

// An export whose context is already cancelled still reaches the disk.
func TestMetricsQueue_CancelledExportIsStillKept(t *testing.T) {
	for _, proto := range wireProtocols {
		t.Run(proto, func(t *testing.T) {
			col := newFakeCollector(t, proto)
			rig := newWireRig(t, col, t.TempDir(), "metrics", 0)
			ctx, cancel := context.WithCancel(context.Background())
			cancel()
			if err := rig.pushMetricCtx(ctx, 1, 1); err == nil {
				t.Fatal("want an error")
			}
			if rig.files() != 1 {
				t.Errorf("files = %d, want 1: a cancelled export must still be kept", rig.files())
			}
		})
	}
}

// Replay discards only what the receiver refuses on its merits; an outage keeps the file.
func TestWireReplay_DiscardsRefusalOnly(t *testing.T) {
	for _, proto := range wireProtocols {
		t.Run(proto, func(t *testing.T) {
			col := newFakeCollector(t, proto)
			rig := newWireRig(t, col, t.TempDir(), "metrics", 0)
			col.setDown(true)
			_ = rig.pushMetric(1, 1)

			rig.ch.healthy.Store(true)
			if _, err := rig.rp.replay(); err == nil || rig.files() != 1 {
				t.Fatalf("an outage during replay must keep the file: err=%v files=%d", err, rig.files())
			}
			col.setDown(false)
			col.setReject(true)
			rig.ch.healthy.Store(true)
			before := droppedBy("receiver_rejected")
			_, _ = rig.rp.replay()
			if rig.files() != 0 || droppedBy("receiver_rejected")-before != 1 {
				t.Errorf("a refusal must discard and count: files=%d", rig.files())
			}
		})
	}
}
