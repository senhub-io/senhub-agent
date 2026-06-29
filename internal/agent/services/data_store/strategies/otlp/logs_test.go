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
	"senhub-agent.go/internal/agent/services/entity"
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

// TestLogsPipeline_EntityRecordPerMethodScope asserts an entity record is
// emitted under the instrumentation scope of its discovery method (#253): each
// distinct scope produces its own ScopeLogs (scope.name = the method, the
// otel.entity.entity_event=true scope attribute preserved), and an empty scope
// falls back to the generic entities scope.
func TestLogsPipeline_EntityRecordPerMethodScope(t *testing.T) {
	pipe, exp := newTestLogsPipeline(t, 10, 1024, 10*time.Millisecond)
	defer pipe.shutdown(context.Background())

	ts := time.Unix(1700000000, 0)
	build := func(typ, id, scope string) (string, log.Record) {
		s, rec, err := buildEntityRecord(entityEvent(typ, id, scope, ts))
		if err != nil {
			t.Fatalf("buildEntityRecord: %v", err)
		}
		return s, rec
	}
	for _, e := range []struct{ typ, id, scope string }{
		{"network.device", "serial:9:a", "senhub-agent/snmp-ifmib"},
		{"network.route", "serial:9:a|0.0.0.0/0", "senhub-agent/snmp-route"},
		{"host", "h-001", ""}, // foundation → generic entities scope
	} {
		scope, rec := build(e.typ, e.id, e.scope)
		pipe.emitEntityRecord(context.Background(), scope, rec)
	}

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if len(exp.snapshot()) >= 3 {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	got := exp.snapshot()
	if len(got) != 3 {
		t.Fatalf("got %d records, want 3", len(got))
	}

	byScope := map[string]int{}
	for _, rec := range got {
		sc := rec.InstrumentationScope()
		byScope[sc.Name]++
		// Every entity Logger — method-scoped or generic — carries the
		// entity-event fast-path flag.
		flagged := false
		for _, kv := range sc.Attributes.ToSlice() {
			if string(kv.Key) == scopeAttrEntityEvent && kv.Value.AsBool() {
				flagged = true
			}
		}
		if !flagged {
			t.Errorf("scope %q missing %s=true", sc.Name, scopeAttrEntityEvent)
		}
	}
	for _, want := range []string{"senhub-agent/snmp-ifmib", "senhub-agent/snmp-route", entitiesScopeName} {
		if byScope[want] != 1 {
			t.Errorf("scope %q: got %d records, want 1 (all scopes: %v)", want, byScope[want], byScope)
		}
	}
}

func entityEvent(typ, id, scope string, ts time.Time) entity.Event {
	return entity.Event{
		Kind: entity.EntityState,
		Time: ts,
		Entity: &entity.Entity{
			Type:  typ,
			ID:    map[string]any{"id": id},
			Scope: scope,
		},
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
