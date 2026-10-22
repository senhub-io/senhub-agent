package otlp

import (
	"context"
	"errors"
	"testing"
	"time"

	"go.opentelemetry.io/otel/sdk/metric/metricdata"
	"go.opentelemetry.io/otel/sdk/resource"

	"senhub-agent.go/internal/agent/services/agentstate"
	"senhub-agent.go/internal/agent/services/data_store/otelmapper"
	"senhub-agent.go/internal/agent/services/data_store/transformers"
	"senhub-agent.go/internal/agent/tags"
	"senhub-agent.go/internal/agent/types/datapoint"
)

func keepaliveBase() time.Time {
	return time.Date(2026, 8, 20, 12, 0, 0, 0, time.UTC)
}

func gaugeDatapoint(name string, value float64, at time.Time) datapoint.DataPoint {
	return datapoint.DataPoint{
		Name:      name,
		Value:     value,
		Timestamp: at,
		Tags: []tags.Tag{
			{Key: "probe_name", Value: "p"},
			{Key: "probe_type", Value: "cpu"},
		},
	}
}

func assertCandidate(t *testing.T, c exportCandidate, wantAt time.Time, wantKeepalive bool) {
	t.Helper()
	if !c.At.Equal(wantAt) {
		t.Fatalf("candidate At = %v, want %v", c.At, wantAt)
	}
	if c.Keepalive != wantKeepalive {
		t.Fatalf("candidate Keepalive = %v, want %v", c.Keepalive, wantKeepalive)
	}
}

func TestKeepalive_60sGauge_NewMeasurementRules(t *testing.T) {
	base := keepaliveBase()
	store := newMetricStore().withKeepalive(4 * time.Minute)
	store.noteProbeCadence("p", 60*time.Second)

	store.upsertAt(gaugeDatapoint("cpu_utilization", 50, base), base)

	cands := store.snapshot(base.Add(30 * time.Second))
	if len(cands) != 1 {
		t.Fatalf("candidates = %d, want 1", len(cands))
	}
	assertCandidate(t, cands[0], base, false)

	store.markEmitted(cands)

	if got := store.snapshot(base.Add(60 * time.Second)); len(got) != 0 {
		t.Fatalf("no new datapoint should not re-emit: %+v", got)
	}

	next := base.Add(60 * time.Second)
	store.upsertAt(gaugeDatapoint("cpu_utilization", 50, next), next)

	cands = store.snapshot(base.Add(90 * time.Second))
	if len(cands) != 1 {
		t.Fatalf("candidates after new datapoint = %d, want 1", len(cands))
	}
	assertCandidate(t, cands[0], next, false)
}

func TestKeepalive_30mGauge_KeepaliveAtInterval(t *testing.T) {
	base := keepaliveBase()
	store := newMetricStore().
		withFreshnessGrace(30 * time.Second).
		withKeepalive(4 * time.Minute)
	store.noteProbeCadence("p", 30*time.Minute)

	store.upsertAt(gaugeDatapoint("cpu_utilization", 50, base), base)
	store.markEmitted(store.snapshot(base))

	for _, now := range []time.Time{
		base.Add(1 * time.Minute),
		base.Add(2 * time.Minute),
		base.Add(3 * time.Minute),
	} {
		if got := store.snapshot(now); len(got) != 0 {
			t.Fatalf("at %v: candidates = %+v, want none", now, got)
		}
	}

	keepAt := base.Add(4 * time.Minute)
	cands := store.snapshot(keepAt)
	if len(cands) != 1 {
		t.Fatalf("at %v: candidates = %d, want 1 keepalive", keepAt, len(cands))
	}
	assertCandidate(t, cands[0], keepAt, true)

	store.markEmitted(cands)

	if got := store.snapshot(base.Add(5 * time.Minute)); len(got) != 0 {
		t.Fatalf("at %v: candidates = %+v, want none", base.Add(5*time.Minute), got)
	}

	secondKeep := base.Add(8 * time.Minute)
	cands = store.snapshot(secondKeep)
	if len(cands) != 1 {
		t.Fatalf("at %v: candidates = %d, want 1 keepalive", secondKeep, len(cands))
	}
	assertCandidate(t, cands[0], secondKeep, true)
}

