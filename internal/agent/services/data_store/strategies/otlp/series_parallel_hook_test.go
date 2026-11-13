package otlp

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"go.opentelemetry.io/otel/sdk/metric/metricdata"
	"go.opentelemetry.io/otel/sdk/resource"

	"senhub-agent.go/internal/agent/services/data_store/otelmapper"
	"senhub-agent.go/internal/agent/services/exporthook"
)

func TestPushMetrics_ParallelPathObservesExportedPartitions(t *testing.T) {
	store := newMetricStore()

	var mu sync.Mutex
	counts := map[string]int{}
	exporthook.RegisterSeriesObserver(func(output string, pts []exporthook.SeriesPoint, _ time.Time) {
		if output != "otlp" {
			return
		}
		mu.Lock()
		defer mu.Unlock()
		for _, p := range pts {
			if strings.HasPrefix(p.Probe, "par-") {
				counts[p.Probe]++
			}
		}
	})

	export := func(_ context.Context, rm *metricdata.ResourceMetrics) error {
		for i := range rm.ScopeMetrics {
			for j := range rm.ScopeMetrics[i].Metrics {
				name := rm.ScopeMetrics[i].Metrics[j].Name
				if strings.Contains(name, "par-ko") {
					return errors.New("down")
				}
			}
		}
		return nil
	}

	var extra []otelmapper.OtelRecord
	for _, probe := range []string{"par-ok-1", "par-ok-2", "par-ko"} {
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

	_, err := pushMetrics(context.Background(), store, fakeNullDefs{},
		resource.NewSchemaless(), "1.0.0", time.Now(), time.Now(),
		otelmapper.DefaultResolveOptions(), nil, extra, export, nil, 4)
	if err == nil {
		t.Fatalf("pushMetrics: expected non-nil error")
	}

	mu.Lock()
	defer mu.Unlock()
	if got := counts["par-ok-1"]; got != 50 {
		t.Errorf("observer count par-ok-1=%d, want 50", got)
	}
	if got := counts["par-ok-2"]; got != 50 {
		t.Errorf("observer count par-ok-2=%d, want 50", got)
	}
	if got := counts["par-ko"]; got != 0 {
		t.Errorf("observer count par-ko=%d, want 0", got)
	}
}
