package otlp

import (
	"math"
	"testing"
	"time"

	"senhub-agent.go/internal/agent/services/agentstate"
	"senhub-agent.go/internal/agent/tags"
	"senhub-agent.go/internal/agent/types/datapoint"
)

func TestCheckpointer_NonFiniteValuesSkipped(t *testing.T) {
	t.Run("NaN value skipped, finite restored", func(t *testing.T) {
		dir := t.TempDir()
		store := newMetricStore()
		store.upsert(datapoint.DataPoint{
			Name:      "nan_metric",
			Value:     math.NaN(),
			Timestamp: time.Now(),
			Tags: []tags.Tag{
				{Key: "probe_name", Value: "p"},
				{Key: "probe_type", Value: "t"},
			},
		})
		store.upsert(datapoint.DataPoint{
			Name:      "finite_metric",
			Value:     1.0,
			Timestamp: time.Now(),
			Tags: []tags.Tag{
				{Key: "probe_name", Value: "p"},
				{Key: "probe_type", Value: "t"},
			},
		})

		c := newCheckpointer(checkpointConfig{Path: dir, Interval: time.Hour}, store, nil)
		if err := c.save(); err != nil {
			t.Fatalf("save: %v", err)
		}

		fresh := newMetricStore()
		c2 := newCheckpointer(checkpointConfig{Path: dir, Interval: time.Hour}, fresh, nil)
		n, err := c2.loadAndRestore()
		if err != nil {
			t.Fatalf("loadAndRestore: %v", err)
		}
		if n != 1 {
			t.Errorf("restored count=%d, want 1", n)
		}
		if fresh.size() != 1 {
			t.Errorf("fresh store size=%d, want 1", fresh.size())
		}
		cms := snapshotMetrics(fresh, time.Now())
		if len(cms) != 1 || cms[0].MetricName != "finite_metric" {
			t.Errorf("restored metrics=%v, want only finite_metric", cms)
		}
	})

	t.Run("+Inf value skipped", func(t *testing.T) {
		dir := t.TempDir()
		store := newMetricStore()
		store.upsert(datapoint.DataPoint{
			Name:      "inf_metric",
			Value:     math.Inf(1),
			Timestamp: time.Now(),
			Tags: []tags.Tag{
				{Key: "probe_name", Value: "p"},
				{Key: "probe_type", Value: "t"},
			},
		})
		store.upsert(datapoint.DataPoint{
			Name:      "ok_metric",
			Value:     2.0,
			Timestamp: time.Now(),
			Tags: []tags.Tag{
				{Key: "probe_name", Value: "p"},
				{Key: "probe_type", Value: "t"},
			},
		})

		c := newCheckpointer(checkpointConfig{Path: dir, Interval: time.Hour}, store, nil)
		if err := c.save(); err != nil {
			t.Fatalf("save: %v", err)
		}

		fresh := newMetricStore()
		c2 := newCheckpointer(checkpointConfig{Path: dir, Interval: time.Hour}, fresh, nil)
		n, err := c2.loadAndRestore()
		if err != nil {
			t.Fatalf("loadAndRestore: %v", err)
		}
		if n != 1 || fresh.size() != 1 {
			t.Errorf("restored count=%d, store size=%d, want 1", n, fresh.size())
		}
	})

	t.Run("histogram with NaN Sum skipped", func(t *testing.T) {
		dir := t.TempDir()
		store := newMetricStore()
		sum := math.NaN()
		minV := 0.1
		store.upsert(datapoint.DataPoint{
			Name:  "http.server.duration",
			Value: 1,
			Tags: []tags.Tag{
				{Key: "probe_name", Value: "edge_in"},
				{Key: "probe_type", Value: "otlp_receiver"},
				{Key: "unit", Value: "s"},
			},
			Histogram: &datapoint.HistogramValue{
				Count:          1,
				Sum:            &sum,
				Min:            &minV,
				BucketCounts:   []uint64{1},
				ExplicitBounds: []float64{0.5},
			},
		})

		before := agentstate.GetOTLPCheckpointErrorsByStage()["non_finite_skipped"]
		c := newCheckpointer(checkpointConfig{Path: dir, Interval: time.Hour}, store, nil)
		if err := c.save(); err != nil {
			t.Fatalf("save: %v", err)
		}
		after := agentstate.GetOTLPCheckpointErrorsByStage()["non_finite_skipped"]
		if after <= before {
			t.Errorf("non_finite_skipped counter did not increase: before=%d after=%d", before, after)
		}

		fresh := newMetricStore()
		c2 := newCheckpointer(checkpointConfig{Path: dir, Interval: time.Hour}, fresh, nil)
		n, err := c2.loadAndRestore()
		if err != nil {
			t.Fatalf("loadAndRestore: %v", err)
		}
		if n != 0 || fresh.size() != 0 {
			t.Errorf("restored count=%d, store size=%d, want 0", n, fresh.size())
		}
	})
}
