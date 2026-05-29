package citrix

import (
	"context"
	"time"

	"senhub-agent.go/probesdk/datapoint"
	"senhub-agent.go/probesdk/tags"
)

// CollectLoadMetrics collects load index metrics from all machines.
// Load indexes are reported by each VDA and indicate resource utilization
// on a 0-10000 scale (0 = idle, 10000 = full load).
func (mc *MetricsCollector) CollectLoadMetrics(ctx context.Context, timestamp time.Time) ([]datapoint.DataPoint, error) {
	mc.logger.Debug().Msg("Collecting load index metrics")

	loadIndexes, err := mc.client.GetLoadIndexes(ctx)
	if err != nil {
		// LoadIndexes endpoint may not exist on all CVAD versions — skip entirely
		mc.logger.Debug().Err(err).Msg("LoadIndexes endpoint unavailable - skipping load metrics")
		return nil, nil
	}

	if len(loadIndexes) == 0 {
		mc.logger.Debug().Msg("No load index data available")
		return mc.createZeroLoadMetrics(timestamp), nil
	}

	// Aggregate across all machines
	var (
		totalEffective int
		totalCpu       int
		totalMemory    int
		totalDisk      int
		totalNetwork   int
		totalSessions  int
		overloaded     int
	)

	for _, li := range loadIndexes {
		totalEffective += li.EffectiveLoadIndex
		totalCpu += li.Cpu
		totalMemory += li.Memory
		totalDisk += li.Disk
		totalNetwork += li.Network
		totalSessions += li.SessionCount

		if li.EffectiveLoadIndex >= LoadIndexOverloaded {
			overloaded++
		}
	}

	count := len(loadIndexes)

	// Convert from 0-10000 scale to percentage (0-100)
	avgEffective := float32(totalEffective) / float32(count) / 100.0
	avgCpu := float32(totalCpu) / float32(count) / 100.0
	avgMemory := float32(totalMemory) / float32(count) / 100.0
	avgDisk := float32(totalDisk) / float32(count) / 100.0
	avgNetwork := float32(totalNetwork) / float32(count) / 100.0
	avgSessions := float32(totalSessions) / float32(count) / 100.0

	loadTag := tags.Tag{Key: "metric_type", Value: "load_index"}

	metrics := []datapoint.DataPoint{
		{
			Name:      MetricLoadIndexEffective,
			Value:     roundToTwoDecimals(avgEffective),
			Timestamp: timestamp,
			Tags:      []tags.Tag{loadTag},
		},
		{
			Name:      MetricLoadIndexCpu,
			Value:     roundToTwoDecimals(avgCpu),
			Timestamp: timestamp,
			Tags:      []tags.Tag{loadTag},
		},
		{
			Name:      MetricLoadIndexMemory,
			Value:     roundToTwoDecimals(avgMemory),
			Timestamp: timestamp,
			Tags:      []tags.Tag{loadTag},
		},
		{
			Name:      MetricLoadIndexDisk,
			Value:     roundToTwoDecimals(avgDisk),
			Timestamp: timestamp,
			Tags:      []tags.Tag{loadTag},
		},
		{
			Name:      MetricLoadIndexNetwork,
			Value:     roundToTwoDecimals(avgNetwork),
			Timestamp: timestamp,
			Tags:      []tags.Tag{loadTag},
		},
		{
			Name:      MetricLoadIndexSessions,
			Value:     roundToTwoDecimals(avgSessions),
			Timestamp: timestamp,
			Tags:      []tags.Tag{loadTag},
		},
		{
			Name:      MetricLoadOverloaded,
			Value:     float32(overloaded),
			Timestamp: timestamp,
			Tags:      []tags.Tag{loadTag},
		},
	}

	mc.logger.Debug().
		Int("machines_count", count).
		Float32("avg_effective_pct", avgEffective).
		Float32("avg_cpu_pct", avgCpu).
		Float32("avg_memory_pct", avgMemory).
		Int("overloaded_machines", overloaded).
		Msg("Load index metrics calculated")

	return metrics, nil
}

// createZeroLoadMetrics creates zero-value load metrics when no data is available
func (mc *MetricsCollector) createZeroLoadMetrics(timestamp time.Time) []datapoint.DataPoint {
	loadTag := tags.Tag{Key: "metric_type", Value: "load_index"}

	names := []string{
		MetricLoadIndexEffective,
		MetricLoadIndexCpu,
		MetricLoadIndexMemory,
		MetricLoadIndexDisk,
		MetricLoadIndexNetwork,
		MetricLoadIndexSessions,
		MetricLoadOverloaded,
	}

	metrics := make([]datapoint.DataPoint, 0, len(names))
	for _, name := range names {
		metrics = append(metrics, datapoint.DataPoint{
			Name:      name,
			Value:     0,
			Timestamp: timestamp,
			Tags:      []tags.Tag{loadTag},
		})
	}

	return metrics
}
