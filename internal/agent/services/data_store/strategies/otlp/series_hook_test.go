package otlp

import (
	"context"
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
