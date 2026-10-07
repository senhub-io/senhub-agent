package otlp

import (
	"context"
	"testing"
	"time"

	sdklog "go.opentelemetry.io/otel/sdk/log"
	"go.opentelemetry.io/otel/sdk/resource"

	"senhub-agent.go/internal/agent/services/agentstate"
)

func TestLogsExportConfig_QueueOnDisablesRetryAndCapsTimeout(t *testing.T) {
	cfg := Config{Timeout: DefaultTimeout}
	cfg.Retry = RetryConfig{Enabled: true, InitialInterval: DefaultRetryInitial, MaxInterval: DefaultRetryMax, MaxElapsedTime: DefaultRetryMaxElapsed}
	cfg.Logs.Enabled = true
	cfg.Persistence = PersistenceConfig{Enabled: true, Path: t.TempDir()}

	got := logsExportConfig(cfg)
	if got.Retry.Enabled {
		t.Error("the SDK retry chain must be off when the queue is the retry mechanism")
	}
	if got.Timeout != logsQueueExportTimeout {
		t.Errorf("timeout=%v, want %v", got.Timeout, logsQueueExportTimeout)
	}
	if !cfg.Retry.Enabled || cfg.Timeout != DefaultTimeout {
		t.Error("the caller's config must not be modified")
	}

	cfg.Persistence.Enabled = false
	if got := logsExportConfig(cfg); !got.Retry.Enabled || got.Timeout != DefaultTimeout {
		t.Error("without the queue the SDK retry and timeout stay as configured")
	}
}

// The in-memory loss window on a kill -9 is the batch interval plus one
// export timeout: a collector that never answers must not hold a batch in
// memory longer than that before it is on disk.
func TestLogsQueueAck_HungCollectorBatchReachesDiskAtTimeout(t *testing.T) {
	dir := t.TempDir()
	q := newLogsQueue(dir, 0, testModuleLogger(t))
	ple := newPersistentLogExporter(hangingExporter{}, q, testModuleLogger(t))
	timed := &timeoutExporter{inner: ple, timeout: 150 * time.Millisecond}
	cfg := LogsSignal{BufferSize: 100, BatchSize: 1, BatchTimeout: time.Hour}
	pipe := buildLogsPipeline(timed, resource.NewSchemaless(), cfg, "test")

	start := time.Now()
	pipe.emit(context.Background(), agentstate.LogRecord{Timestamp: time.Now(), Severity: 9, Body: "x", ProducerProbeName: "syslog"})
	_ = pipe.provider.ForceFlush(context.Background())
	if elapsed := time.Since(start); elapsed > 2*time.Second {
		t.Errorf("batch took %v to be settled", elapsed)
	}
	if n, _ := q.pending(); n != 1 {
		t.Errorf("pending=%d, want the timed-out batch on disk", n)
	}
}

// hangingExporter blocks until its context ends, like a collector that
// drops packets.
type hangingExporter struct{}

func (hangingExporter) Export(ctx context.Context, _ []sdklog.Record) error {
	<-ctx.Done()
	return ctx.Err()
}
func (hangingExporter) ForceFlush(context.Context) error { return nil }
func (hangingExporter) Shutdown(context.Context) error   { return nil }

// timeoutExporter stands in for the transport's own per-export timeout.
type timeoutExporter struct {
	inner   sdklog.Exporter
	timeout time.Duration
}

func (e *timeoutExporter) Export(ctx context.Context, r []sdklog.Record) error {
	ctx, cancel := context.WithTimeout(ctx, e.timeout)
	defer cancel()
	return e.inner.Export(ctx, r)
}
func (e *timeoutExporter) ForceFlush(ctx context.Context) error { return e.inner.ForceFlush(ctx) }
func (e *timeoutExporter) Shutdown(ctx context.Context) error   { return e.inner.Shutdown(ctx) }