func TestKeepalive_StoppedProbe_StopsAfterVouchWindow(t *testing.T) {
	base := keepaliveBase()
	store := newMetricStore().
		withFreshnessGrace(30 * time.Second).
		withKeepalive(4 * time.Minute)
	store.noteProbeCadence("p", 30*time.Minute)

	store.upsertAt(gaugeDatapoint("cpu_utilization", 50, base), base)
	store.markEmitted(store.snapshot(base))

	cands := store.snapshot(base.Add(4 * time.Minute))
	if len(cands) != 1 || !cands[0].Keepalive {
		t.Fatalf("keepalive candidate = %+v, want one keepalive", cands)
	}
	store.markEmitted(cands)

	pastVouch := base.Add(45*time.Minute + 31*time.Second)
	if got := store.snapshot(pastVouch); len(got) != 0 {
		t.Fatalf("after vouch window: candidates = %+v, want none", got)
	}
}

func TestPushMetrics_KeepaliveGaugeFilter(t *testing.T) {
	base := keepaliveBase()
	baseKeep := agentstate.GetOTLPKeepalivePointsTotal()
	start := base.Add(-time.Minute)

	t.Run("counter is never keepalive", func(t *testing.T) {
		store := newMetricStore().
			withFreshnessGrace(30 * time.Second).
			withKeepalive(4 * time.Minute)
		store.noteProbeCadence("cpu", 30*time.Minute)
		store.upsertAt(datapoint.DataPoint{
			Name:      "cpu_user_time",
			Value:     42,
			Timestamp: base,
			Tags: []tags.Tag{
				{Key: "probe_name", Value: "cpu"},
				{Key: "probe_type", Value: "cpu"},
			},
		}, base)

		defs := &fakeDefs{defs: map[string]*transformers.ProbeDefinition{"cpu": newCounterDef()}}
		exportCalls := 0
		var captured *metricdata.ResourceMetrics
		export := func(_ context.Context, rm *metricdata.ResourceMetrics) error {
			exportCalls++
			captured = rm
			return nil
		}

		count, err := pushMetrics(context.Background(), store, defs, resource.NewSchemaless(), "",
			start, base, otelmapper.DefaultResolveOptions(), nil, nil, export, nil, 1)
		if err != nil {
			t.Fatalf("pushMetrics: %v", err)
		}
		if count != 1 {
			t.Fatalf("initial count = %d, want 1", count)
		}
		if exportCalls != 1 {
			t.Fatalf("initial export calls = %d, want 1", exportCalls)
		}
		if got := captured.ScopeMetrics[0].Metrics[0].Data.(metricdata.Sum[float64]).DataPoints[0].Time; !got.Equal(base) {
			t.Fatalf("initial counter time = %v, want %v", got, base)
		}

		for _, now := range []time.Time{base.Add(4 * time.Minute), base.Add(8 * time.Minute)} {
			before := exportCalls
			count, err := pushMetrics(context.Background(), store, defs, resource.NewSchemaless(), "",
				start, now, otelmapper.DefaultResolveOptions(), nil, nil, export, nil, 1)
			if err != nil {
				t.Fatalf("pushMetrics at %v: %v", now, err)
			}
			if count != 0 {
				t.Fatalf("pushMetrics at %v: count = %d, want 0 after gauge filter", now, count)
			}
			if exportCalls != before {
				t.Fatalf("pushMetrics at %v called export, want no export", now)
			}
		}

		if got := agentstate.GetOTLPKeepalivePointsTotal() - baseKeep; got != 0 {
			t.Fatalf("keepalive counter delta = %d, want 0 for counter", got)
		}
	})

	t.Run("gauge emits keepalive at export time", func(t *testing.T) {
		store := newMetricStore().
			withFreshnessGrace(30 * time.Second).
			withKeepalive(4 * time.Minute)
		store.noteProbeCadence("cpu", 30*time.Minute)
		store.upsertAt(datapoint.DataPoint{
			Name:      "cpu_utilization",
			Value:     50,
			Timestamp: base,
			Tags: []tags.Tag{
				{Key: "probe_name", Value: "cpu"},
				{Key: "probe_type", Value: "cpu"},
				{Key: "unit", Value: "%"},
			},
		}, base)

		defs := &fakeDefs{defs: map[string]*transformers.ProbeDefinition{"cpu": newGaugeDef()}}
		var captured *metricdata.ResourceMetrics
		export := func(_ context.Context, rm *metricdata.ResourceMetrics) error {
			captured = rm
			return nil
		}

		count, err := pushMetrics(context.Background(), store, defs, resource.NewSchemaless(), "",
			start, base, otelmapper.DefaultResolveOptions(), nil, nil, export, nil, 1)
		if err != nil {
			t.Fatalf("pushMetrics initial: %v", err)
		}
		if count != 1 {
			t.Fatalf("initial count = %d, want 1", count)
		}
		g := captured.ScopeMetrics[0].Metrics[0].Data.(metricdata.Gauge[float64])
		if !g.DataPoints[0].Time.Equal(base) {
			t.Fatalf("initial gauge time = %v, want %v", g.DataPoints[0].Time, base)
		}

		keepAt := base.Add(4 * time.Minute)
		count, err = pushMetrics(context.Background(), store, defs, resource.NewSchemaless(), "",
			start, keepAt, otelmapper.DefaultResolveOptions(), nil, nil, export, nil, 1)
		if err != nil {
			t.Fatalf("pushMetrics keepalive: %v", err)
		}
		if count != 1 {
			t.Fatalf("keepalive count = %d, want 1", count)
		}
		g = captured.ScopeMetrics[0].Metrics[0].Data.(metricdata.Gauge[float64])
		if !g.DataPoints[0].Time.Equal(keepAt) {
			t.Fatalf("keepalive gauge time = %v, want export time %v", g.DataPoints[0].Time, keepAt)
		}
		if g.DataPoints[0].Value != 0.5 {
			t.Fatalf("keepalive gauge value = %v, want 0.5", g.DataPoints[0].Value)
		}
		if got := agentstate.GetOTLPKeepalivePointsTotal() - baseKeep; got != 1 {
			t.Fatalf("keepalive counter delta = %d, want 1 for gauge", got)
		}
	})
}

