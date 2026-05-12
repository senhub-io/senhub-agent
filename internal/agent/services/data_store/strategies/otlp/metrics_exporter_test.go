package otlp

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"go.opentelemetry.io/otel/sdk/metric/metricdata"
	"go.opentelemetry.io/otel/sdk/resource"

	"senhub-agent.go/internal/agent/services/data_store/otelmapper"
	"senhub-agent.go/internal/agent/services/data_store/transformers"
	"senhub-agent.go/internal/agent/tags"
	"senhub-agent.go/internal/agent/types/datapoint"
)

// fakeDefs is a map-backed DefinitionLookup used to drive otelmapper.Resolve
// without loading actual YAML.
type fakeDefs struct {
	defs map[string]*transformers.ProbeDefinition
}

func (f *fakeDefs) GetProbeDefinition(probeType string) *transformers.ProbeDefinition {
	return f.defs[probeType]
}

// newCounterDef returns a probe definition with a single counter metric
// `cpu_user_time` mapped to OTel `system.cpu.time` (counter, unit "s").
func newCounterDef() *transformers.ProbeDefinition {
	return &transformers.ProbeDefinition{
		ProbeName: "cpu",
		Metrics: []transformers.MetricDefinition{
			{
				Name:        "cpu_user_time",
				Unit:        "s",
				Description: "User CPU time",
				Otel: &transformers.OtelMapping{
					Name: "system.cpu.time",
					Unit: "s",
					Type: "counter",
					Attributes: map[string]string{
						"cpu.mode": "user",
					},
				},
			},
		},
	}
}

func newGaugeDef() *transformers.ProbeDefinition {
	return &transformers.ProbeDefinition{
		ProbeName: "cpu",
		Metrics: []transformers.MetricDefinition{
			{
				Name:        "cpu_utilization",
				Unit:        "%",
				Description: "CPU utilization",
				Otel: &transformers.OtelMapping{
					Name: "system.cpu.utilization",
					Unit: "1",
					Type: "gauge",
				},
			},
		},
	}
}

func TestPushMetrics_BuildsResourceMetricsWithCounter(t *testing.T) {
	store := newMetricStore()
	identity := []tags.Tag{
		{Key: "probe_name", Value: "host-a"},
		{Key: "probe_type", Value: "cpu"},
	}
	store.upsert(datapoint.DataPoint{Name: "cpu_user_time", Value: 42, Tags: identity})

	defs := &fakeDefs{defs: map[string]*transformers.ProbeDefinition{
		"cpu": newCounterDef(),
	}}
	res := resource.NewSchemaless()

	var captured *metricdata.ResourceMetrics
	export := func(_ context.Context, rm *metricdata.ResourceMetrics) error {
		captured = rm
		return nil
	}

	startTime := time.Unix(1700000000, 0)
	now := time.Unix(1700000010, 0)

	count, err := pushMetrics(context.Background(), store, defs, res, "1.2.3",
		startTime, now, otelmapper.DefaultResolveOptions(), nil, export, nil)
	if err != nil {
		t.Fatalf("pushMetrics: %v", err)
	}
	if count != 1 {
		t.Errorf("count=%d, want 1", count)
	}
	if captured == nil {
		t.Fatal("export not called")
	}
	if got := len(captured.ScopeMetrics); got != 1 {
		t.Fatalf("ScopeMetrics len=%d", got)
	}
	if got := captured.ScopeMetrics[0].Scope.Version; got != "1.2.3" {
		t.Errorf("Scope.Version=%q", got)
	}
	if got := len(captured.ScopeMetrics[0].Metrics); got != 1 {
		t.Fatalf("Metrics len=%d", got)
	}
	m := captured.ScopeMetrics[0].Metrics[0]
	if m.Name != "system.cpu.time" || m.Unit != "s" {
		t.Errorf("metric=%+v", m)
	}
	sum, ok := m.Data.(metricdata.Sum[float64])
	if !ok {
		t.Fatalf("Data is %T, want Sum[float64]", m.Data)
	}
	if !sum.IsMonotonic {
		t.Error("counter should be monotonic")
	}
	if sum.Temporality != metricdata.CumulativeTemporality {
		t.Errorf("temporality=%v", sum.Temporality)
	}
	if got := len(sum.DataPoints); got != 1 {
		t.Fatalf("DataPoints len=%d", got)
	}
	dp := sum.DataPoints[0]
	if dp.Value != 42 {
		t.Errorf("value=%v", dp.Value)
	}
	if !dp.StartTime.Equal(startTime) {
		t.Errorf("StartTime=%v", dp.StartTime)
	}
	if !dp.Time.Equal(now) {
		t.Errorf("Time=%v", dp.Time)
	}
}

