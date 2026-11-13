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
	"senhub-agent.go/internal/agent/services/exporthook"
	"senhub-agent.go/internal/agent/tags"
	"senhub-agent.go/internal/agent/types/datapoint"
)

// TestPushMetrics_HandsSeriesToTheObserver checks a registered observer
// sees each exported series once, after the global tags are stripped.
func TestPushMetrics_HandsSeriesToTheObserver(t *testing.T) {
	const probe = "series-hook-probe"
	var mu sync.Mutex
	var seen []exporthook.SeriesPoint
	exporthook.RegisterSeriesObserver(func(output string, pts []exporthook.SeriesPoint, _ time.Time) {
		mu.Lock()
		defer mu.Unlock()
		for _, p := range pts {
			if output == "otlp" && p.Probe == probe {
				seen = append(seen, p)
			}
		}
	})

	store := newMetricStore()
	store.upsert(datapoint.DataPoint{Name: "cpu_user_time", Value: 1, Tags: []tags.Tag{
		{Key: "probe_name", Value: probe},
		{Key: "probe_type", Value: "cpu"},
		{Key: "site", Value: "paris"},
	}})
	defs := &fakeDefs{defs: map[string]*transformers.ProbeDefinition{"cpu": newCounterDef()}}

	_, err := pushMetrics(context.Background(), store, defs, resource.NewSchemaless(), "",
		time.Now(), time.Now(), otelmapper.DefaultResolveOptions(), map[string]bool{"site": true}, nil,
		func(context.Context, *metricdata.ResourceMetrics) error { return nil }, nil, 1)
	if err != nil {
		t.Fatalf("pushMetrics: %v", err)
	}

	mu.Lock()
	defer mu.Unlock()
	if len(seen) != 1 {
		t.Fatalf("observer saw %d series of %s, want 1", len(seen), probe)
	}
	if _, ok := seen[0].Attributes["site"]; ok {
		t.Error("a global tag rides on the resource and must not count in the series")
	}
	if seen[0].Name == "" {
		t.Error("series without a name")
	}
}

// TestPushMetrics_FailedExportIsNotObserved checks that a failed export
// does not hand any series to the observer.
func TestPushMetrics_FailedExportIsNotObserved(t *testing.T) {
	const probe = "series-hook-failed"
	var mu sync.Mutex
	var seen []exporthook.SeriesPoint
	exporthook.RegisterSeriesObserver(func(output string, pts []exporthook.SeriesPoint, _ time.Time) {
		mu.Lock()
		defer mu.Unlock()
		for _, p := range pts {
			if output == "otlp" && p.Probe == probe {
				seen = append(seen, p)
			}
		}
	})

	store := newMetricStore()
	store.upsert(datapoint.DataPoint{Name: "cpu_user_time", Value: 1, Tags: []tags.Tag{
		{Key: "probe_name", Value: probe},
		{Key: "probe_type", Value: "cpu"},
		{Key: "site", Value: "paris"},
	}})
	defs := &fakeDefs{defs: map[string]*transformers.ProbeDefinition{"cpu": newCounterDef()}}

	_, err := pushMetrics(context.Background(), store, defs, resource.NewSchemaless(), "",
		time.Now(), time.Now(), otelmapper.DefaultResolveOptions(), map[string]bool{"site": true}, nil,
		func(context.Context, *metricdata.ResourceMetrics) error {
			return errors.New("collector down")
		}, nil, 1)
	if err == nil {
		t.Fatal("pushMetrics should return an error when export fails")
	}

	mu.Lock()
	defer mu.Unlock()
	if len(seen) != 0 {
		t.Fatalf("observer saw %d series of %s after a failed export, want 0", len(seen), probe)
	}
}

// TestExportInParallel_ObservesOnlyExportedPartitions checks the callback
// receives only partitions whose export succeeded.
func TestExportInParallel_ObservesOnlyExportedPartitions(t *testing.T) {
	parts := map[string][]otelmapper.OtelRecord{
		"ok": {{Name: "a", Attributes: map[string]string{"probe_name": "ok"}}},
		"ko": {{Name: "b", Attributes: map[string]string{"probe_name": "ko"}}},
	}
	exportFn := func(_ context.Context, rm *metricdata.ResourceMetrics) error {
		for _, scopeMetrics := range rm.ScopeMetrics {
			for _, metric := range scopeMetrics.Metrics {
				if metric.Name == "b" {
					return errors.New("down")
				}
			}
		}
		return nil
	}
	var observed []string
	_, err := exportInParallel(context.Background(), parts,
		resource.NewSchemaless(), "", time.Now(), time.Now(), exportFn, 2,
		func(recs []otelmapper.OtelRecord) {
			for _, r := range recs {
				observed = append(observed, r.Name)
			}
		})
	if err == nil {
		t.Fatal("exportInParallel should return an error when one partition fails")
	}
	if len(observed) != 1 || observed[0] != "a" {
		t.Errorf("observed=%v, want [a]", observed)
	}
}