func TestKeepalive_HistogramNeverKeepalive(t *testing.T) {
	base := keepaliveBase()
	store := newMetricStore().
		withFreshnessGrace(30 * time.Second).
		withKeepalive(4 * time.Minute)
	store.noteProbeCadence("p", 30*time.Minute)

	store.upsertAt(datapoint.DataPoint{
		Name:      "http.server.duration",
		Value:     2,
		Timestamp: base,
		Tags: []tags.Tag{
			{Key: "probe_name", Value: "p"},
			{Key: "probe_type", Value: "cpu"},
		},
		Histogram: &datapoint.HistogramValue{Count: 2, BucketCounts: []uint64{2}},
	}, base)

	cands := store.snapshot(base)
	if len(cands) != 1 {
		t.Fatalf("initial candidates = %d, want 1", len(cands))
	}
	assertCandidate(t, cands[0], base, false)

	store.markEmitted(cands)

	if got := store.snapshot(base.Add(4 * time.Minute)); len(got) != 0 {
		t.Fatalf("histogram produced keepalive candidate: %+v", got)
	}
}

func TestKeepalive_ZeroDisablesKeepalive(t *testing.T) {
	base := keepaliveBase()
	store := newMetricStore().withFreshnessGrace(30 * time.Second)
	store.noteProbeCadence("p", 30*time.Minute)

	store.upsertAt(gaugeDatapoint("cpu_utilization", 50, base), base)
	store.markEmitted(store.snapshot(base))

	for _, now := range []time.Time{base.Add(4 * time.Minute), base.Add(8 * time.Minute)} {
		if got := store.snapshot(now); len(got) != 0 {
			t.Fatalf("at %v: candidates = %+v, want none", now, got)
		}
	}
}