func TestPushMetrics_BuildsResourceMetricsWithGauge(t *testing.T) {
	store := newMetricStore()
	identity := []tags.Tag{
		{Key: "probe_name", Value: "host-a"},
		{Key: "probe_type", Value: "cpu"},
	}
	// 50% raw, after % → ratio conversion the OtelRecord value is 0.5.
	store.upsert(datapoint.DataPoint{
		Name:  "cpu_utilization",
		Value: 50,
		Tags:  append(identity, tags.Tag{Key: "unit", Value: "%"}),
	})

	defs := &fakeDefs{defs: map[string]*transformers.ProbeDefinition{
		"cpu": newGaugeDef(),
	}}

	var captured *metricdata.ResourceMetrics
	count, err := pushMetrics(context.Background(), store, defs, resource.NewSchemaless(), "",
		time.Now(), time.Now(), otelmapper.DefaultResolveOptions(), nil,
		func(_ context.Context, rm *metricdata.ResourceMetrics) error {
			captured = rm
			return nil
		}, nil)
	if err != nil {
		t.Fatalf("pushMetrics: %v", err)
	}
	if count != 1 {
		t.Errorf("count=%d", count)
	}
	g, ok := captured.ScopeMetrics[0].Metrics[0].Data.(metricdata.Gauge[float64])
	if !ok {
		t.Fatalf("Data is %T, want Gauge[float64]", captured.ScopeMetrics[0].Metrics[0].Data)
	}
	if g.DataPoints[0].Value != 0.5 {
		t.Errorf("value=%v, want 0.5 (after %% → ratio)", g.DataPoints[0].Value)
	}
}

func TestPushMetrics_NoStoredMetricsIsNoOp(t *testing.T) {
	store := newMetricStore()
	defs := &fakeDefs{defs: map[string]*transformers.ProbeDefinition{}}

	called := false
	count, err := pushMetrics(context.Background(), store, defs, resource.NewSchemaless(), "",
		time.Now(), time.Now(), otelmapper.DefaultResolveOptions(), nil,
		func(_ context.Context, _ *metricdata.ResourceMetrics) error {
			called = true
			return nil
		}, nil)
	if err != nil {
		t.Fatalf("err=%v", err)
	}
	if count != 0 {
		t.Errorf("count=%d", count)
	}
	if called {
		t.Errorf("export called on empty store")
	}
}

func TestPushMetrics_MissingProbeDefTriggersHandlerAndContinues(t *testing.T) {
	store := newMetricStore()
	identity := []tags.Tag{
		{Key: "probe_name", Value: "x"},
		{Key: "probe_type", Value: "unknown-probe"},
	}
	store.upsert(datapoint.DataPoint{Name: "m", Value: 1, Tags: identity})

	// Add one with a known def so we can verify the export still runs
	// for the others.
	identity2 := []tags.Tag{
		{Key: "probe_name", Value: "y"},
		{Key: "probe_type", Value: "cpu"},
	}
	store.upsert(datapoint.DataPoint{Name: "cpu_user_time", Value: 5, Tags: identity2})

	defs := &fakeDefs{defs: map[string]*transformers.ProbeDefinition{
		"cpu": newCounterDef(),
	}}

	var (
		mu      sync.Mutex
		handled []string
	)
	handler := func(m otelmapper.CacheMetric, err error) {
		mu.Lock()
		handled = append(handled, m.ProbeType+":"+m.MetricName)
		mu.Unlock()
	}

	var capturedCount int
	count, err := pushMetrics(context.Background(), store, defs, resource.NewSchemaless(), "",
		time.Now(), time.Now(), otelmapper.DefaultResolveOptions(), nil,
		func(_ context.Context, rm *metricdata.ResourceMetrics) error {
			capturedCount = len(rm.ScopeMetrics[0].Metrics)
			return nil
		}, handler)
	if err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Errorf("count=%d, want 1 (only cpu mapped)", count)
	}
	if capturedCount != 1 {
		t.Errorf("captured Metrics len=%d", capturedCount)
	}
	if len(handled) != 1 || handled[0] != "unknown-probe:m" {
		t.Errorf("handler calls=%v, want exactly unknown-probe:m", handled)
	}
}

