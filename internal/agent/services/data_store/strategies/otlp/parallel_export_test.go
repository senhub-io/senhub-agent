package otlp

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"go.opentelemetry.io/otel/sdk/metric/metricdata"
	"go.opentelemetry.io/otel/sdk/resource"

	"senhub-agent.go/internal/agent/services/data_store/otelmapper"
	"senhub-agent.go/internal/agent/services/data_store/transformers"
)

// fakeNullDefs is a minimal DefinitionLookup that always returns nil
// (so otelmapper.Resolve never gets invoked — we feed records via
// extraRecords directly in these tests).
type fakeNullDefs struct{}

func (fakeNullDefs) GetProbeDefinition(string) *transformers.ProbeDefinition { return nil }

func TestPartitionRecordsByProbe(t *testing.T) {
	recs := []otelmapper.OtelRecord{
		{Name: "a", Attributes: map[string]string{"probe_name": "p1"}},
		{Name: "b", Attributes: map[string]string{"probe_name": "p1"}},
		{Name: "c", Attributes: map[string]string{"probe_name": "p2"}},
		{Name: "d", Attributes: map[string]string{}}, // agent self-metric
		{Name: "e"}, // nil attrs → __agent__ too
	}
	parts := partitionRecordsByProbe(recs)
	if got := len(parts["p1"]); got != 2 {
		t.Errorf("p1 partition size=%d, want 2", got)
	}
	if got := len(parts["p2"]); got != 1 {
		t.Errorf("p2 partition size=%d, want 1", got)
	}
	if got := len(parts["__agent__"]); got != 2 {
		t.Errorf("__agent__ partition size=%d, want 2", got)
	}
}

func TestPushMetrics_SmallBatchUsesSingleBatchPath(t *testing.T) {
	store := newMetricStore()
	// Inject one cms via upsert — actual records will come from
	// extraRecords because fakeNullDefs.GetProbeDefinition returns nil.
	var exportCalls atomic.Int32
	export := func(_ context.Context, _ *metricdata.ResourceMetrics) error {
		exportCalls.Add(1)
		return nil
	}
	extra := []otelmapper.OtelRecord{
		{Name: "agent.metric", Type: "gauge", Unit: "1", Value: 42, Attributes: map[string]string{}},
	}
	count, err := pushMetrics(context.Background(), store, fakeNullDefs{},
		resource.NewSchemaless(), "1.0.0", time.Now(), time.Now(),
		otelmapper.DefaultResolveOptions(), nil, extra, export, nil, 4)
	if err != nil {
		t.Fatalf("pushMetrics: %v", err)
	}
	if count != 1 {
		t.Errorf("count=%d, want 1", count)
	}
	if got := exportCalls.Load(); got != 1 {
		t.Errorf("export called %d times, want 1 (small batch → single path)", got)
	}
}

func TestPushMetrics_LargeBatchFansOutByProbe(t *testing.T) {
	store := newMetricStore()
	var exportCalls atomic.Int32
	export := func(_ context.Context, _ *metricdata.ResourceMetrics) error {
		exportCalls.Add(1)
		return nil
	}

	// 3 probes × 50 series = 150 records, above SplitBatchThreshold.
	var extra []otelmapper.OtelRecord
	for _, probe := range []string{"p1", "p2", "p3"} {
		for i := 0; i < 50; i++ {
			extra = append(extra, otelmapper.OtelRecord{
				Name:       "metric_" + probe + "_" + iota3(i),
				Type:       "gauge",
				Unit:       "1",
				Value:      float64(i),
				Attributes: map[string]string{"probe_name": probe},
			})
		}
	}

	count, err := pushMetrics(context.Background(), store, fakeNullDefs{},
		resource.NewSchemaless(), "1.0.0", time.Now(), time.Now(),
		otelmapper.DefaultResolveOptions(), nil, extra, export, nil, 4)
	if err != nil {
		t.Fatalf("pushMetrics: %v", err)
	}
	if count != 150 {
		t.Errorf("count=%d, want 150", count)
	}
	// One Export call per probe (3 probes → 3 sub-batches).
	if got := exportCalls.Load(); got != 3 {
		t.Errorf("export called %d times, want 3 (one per probe)", got)
	}
}