func TestPushMetrics_ExportError_DoesNotMarkEmitted(t *testing.T) {
	base := keepaliveBase()
	store := newMetricStore()
	store.upsertAt(datapoint.DataPoint{
		Name:      "cpu_utilization",
		Value:     50,
		Timestamp: base,
		Tags: []tags.Tag{
			{Key: "probe_name", Value: "cpu"},
			{Key: "probe_type", Value: "cpu"},
			{Key: "unit", Value: "%"},
		},
	}, base)

	defs := &fakeDefs{defs: map[string]*transformers.ProbeDefinition{"cpu": newGaugeDef()}}
	res := resource.NewSchemaless()
	start := base.Add(-time.Minute)

	calls := 0
	var captured *metricdata.ResourceMetrics
	export := func(_ context.Context, rm *metricdata.ResourceMetrics) error {
		calls++
		if calls == 1 {
			return errors.New("collector down")
		}
		captured = rm
		return nil
	}

	_, err := pushMetrics(context.Background(), store, defs, res, "",
		start, base, otelmapper.DefaultResolveOptions(), nil, nil, export, nil, 1)
	if err == nil {
		t.Fatal("first pushMetrics should fail")
	}

	count, err := pushMetrics(context.Background(), store, defs, res, "",
		start, base.Add(time.Second), otelmapper.DefaultResolveOptions(), nil, nil, export, nil, 1)
	if err != nil {
		t.Fatalf("second pushMetrics: %v", err)
	}
	if count != 1 {
		t.Fatalf("second count = %d, want 1", count)
	}
	if calls != 2 {
		t.Fatalf("export calls = %d, want 2", calls)
	}

	g := captured.ScopeMetrics[0].Metrics[0].Data.(metricdata.Gauge[float64])
	if !g.DataPoints[0].Time.Equal(base) {
		t.Fatalf("re-emitted time = %v, want original observation time %v", g.DataPoints[0].Time, base)
	}

	before := calls
	count, err = pushMetrics(context.Background(), store, defs, res, "",
		start, base.Add(2*time.Second), otelmapper.DefaultResolveOptions(), nil, nil, export, nil, 1)
	if err != nil {
		t.Fatalf("third pushMetrics: %v", err)
	}
	if count != 0 {
		t.Fatalf("third count = %d, want 0 after successful mark", count)
	}
	if calls != before {
		t.Fatalf("third pushMetrics called export, want no export")
	}
}

func TestKeepalive_NewMeasurementEarlierThanLastKeepalive_EmittedAtItsOwnTime(t *testing.T) {
	base := keepaliveBase()
	store := newMetricStore().
		withFreshnessGrace(30 * time.Second).
		withKeepalive(4 * time.Minute)
	store.noteProbeCadence("p", 30*time.Minute)

	store.upsertAt(gaugeDatapoint("cpu_utilization", 50, base), base)
	store.markEmitted(store.snapshot(base))

	cands := store.snapshot(base.Add(4 * time.Minute))
	if len(cands) != 1 || !cands[0].Keepalive {
		t.Fatalf("expected one keepalive candidate, got %+v", cands)
	}
	store.markEmitted(cands)

	older := base.Add(3 * time.Minute)
	received := base.Add(4*time.Minute + 30*time.Second)
	store.upsertAt(gaugeDatapoint("cpu_utilization", 50, older), received)

	cands = store.snapshot(received)
	if len(cands) != 1 {
		t.Fatalf("candidates = %d, want 1", len(cands))
	}
	assertCandidate(t, cands[0], older, false)
}

func TestKeepalive_RestoredEntry_EmittedOnce(t *testing.T) {
	base := keepaliveBase()
	observed := base.Add(-10 * time.Minute)

	store := newMetricStore().
		withFreshnessGrace(30 * time.Second).
		withKeepalive(4 * time.Minute)
	store.noteProbeCadence("p", 30*time.Minute)
	store.restoreFromSnapshot([]entrySnapshot{{
		ProbeName:  "p",
		ProbeType:  "cpu",
		MetricName: "cpu_utilization",
		Value:      50,
		Tags:       map[string]string{"probe_name": "p", "probe_type": "cpu"},
		ObservedAt: observed,
	}})

	cands := store.snapshot(base)
	if len(cands) != 1 {
		t.Fatalf("restored candidates = %d, want 1", len(cands))
	}
	assertCandidate(t, cands[0], observed, false)

	store.markEmitted(cands)

	if got := store.snapshot(base.Add(4 * time.Minute)); len(got) != 0 {
		t.Fatalf("restored entry re-emitted: %+v", got)
	}
	if got := store.snapshot(base.Add(time.Hour)); len(got) != 0 {
		t.Fatalf("restored entry re-emitted later: %+v", got)
	}
}

func TestMarkEmitted_ClearsPendingOnlyIfReceivedAtUnchanged(t *testing.T) {
	base := keepaliveBase()
	store := newMetricStore()

	store.upsertAt(gaugeDatapoint("cpu_utilization", 50, base), base)
	cands := store.snapshot(base)
	if len(cands) != 1 {
		t.Fatalf("initial candidates = %d, want 1", len(cands))
	}

	replacement := base.Add(time.Second)
	store.upsertAt(gaugeDatapoint("cpu_utilization", 50, replacement), replacement)

	store.markEmitted(cands)

	after := store.snapshot(base.Add(2 * time.Second))
	if len(after) != 1 {
		t.Fatalf("after replacement candidates = %d, want 1", len(after))
	}
	assertCandidate(t, after[0], replacement, false)
}
