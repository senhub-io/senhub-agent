package otlp

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	sdklog "go.opentelemetry.io/otel/sdk/log"
	"go.opentelemetry.io/otel/sdk/resource"
	"google.golang.org/genproto/googleapis/rpc/errdetails"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/durationpb"

	"senhub-agent.go/internal/agent/services/agentstate"
	"senhub-agent.go/internal/agent/services/entity"
	"senhub-agent.go/internal/agent/services/exporterrors"
)

// sizeLimitedExporter accepts an export only when its estimated size is within limit;
// otherwise it answers like grpc-go or OTLP/HTTP.
type sizeLimitedExporter struct {
	mu            sync.Mutex
	limit         int
	calls         int
	accepted      [][]sdklog.Record
	http413       bool
	failTransient func([]sdklog.Record) bool // optional: return codes.Unavailable for these exports (after size check passes)
}

func (s *sizeLimitedExporter) Export(_ context.Context, records []sdklog.Record) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.calls++
	size := 0
	for i := range records {
		size += estimateRecordBytes(&records[i])
	}
	if size > s.limit {
		if s.http413 {
			return fmt.Errorf("failed to send logs to http://127.0.0.1:4318/v1/logs: 413 Request Entity Too Large (body: )")
		}
		return status.Error(codes.ResourceExhausted, "grpc: received message after decompression larger than max 1048576")
	}
	if s.failTransient != nil && s.failTransient(records) {
		return status.Error(codes.Unavailable, "transient down")
	}
	cp := make([]sdklog.Record, len(records))
	for i := range records {
		cp[i] = records[i].Clone()
	}
	s.accepted = append(s.accepted, cp)
	return nil
}
func (s *sizeLimitedExporter) ForceFlush(context.Context) error { return nil }
func (s *sizeLimitedExporter) Shutdown(context.Context) error   { return nil }

func (s *sizeLimitedExporter) acceptedEntities() map[string]string {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := map[string]string{}
	for _, exp := range s.accepted {
		for id, rels := range entityPayloads(exp) {
			out[id] = rels
		}
	}
	return out
}

func (s *sizeLimitedExporter) totalAcceptedRecords() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	n := 0
	for _, exp := range s.accepted {
		n += len(exp)
	}
	return n
}

func (s *sizeLimitedExporter) callCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.calls
}

// containsEntity reports whether one of records describes the entity
// whose identity mentions id (entity.id is a key/value list).
func containsEntity(records []sdklog.Record, id string) bool {
	for got := range entityPayloads(records) {
		if strings.Contains(got, id) {
			return true
		}
	}
	return false
}

func TestSplitBisect_EntityEventsAllDeliveredOnce(t *testing.T) {
	const budget = 20 << 10
	fake := &sizeLimitedExporter{limit: budget}
	exporter := newSplittingLogExporter(fake, DefaultLogsMaxExportBytes, nil)
	baseline := &captureLogExporter{}
	emitTestEntities(t, baseline, 200, 12)
	want := entityPayloads(baseline.exports[0])

	before := agentstate.GetOTLPDroppedByReason()["record_too_large"]
	emitTestEntities(t, exporter, 200, 12)
	after := agentstate.GetOTLPDroppedByReason()["record_too_large"]
	if after != before {
		t.Fatalf("dropped record_too_large: got %d, want %d", after, before)
	}

	fake.mu.Lock()
	got := map[string]string{}
	for _, exp := range fake.accepted {
		size := 0
		for i := range exp {
			size += estimateRecordBytes(&exp[i])
			if size > budget {
				fake.mu.Unlock()
				t.Fatalf("export over budget: %d bytes", size)
			}
		}
		for id, rels := range entityPayloads(exp) {
			if _, dup := got[id]; dup {
				fake.mu.Unlock()
				t.Fatalf("entity %s exported twice", id)
			}
			got[id] = rels
		}
	}
	fake.mu.Unlock()

	if len(got) != len(want) {
		t.Fatalf("union entities: got %d, want %d", len(got), len(want))
	}
	for id, rels := range want {
		if got[id] != rels {
			t.Errorf("entity %s lost or altered relationships", id)
		}
	}

	if c := fake.callCount(); c > 2*200 {
		t.Errorf("too many calls: %d > 400", c)
	}
	if wb := exporter.(*splittingLogExporter).workingBudget(); wb >= DefaultLogsMaxExportBytes || wb < minLogsExportBudget {
		t.Errorf("working budget %d not lowered and within bounds", wb)
	}

	// second round should be more efficient due to learned budget
	oldCalls := fake.callCount()
	fake.limit = budget
	fake.accepted = fake.accepted[:0]
	fake.calls = 0
	emitTestEntities(t, exporter, 200, 12)
	newCalls := fake.callCount()
	if newCalls >= oldCalls {
		t.Errorf("second round calls %d not fewer than first %d", newCalls, oldCalls)
	}
}