func TestPushMetrics_LargeBatch_MaxConcurrent1_StaysSingleBatch(t *testing.T) {
	store := newMetricStore()
	var exportCalls atomic.Int32
	export := func(_ context.Context, _ *metricdata.ResourceMetrics) error {
		exportCalls.Add(1)
		return nil
	}
	var extra []otelmapper.OtelRecord
	for i := 0; i < 200; i++ {
		extra = append(extra, otelmapper.OtelRecord{
			Name: "m_" + iota3(i), Type: "gauge", Unit: "1",
			Attributes: map[string]string{"probe_name": "p1"},
		})
	}
	_, err := pushMetrics(context.Background(), store, fakeNullDefs{},
		resource.NewSchemaless(), "1.0.0", time.Now(), time.Now(),
		otelmapper.DefaultResolveOptions(), nil, extra, export, nil, 1)
	if err != nil {
		t.Fatalf("pushMetrics: %v", err)
	}
	if got := exportCalls.Load(); got != 1 {
		t.Errorf("with max_concurrent_exports=1, export should be called once, got %d", got)
	}
}

func TestExportInParallel_PropagatesFirstError(t *testing.T) {
	parts := map[string][]otelmapper.OtelRecord{
		"p1": {{Name: "a"}, {Name: "b"}},
		"p2": {{Name: "c"}},
	}
	want := errors.New("boom on p2")
	var p1Done, p2Done atomic.Bool
	export := func(_ context.Context, rm *metricdata.ResourceMetrics) error {
		// Each ResourceMetrics carries one ScopeMetrics; the count of
		// data points distinguishes p1 (2) from p2 (1).
		if len(rm.ScopeMetrics[0].Metrics) == 1 {
			p2Done.Store(true)
			return want
		}
		p1Done.Store(true)
		return nil
	}
	total, err := exportInParallel(context.Background(), parts,
		resource.NewSchemaless(), "", time.Now(), time.Now(), export, 4)
	if err == nil || !errors.Is(err, want) {
		t.Errorf("err=%v, want wrapped %v", err, want)
	}
	// p1 succeeded, contributing 2 to total. p2 failed, contributing 0.
	if total != 2 {
		t.Errorf("total=%d, want 2 (only p1's 2 records counted)", total)
	}
	if !p1Done.Load() || !p2Done.Load() {
		t.Errorf("both partitions should have been attempted: p1=%v p2=%v", p1Done.Load(), p2Done.Load())
	}
}

func TestExportInParallel_RespectsConcurrencyBound(t *testing.T) {
	const concurrency = 2
	parts := map[string][]otelmapper.OtelRecord{}
	for i := 0; i < 8; i++ {
		parts["p"+iota3(i)] = []otelmapper.OtelRecord{{Name: "m"}}
	}

	var inFlight, maxObserved atomic.Int32
	export := func(_ context.Context, _ *metricdata.ResourceMetrics) error {
		now := inFlight.Add(1)
		// race: observed max may be stale, but tracks the upper bound.
		for {
			cur := maxObserved.Load()
			if now <= cur || maxObserved.CompareAndSwap(cur, now) {
				break
			}
		}
		time.Sleep(20 * time.Millisecond) // simulate gRPC latency
		inFlight.Add(-1)
		return nil
	}
	_, err := exportInParallel(context.Background(), parts,
		resource.NewSchemaless(), "", time.Now(), time.Now(), export, concurrency)
	if err != nil {
		t.Fatalf("err=%v", err)
	}
	if got := maxObserved.Load(); got > concurrency {
		t.Errorf("max concurrent = %d, want <= %d", got, concurrency)
	}
}

// iota3 is a stable string for naming in tests.
func iota3(i int) string {
	if i < 10 {
		return "00" + string(rune('0'+i))
	}
	if i < 100 {
		return "0" + string(rune('0'+i/10)) + string(rune('0'+i%10))
	}
	return string(rune('0'+i/100)) + string(rune('0'+(i/10)%10)) + string(rune('0'+i%10))
}