func TestPushMetrics_ExportErrorPropagates(t *testing.T) {
	store := newMetricStore()
	identity := []tags.Tag{
		{Key: "probe_name", Value: "host"},
		{Key: "probe_type", Value: "cpu"},
	}
	store.upsert(datapoint.DataPoint{Name: "cpu_user_time", Value: 1, Tags: identity})

	defs := &fakeDefs{defs: map[string]*transformers.ProbeDefinition{
		"cpu": newCounterDef(),
	}}

	expected := errors.New("collector down")
	_, err := pushMetrics(context.Background(), store, defs, resource.NewSchemaless(), "",
		time.Now(), time.Now(), otelmapper.DefaultResolveOptions(), nil,
		func(_ context.Context, _ *metricdata.ResourceMetrics) error { return expected },
		nil)
	if err == nil || !errors.Is(err, expected) {
		t.Errorf("err=%v, want wrapped %v", err, expected)
	}
}

func TestBuildResourceMetrics_GroupsByNameUnitType(t *testing.T) {
	now := time.Unix(1700000000, 0)
	startTime := time.Unix(1699999000, 0)

	records := []otelmapper.OtelRecord{
		{Name: "system.cpu.time", Unit: "s", Type: "counter", Value: 1, Attributes: map[string]string{"mode": "user"}},
		{Name: "system.cpu.time", Unit: "s", Type: "counter", Value: 2, Attributes: map[string]string{"mode": "sys"}},
		{Name: "system.memory.usage", Unit: "By", Type: "gauge", Value: 100},
	}
	rm := buildResourceMetrics(records, resource.NewSchemaless(), "1.0", startTime, now)

	if got := len(rm.ScopeMetrics[0].Metrics); got != 2 {
		t.Fatalf("Metrics len=%d, want 2 (one per name+unit+type)", got)
	}
	cpuTime := rm.ScopeMetrics[0].Metrics[0]
	if cpuTime.Name != "system.cpu.time" {
		t.Errorf("first metric name=%q", cpuTime.Name)
	}
	sum := cpuTime.Data.(metricdata.Sum[float64])
	if got := len(sum.DataPoints); got != 2 {
		t.Errorf("CPU time DataPoints=%d, want 2 (variant by mode)", got)
	}
}

func TestBuildResourceMetrics_DefaultsScopeVersion(t *testing.T) {
	rm := buildResourceMetrics(
		[]otelmapper.OtelRecord{{Name: "x", Unit: "1", Type: "gauge", Value: 1}},
		resource.NewSchemaless(),
		"", // unset
		time.Now(), time.Now(),
	)
	if got := rm.ScopeMetrics[0].Scope.Version; got != "dev" {
		t.Errorf("Scope.Version=%q, want fallback 'dev'", got)
	}
}

func TestBuildAggregation_TypeMapping(t *testing.T) {
	now := time.Now()
	rec := otelmapper.OtelRecord{Name: "x", Unit: "1", Value: 1}

	if _, ok := buildAggregation("counter", []otelmapper.OtelRecord{rec}, now, now).(metricdata.Sum[float64]); !ok {
		t.Error("counter should be Sum")
	}
	if a := buildAggregation("counter", []otelmapper.OtelRecord{rec}, now, now); !a.(metricdata.Sum[float64]).IsMonotonic {
		t.Error("counter should be monotonic")
	}
	if a := buildAggregation("updowncounter", []otelmapper.OtelRecord{rec}, now, now); a.(metricdata.Sum[float64]).IsMonotonic {
		t.Error("updowncounter should NOT be monotonic")
	}
	if _, ok := buildAggregation("gauge", []otelmapper.OtelRecord{rec}, now, now).(metricdata.Gauge[float64]); !ok {
		t.Error("gauge should be Gauge")
	}
	// Unknown types fall back to Gauge — better than dropping.
	if _, ok := buildAggregation("histogram", []otelmapper.OtelRecord{rec}, now, now).(metricdata.Gauge[float64]); !ok {
		t.Error("unknown type should fall back to Gauge")
	}
}