func TestSplitBisect_EntityEventsHTTP413(t *testing.T) {
	const budget = 20 << 10
	fake := &sizeLimitedExporter{limit: budget, http413: true}
	exporter := newSplittingLogExporter(fake, DefaultLogsMaxExportBytes, nil)
	baseline := &captureLogExporter{}
	emitTestEntities(t, baseline, 200, 12)
	want := entityPayloads(baseline.exports[0])

	before := agentstate.GetOTLPDroppedByReason()["record_too_large"]
	emitTestEntities(t, exporter, 200, 12)
	after := agentstate.GetOTLPDroppedByReason()["record_too_large"]
	if after != before {
		t.Fatalf("dropped record_too_large: got %d, want %d", after, before)
	}

	fake.mu.Lock()
	got := map[string]string{}
	for _, exp := range fake.accepted {
		size := 0
		for i := range exp {
			size += estimateRecordBytes(&exp[i])
		}
		if size > budget {
			fake.mu.Unlock()
			t.Fatalf("export over budget: %d bytes", size)
		}
		for id, rels := range entityPayloads(exp) {
			if _, dup := got[id]; dup {
				fake.mu.Unlock()
				t.Fatalf("entity %s exported twice", id)
			}
			got[id] = rels
		}
	}
	fake.mu.Unlock()

	if len(got) != len(want) {
		t.Fatalf("union entities: got %d, want %d", len(got), len(want))
	}
	for id, rels := range want {
		if got[id] != rels {
			t.Errorf("entity %s lost or altered relationships", id)
		}
	}
}

