package otlp

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"go.opentelemetry.io/otel/attribute"
	sdklog "go.opentelemetry.io/otel/sdk/log"
	"go.opentelemetry.io/otel/sdk/resource"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"senhub-agent.go/internal/agent/services/agentstate"
	"senhub-agent.go/internal/agent/services/entity"
	"senhub-agent.go/internal/agent/services/exporterrors"
)

type scriptedLogExporter struct {
	errs  []error
	calls int
}

func (s *scriptedLogExporter) Export(context.Context, []sdklog.Record) error {
	i := s.calls
	s.calls++
	if i < len(s.errs) {
		return s.errs[i]
	}
	return nil
}
func (s *scriptedLogExporter) ForceFlush(context.Context) error { return nil }
func (s *scriptedLogExporter) Shutdown(context.Context) error   { return nil }

type captureLogExporter struct {
	mu      sync.Mutex
	exports [][]sdklog.Record
}

func (c *captureLogExporter) Export(_ context.Context, records []sdklog.Record) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	cp := make([]sdklog.Record, len(records))
	for i := range records {
		cp[i] = records[i].Clone()
	}
	c.exports = append(c.exports, cp)
	return nil
}
func (c *captureLogExporter) ForceFlush(context.Context) error { return nil }
func (c *captureLogExporter) Shutdown(context.Context) error   { return nil }

func emitTestEntities(t *testing.T, exp sdklog.Exporter, n, relsPerEntity int) {
	t.Helper()
	cfg := LogsSignal{BufferSize: n + 10, BatchSize: n + 10, BatchTimeout: time.Hour}
	pipe := buildLogsPipeline(exp, resource.NewSchemaless(), cfg, "test")
	ctx := context.Background()
	for i := 0; i < n; i++ {
		e := &entity.Entity{
			Type:       "host",
			ID:         map[string]any{"host.id": fmt.Sprintf("h-%04d", i)},
			Attributes: map[string]any{"host.name": fmt.Sprintf("host-%04d", i)},
		}
		for r := 0; r < relsPerEntity; r++ {
			e.Relationships = append(e.Relationships, entity.Relationship{
				Type:       "connected_to",
				TargetType: "network.interface",
				TargetID:   map[string]any{"network.interface.id": fmt.Sprintf("if-%04d-%03d", i, r)},
			})
		}
		scope, rec, err := buildEntityRecord(entity.Event{Kind: entity.EntityState, Entity: e, Time: time.Unix(1780272000, 0).UTC(), Interval: time.Minute})
		if err != nil {
			t.Fatalf("buildEntityRecord: %v", err)
		}
		pipe.emitEntityRecord(ctx, scope, rec)
	}
	if err := pipe.provider.ForceFlush(ctx); err != nil {
		t.Fatalf("flush: %v", err)
	}
}

func entityPayloads(records []sdklog.Record) map[string]string {
	out := map[string]string{}
	for i := range records {
		var id, rels string
		records[i].WalkAttributes(func(kv attribute.KeyValue) bool {
			switch string(kv.Key) {
			case "entity.id":
				id = kv.Value.String()
			case "entity.relationships":
				rels = kv.Value.String()
			}
			return true
		})
		out[id] = rels
	}
	return out
}

func TestSplittingLogExporter_SplitsBetweenEntitiesUnderBudget(t *testing.T) {
	const budget = 20 << 10

	whole := &captureLogExporter{}
	emitTestEntities(t, whole, 200, 12)
	if len(whole.exports) != 1 {
		t.Fatalf("baseline: %d exports, want 1", len(whole.exports))
	}
	want := entityPayloads(whole.exports[0])

	total := 0
	for i := range whole.exports[0] {
		total += estimateRecordBytes(&whole.exports[0][i])
	}
	if total <= budget {
		t.Fatalf("fixture too small: %d bytes <= budget %d; the test proves nothing", total, budget)
	}

	sink := &captureLogExporter{}
	emitTestEntities(t, newSplittingLogExporter(sink, budget, nil), 200, 12)

	if len(sink.exports) < 2 {
		t.Fatalf("expected several exports, got %d", len(sink.exports))
	}
	got := map[string]string{}
	count := 0
	for _, exp := range sink.exports {
		size := 0
		for i := range exp {
			size += estimateRecordBytes(&exp[i])
		}
		if size > budget {
			t.Errorf("export of %d records estimated %d bytes, over budget %d", len(exp), size, budget)
		}
		count += len(exp)
		for id, rels := range entityPayloads(exp) {
			if _, dup := got[id]; dup {
				t.Errorf("entity %s exported twice", id)
			}
			got[id] = rels
		}
	}
	if count != 200 || len(got) != len(want) {
		t.Fatalf("union has %d records / %d entities, want 200 / %d", count, len(got), len(want))
	}
	for id, rels := range want {
		if got[id] != rels {
			t.Errorf("entity %s lost or altered its relationships array", id)
		}
	}
}

func TestSplittingLogExporter_OversizedRecordGoesAlone(t *testing.T) {
	sink := &captureLogExporter{}
	emitTestEntities(t, newSplittingLogExporter(sink, 64, nil), 3, 2)
	if len(sink.exports) != 3 {
		t.Fatalf("got %d exports, want one per oversized record (3)", len(sink.exports))
	}
}

func TestSplittingLogExporter_SmallBatchPassesThrough(t *testing.T) {
	sink := &captureLogExporter{}
	emitTestEntities(t, newSplittingLogExporter(sink, 0, nil), 5, 2)
	if len(sink.exports) != 1 || len(sink.exports[0]) != 5 {
		t.Fatalf("small batch was split: %d exports", len(sink.exports))
	}
}

func TestSplittingLogExporter_MixedErrorKeepsOnlyUndelivered(t *testing.T) {
	sink := &scriptedLogExporter{errs: []error{
		status.Error(codes.Unavailable, "down"),
		status.Error(codes.ResourceExhausted, "message larger than max"),
	}}
	tooLargeBefore := agentstate.GetOTLPDroppedByReason()["record_too_large"]
	rec := &captureLogExporter{}
	emitTestEntities(t, rec, 2, 2)
	records := rec.exports[0]
	// Split into two chunks of 1 record each using a tiny budget
	exporter := newSplittingLogExporter(sink, 64, nil)
	err := exporter.Export(context.Background(), records)
	if err == nil {
		t.Fatal("expected error")
	}
	se, ok := err.(*splitExportError)
	if !ok {
		t.Fatalf("expected *splitExportError, got %T", err)
	}
	if len(se.undelivered) != 1 {
		t.Fatalf("undelivered = %d, want 1", len(se.undelivered))
	}
	if se.rejected != 0 {
		t.Fatalf("rejected = %d, want 0", se.rejected)
	}
	if se.dropped != 1 {
		t.Fatalf("dropped = %d, want 1", se.dropped)
	}
	if got := agentstate.GetOTLPDroppedByReason()["record_too_large"] - tooLargeBefore; got != 1 {
		t.Fatalf("record_too_large dropped = %d, want 1", got)
	}
	if !exporterrors.IsRetryable(classifyExportError(se.err)) {
		t.Fatalf("expected retryable joined error, got %v", se.err)
	}
}
