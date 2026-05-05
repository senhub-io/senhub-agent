package otlp

import (
	"context"
	"sync"
	"testing"
	"time"

	"go.opentelemetry.io/otel/log"
	sdklog "go.opentelemetry.io/otel/sdk/log"
	"go.opentelemetry.io/otel/sdk/resource"

	"senhub-agent.go/internal/agent/services/agentstate"
)

// recordingExporter captures everything the SDK BatchProcessor pushes,
// so the test can assert on attribute-level details without standing
// up an OTLP collector.
type recordingExporter struct {
	mu      sync.Mutex
	records []sdklog.Record
}

func (r *recordingExporter) Export(_ context.Context, recs []sdklog.Record) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.records = append(r.records, recs...)
	return nil
}
func (*recordingExporter) Shutdown(context.Context) error   { return nil }
func (*recordingExporter) ForceFlush(context.Context) error { return nil }

func (r *recordingExporter) snapshot() []sdklog.Record {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]sdklog.Record, len(r.records))
	copy(out, r.records)
	return out
}

func newTestLogsPipeline(t *testing.T, batchSize, bufferSize int, batchTimeout time.Duration) (*logsPipeline, *recordingExporter) {
	t.Helper()
	exp := &recordingExporter{}
	cfg := LogsSignal{
		Enabled:      true,
		BatchSize:    batchSize,
		BatchTimeout: batchTimeout,
		BufferSize:   bufferSize,
	}
	return buildLogsPipeline(exp, resource.NewSchemaless(), cfg, "1.0.0"), exp
}

func TestLogsPipeline_EmitFlushesViaBatchProcessor(t *testing.T) {
	pipe, exp := newTestLogsPipeline(t, 10, 1024, 10*time.Millisecond)
	defer pipe.shutdown(context.Background())

	pipe.emit(context.Background(), agentstate.LogRecord{
		Timestamp:    time.Unix(1700000000, 0),
		Severity:     agentstate.LogSeverityWarn,
		SeverityText: "WARN",
		Body:         "thing broke",
		Attributes:   map[string]string{"syslog.facility": "16"},
	})

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if len(exp.snapshot()) > 0 {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	got := exp.snapshot()
	if len(got) != 1 {
		t.Fatalf("got %d records, want 1", len(got))
	}
	rec := got[0]
	if rec.Severity() != log.SeverityWarn1 {
		t.Errorf("Severity=%v, want SeverityWarn1", rec.Severity())
	}
	if rec.SeverityText() != "WARN" {
		t.Errorf("SeverityText=%q", rec.SeverityText())
	}
	body := rec.Body()
	if body.AsString() != "thing broke" {
		t.Errorf("Body=%q", body.AsString())
	}

	// Resource should be attached automatically by the SDK Logger.
	if rec.Resource() == nil {
		t.Errorf("Resource not attached")
	}
}

func TestLogsPipeline_AttributesAndProducerIdentity(t *testing.T) {
	pipe, exp := newTestLogsPipeline(t, 10, 1024, 10*time.Millisecond)
	defer pipe.shutdown(context.Background())

	pipe.emit(context.Background(), agentstate.LogRecord{
		Timestamp:         time.Now(),
		Severity:          agentstate.LogSeverityInfo,
		Body:              "msg",
		Attributes:        map[string]string{"syslog.appname": "sshd"},
		ProducerProbeName: "syslog-prod",
		ProducerProbeType: "syslog",
	})

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if len(exp.snapshot()) > 0 {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	got := exp.snapshot()
	if len(got) != 1 {
		t.Fatalf("len=%d", len(got))
	}

	collected := map[string]string{}
	got[0].WalkAttributes(func(kv log.KeyValue) bool {
		collected[string(kv.Key)] = kv.Value.AsString()
		return true
	})

	for _, expected := range []struct {
		key, val string
	}{
		{"syslog.appname", "sshd"},
		{"senhub.probe.name", "syslog-prod"},
		{"senhub.probe.type", "syslog"},
	} {
		if collected[expected.key] != expected.val {
			t.Errorf("attr %q=%q, want %q", expected.key, collected[expected.key], expected.val)
		}
	}
}

func TestLogsPump_StartStopIsCleanWithoutLogs(t *testing.T) {
	agentstate.UnsubscribeLogs(nil) // no-op safety
	defer resetLogChannel()

	pipe, _ := newTestLogsPipeline(t, 10, 32, 10*time.Millisecond)
	defer pipe.shutdown(context.Background())

	pump := newLogsPump(pipe, 16)
	pump.start()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	pump.stop(ctx)
}

func TestLogsPump_DrainsAgentstateChannelToExporter(t *testing.T) {
	defer resetLogChannel()

	pipe, exp := newTestLogsPipeline(t, 10, 1024, 10*time.Millisecond)
	defer pipe.shutdown(context.Background())

	pump := newLogsPump(pipe, 64)
	pump.start()
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		pump.stop(ctx)
	}()

	for i := 0; i < 5; i++ {
		agentstate.PublishLog(agentstate.LogRecord{
			Timestamp: time.Now(),
			Severity:  agentstate.LogSeverityInfo,
			Body:      "msg",
		})
	}

	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if len(exp.snapshot()) >= 5 {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}

	if got := len(exp.snapshot()); got != 5 {
		t.Errorf("exporter received %d records, want 5", got)
	}
}

// resetLogChannel exposes the agentstate test helper through the
// otlp_test package by going through the public API: subscribe and
// unsubscribe to drain any leftover state. Safer than relying on
// agentstate's internal reset helper from outside the package.
func resetLogChannel() {
	// Subscribe-and-discard pattern: any record published here is
	// silently dropped. The next test starts from a known baseline.
	ch := agentstate.SubscribeLogs(1)
	agentstate.UnsubscribeLogs(ch)
}