func TestSplitBisect_OversizedEntityDroppedAndCounted(t *testing.T) {
	baseline := &captureLogExporter{}
	cfg := LogsSignal{BufferSize: 210, BatchSize: 210, BatchTimeout: time.Hour}
	pipe := buildLogsPipeline(baseline, resource.NewSchemaless(), cfg, "test")
	ctx := context.Background()
	for i := 0; i < 200; i++ {
		e := &entity.Entity{
			Type:       "host",
			ID:         map[string]any{"host.id": fmt.Sprintf("h-%04d", i)},
			Attributes: map[string]any{"host.name": fmt.Sprintf("host-%04d", i)},
		}
		rels := 12
		if i == 7 {
			rels = 400
		}
		for r := 0; r < rels; r++ {
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
	if len(baseline.exports) != 1 {
		t.Fatalf("baseline: %d exports, want 1", len(baseline.exports))
	}
	records := baseline.exports[0]
	want := entityPayloads(records)

	normal, huge, hugeID := 0, 0, ""
	for i := range records {
		n := estimateRecordBytes(&records[i])
		if n > huge {
			normal, huge = max(normal, huge), n
			for id := range entityPayloads(records[i : i+1]) {
				hugeID = id
			}
		} else if n > normal {
			normal = n
		}
	}
	limit := 20 << 10
	if normal > limit || huge <= limit {
		t.Fatalf("fixture: normal %d, huge %d, limit %d", normal, huge, limit)
	}

	fake := &sizeLimitedExporter{limit: limit}
	exporter := newSplittingLogExporter(fake, DefaultLogsMaxExportBytes, nil)
	tooLargeBefore := agentstate.GetOTLPDroppedByReason()["record_too_large"]
	if err := exporter.Export(ctx, records); err != nil {
		t.Fatalf("Export: %v, want nil (everything else delivered)", err)
	}

	got := map[string]string{}
	for _, exp := range fake.accepted {
		for id, rels := range entityPayloads(exp) {
			if _, dup := got[id]; dup {
				t.Errorf("entity %s delivered twice", id)
			}
			got[id] = rels
		}
	}
	if _, ok := got[hugeID]; ok || hugeID == "" {
		t.Fatal("the oversized entity was reported delivered")
	}
	if len(got) != 199 {
		t.Fatalf("delivered %d entities, want 199", len(got))
	}
	for id, rels := range got {
		if want[id] != rels {
			t.Errorf("entity %s altered", id)
		}
	}
	if dropped := agentstate.GetOTLPDroppedByReason()["record_too_large"] - tooLargeBefore; dropped != 1 {
		t.Errorf("record_too_large delta = %d, want 1", dropped)
	}
	if c := fake.callCount(); c > 2*len(records) {
		t.Errorf("calls = %d, over the 2n cap", c)
	}
}

func TestSplitBisect_NoRefusalPathUnchanged(t *testing.T) {
	// single chunk: same slice pointer
	{
		rec := &captureLogExporter{}
		emitTestEntities(t, rec, 5, 2)
		records := rec.exports[0]
		under := &recordingSliceExporter{}
		exporter := newSplittingLogExporter(under, DefaultLogsMaxExportBytes, nil)
		if err := exporter.Export(context.Background(), records); err != nil {
			t.Fatal(err)
		}
		if under.calls != 1 {
			t.Fatalf("calls = %d, want 1", under.calls)
		}
		if len(under.got) != len(records) {
			t.Fatalf("length mismatch: got %d, want %d", len(under.got), len(records))
		}
		if &under.got[0] != &records[0] {
			t.Fatal("slice not passed by reference")
		}
	}
	// multi chunk with no refusal: chunks identical to splitRecordsByBytes
	{
		rec := &captureLogExporter{}
		emitTestEntities(t, rec, 200, 12)
		records := rec.exports[0]
		expected := splitRecordsByBytes(records, 20<<10)
		under := &recordingSliceExporter{}
		exporter := newSplittingLogExporter(under, 20<<10, nil)
		if err := exporter.Export(context.Background(), records); err != nil {
			t.Fatal(err)
		}
		if len(under.exports) != len(expected) {
			t.Fatalf("chunks count = %d, want %d", len(under.exports), len(expected))
		}
		for i := range expected {
			if len(under.exports[i]) != len(expected[i]) {
				t.Errorf("chunk %d length = %d, want %d", i, len(under.exports[i]), len(expected[i]))
			}
			if len(expected[i]) > 0 && &under.exports[i][0] != &expected[i][0] {
				t.Errorf("chunk %d not same sub-slice", i)
			}
		}
		if wb := exporter.(*splittingLogExporter).workingBudget(); wb != 20<<10 {
			t.Errorf("working budget changed without refusal: got %d", wb)
		}
	}
}

// recordingSliceExporter keeps the slices it was handed, not copies, so
// a test can check the splitter forwards the caller's own records.
type recordingSliceExporter struct {
	mu      sync.Mutex
	calls   int
	got     []sdklog.Record
	exports [][]sdklog.Record
}

func (r *recordingSliceExporter) Export(_ context.Context, records []sdklog.Record) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.calls++
	r.got = records
	r.exports = append(r.exports, records)
	return nil
}
func (r *recordingSliceExporter) ForceFlush(context.Context) error { return nil }
func (r *recordingSliceExporter) Shutdown(context.Context) error   { return nil }

func TestSplitBisect_RetryInfoIsNotBisected(t *testing.T) {
	st, err := status.New(codes.ResourceExhausted, "throttled").WithDetails(
		&errdetails.RetryInfo{RetryDelay: durationpb.New(time.Second)},
	)
	if err != nil {
		t.Fatal(err)
	}
	retryErr := st.Err()
	sink := &scriptedLogExporter{errs: []error{retryErr}}
	rec := &captureLogExporter{}
	emitTestEntities(t, rec, 2, 2)
	exporter := newSplittingLogExporter(sink, DefaultLogsMaxExportBytes, nil)
	err = exporter.Export(context.Background(), rec.exports[0])
	if err == nil {
		t.Fatal("expected error")
	}
	if !exporterrors.IsRetryable(classifyExportError(err)) {
		t.Fatalf("expected retryable, got %v", err)
	}
	var se *splitExportError
	if errors.As(err, &se) {
		t.Fatalf("should not be splitExportError, got %T", se)
	}
	if sink.calls != 1 {
		t.Fatalf("calls = %d, want 1", sink.calls)
	}
}

func TestSplitBisect_TransientPartReturnsOnlyUndelivered(t *testing.T) {
	const budget = 20 << 10
	targetID := "h-0150"
	fake := &sizeLimitedExporter{
		limit: budget,
		failTransient: func(records []sdklog.Record) bool {
			return containsEntity(records, targetID)
		},
	}
	exporter := newSplittingLogExporter(fake, DefaultLogsMaxExportBytes, nil)
	baseline := &captureLogExporter{}
	emitTestEntities(t, baseline, 200, 12)
	records := baseline.exports[0]

	err := exporter.Export(context.Background(), records)
	if err == nil {
		t.Fatal("expected error")
	}
	var se *splitExportError
	if !errors.As(err, &se) {
		t.Fatalf("expected *splitExportError, got %T", err)
	}
	if se.rejected != 0 || se.dropped != 0 {
		t.Fatalf("unexpected rejected/dropped: %d/%d", se.rejected, se.dropped)
	}
	if len(se.undelivered) == 0 {
		t.Fatal("no undelivered records")
	}
	undeliveredEntities := entityPayloads(se.undelivered)
	if !containsEntity(se.undelivered, targetID) {
		t.Fatalf("target %s not in undelivered", targetID)
	}
	accepted := fake.acceptedEntities()
	for id := range accepted {
		if strings.Contains(id, targetID) {
			t.Fatalf("target %s found in accepted", id)
		}
		if _, dup := undeliveredEntities[id]; dup {
			t.Fatalf("entity %s both delivered and kept as undelivered", id)
		}
	}
	union := make(map[string]bool)
	for id := range accepted {
		union[id] = true
	}
	for id := range undeliveredEntities {
		union[id] = true
	}
	if len(union) != 200 {
		t.Fatalf("union size = %d, want 200", len(union))
	}
}

func TestSplitBisect_CancelledContextStops(t *testing.T) {
	fake := &sizeLimitedExporter{limit: 1} // always refuse size
	exporter := newSplittingLogExporter(fake, DefaultLogsMaxExportBytes, nil)
	rec := &captureLogExporter{}
	emitTestEntities(t, rec, 10, 2)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err := exporter.Export(ctx, rec.exports[0])
	if err == nil {
		t.Fatal("expected error")
	}
	var se *splitExportError
	if !errors.As(err, &se) {
		t.Fatalf("expected *splitExportError, got %T", err)
	}
	if len(se.undelivered) != len(rec.exports[0]) {
		t.Fatalf("undelivered = %d, want %d", len(se.undelivered), len(rec.exports[0]))
	}
	if fake.callCount() > 1 {
		t.Fatalf("too many calls: %d", fake.callCount())
	}
}

func TestPersistentLogExporter_SplitPersistsOnlyUndelivered(t *testing.T) {
	rejectedBefore := agentstate.GetOTLPDroppedByReason()["receiver_rejected"]
	q := newLogsQueue(t.TempDir(), 0, testModuleLogger(t))
	ple := newPersistentLogExporter(nil, q, testModuleLogger(t))
	fake := &sizeLimitedExporter{
		limit: 8 << 10,
		failTransient: func(recs []sdklog.Record) bool {
			for i := range recs {
				if strings.Contains(recs[i].Body().AsString(), "line-042") {
					return true
				}
			}
			return false
		},
	}
	ple.wrapped = newSplittingLogExporter(fake, DefaultLogsMaxExportBytes, ple.logger)

	pipe := buildLogsPipeline(ple, resource.NewSchemaless(), LogsSignal{BufferSize: 200, BatchSize: 100, BatchTimeout: time.Hour}, "test")
	ctx := context.Background()
	for i := 0; i < 100; i++ {
		body := fmt.Sprintf("line-%03d %s", i, strings.Repeat("x", 300))
		pipe.emit(ctx, agentstate.LogRecord{
			Timestamp:         time.Unix(1700000000+int64(i), 0),
			Severity:          9,
			SeverityText:      "INFO",
			Body:              body,
			ProducerProbeName: "syslog",
		})
	}
	// The failed part makes the flush report an error; what matters is
	// where each record ended up.
	_ = pipe.provider.ForceFlush(ctx)

	// Check queue records
	q.mu.Lock()
	queueRecs := q.records
	q.mu.Unlock()
	if queueRecs == 0 || queueRecs >= 100 {
		t.Fatalf("queue records = %d, want between 1 and 99", queueRecs)
	}

	// Read queue files and collect bodies
	var queuedBodies []string
	q.mu.Lock()
	files := q.listLocked()
	for _, name := range files {
		b, err := q.readBatchFile(filepath.Join(q.dir, name))
		if err != nil {
			continue
		}
		for _, pr := range b.Records {
			queuedBodies = append(queuedBodies, pr.Body)
		}
	}
	q.mu.Unlock()

	// Accepted bodies
	acceptedBodies := []string{}
	for _, exp := range fake.accepted {
		for _, rec := range exp {
			acceptedBodies = append(acceptedBodies, rec.Body().AsString())
		}
	}

	allBodies := map[string]bool{}
	for _, b := range acceptedBodies {
		allBodies[b] = true
	}
	for _, b := range queuedBodies {
		if allBodies[b] {
			t.Fatalf("body %q duplicated in accepted and queue", b)
		}
		allBodies[b] = true
	}
	if len(allBodies) != 100 {
		t.Fatalf("total distinct bodies = %d, want 100", len(allBodies))
	}
	if got := agentstate.GetOTLPDroppedByReason()["receiver_rejected"] - rejectedBefore; got != 0 {
		t.Fatalf("receiver_rejected delta = %d, want 0", got)
	}
}

func TestLogsReplay_SplitRewritesRemainder(t *testing.T) {
	q := newLogsQueue(t.TempDir(), 0, testModuleLogger(t))
	// Enqueue 50 sample records with distinct bodies of ~300 bytes
	prs := make([]persistedLogRecord, 50)
	for i := range prs {
		prs[i] = persistedLogRecord{
			TimestampUnixNano: int64(1700000000000000000 + i),
			SeverityNumber:    9,
			SeverityText:      "INFO",
			Body:              fmt.Sprintf("rec-%03d %s", i, strings.Repeat("y", 300)),
			Attributes:        map[string]string{"senhub.probe.name": "syslog"},
		}
	}
	if err := q.enqueue(prs); err != nil {
		t.Fatal(err)
	}

	// Build pipeline with splitting exporter and failing transient once for rec-030
	fake := &sizeLimitedExporter{
		limit: 8 << 10,
		failTransient: func(recs []sdklog.Record) bool {
			for _, rec := range recs {
				if strings.Contains(rec.Body().AsString(), "rec-030") {
					return true
				}
			}
			return false
		},
	}
	ple := newPersistentLogExporter(nil, q, testModuleLogger(t))
	ple.wrapped = newSplittingLogExporter(fake, DefaultLogsMaxExportBytes, ple.logger)
	pipe := buildLogsPipeline(ple, resource.NewSchemaless(), LogsSignal{
		BufferSize:   100,
		BatchSize:    100,
		BatchTimeout: time.Hour,
	}, "test")
	replayer := newLogsReplayer(q, pipe, ple, testModuleLogger(t))

	q.mu.Lock()
	savedAt := mustReadOnlyBatch(t, q).SavedAt
	q.mu.Unlock()

	// First replay: should fail with remainder
	_, err := replayer.replay()
	if err == nil {
		t.Fatal("expected error on first replay")
	}
	// Queue should still have exactly one file with fewer records
	q.mu.Lock()
	files := q.listLocked()
	if len(files) != 1 {
		q.mu.Unlock()
		t.Fatalf("queue files after first replay = %d, want 1", len(files))
	}
	b, err := q.readBatchFile(filepath.Join(q.dir, files[0]))
	q.mu.Unlock()
	if err != nil {
		t.Fatal(err)
	}
	if len(b.Records) == 0 || len(b.Records) >= 50 {
		t.Fatalf("remaining records = %d, want between 1 and 49", len(b.Records))
	}
	if !b.SavedAt.Equal(savedAt) {
		t.Fatalf("SavedAt changed: %v -> %v", savedAt, b.SavedAt)
	}
	// Second replay: should succeed and queue empty
	// Reset failTransient to always succeed
	fake.failTransient = nil
	_, err = replayer.replay()
	if err != nil {
		t.Fatalf("second replay error: %v", err)
	}
	q.mu.Lock()
	files = q.listLocked()
	q.mu.Unlock()
	if len(files) != 0 {
		t.Fatalf("queue files after second replay = %d, want 0", len(files))
	}
	// Verify all 50 bodies accepted exactly once
	accepted := map[string]bool{}
	for _, exp := range fake.accepted {
		for _, rec := range exp {
			body := rec.Body().AsString()
			if accepted[body] {
				t.Fatalf("duplicate accepted body %q", body)
			}
			accepted[body] = true
		}
	}
	if len(accepted) != 50 {
		t.Fatalf("accepted distinct bodies = %d, want 50", len(accepted))
	}
}

func mustReadOnlyBatch(t *testing.T, q *logsQueue) logBatch {
	t.Helper()
	files := q.listLocked()
	if len(files) != 1 {
		t.Fatalf("queue files = %d, want 1", len(files))
	}
	b, err := q.readBatchFile(filepath.Join(q.dir, files[0]))
	if err != nil {
		t.Fatal(err)
	}
	return b
}
