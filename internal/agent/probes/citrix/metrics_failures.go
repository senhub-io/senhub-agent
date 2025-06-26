package citrix

import (
	"context"
	"time"

	"senhub-agent.go/internal/agent/types/datapoint"
	"senhub-agent.go/internal/agent/tags"
)

// CollectFailureMetrics collects all failure-related metrics
func (mc *MetricsCollector) CollectFailureMetrics(ctx context.Context, timestamp time.Time) ([]datapoint.DataPoint, error) {
	mc.logger.Debug().Msg("Collecting failure metrics")
	
	var metrics []datapoint.DataPoint
	
	// Get connection failures from last hour
	oneHourAgo := timestamp.Add(-1 * time.Hour)
	
	// 1. Connection failures
	connectionFailures, err := mc.client.GetConnectionFailureLogs(ctx, oneHourAgo)
	if err != nil {
		mc.logger.Warn().Err(err).Msg("Failed to get connection failures")
		// Don't return error, continue with other metrics
	} else {
		connectionFailureMetric := datapoint.DataPoint{
			Name:      "connection_failures_count",
			Value:     float32(len(connectionFailures)),
			Timestamp: timestamp,
			Tags: []tags.Tag{
				{Key: "metric_type", Value: "failures"},
			},
		}
		metrics = append(metrics, connectionFailureMetric)
	}
	
	// 2. Black hole machines (machines with sessions but faulty)
	blackHoleMetric, err := mc.calculateBlackHoleMachines(ctx, timestamp)
	if err != nil {
		mc.logger.Warn().Err(err).Msg("Failed to calculate black hole machines")
	} else {
		metrics = append(metrics, blackHoleMetric)
	}
	
	mc.logger.Debug().Int("metrics_count", len(metrics)).Msg("Failure metrics collected")
	return metrics, nil
}

// calculateBlackHoleMachines finds machines with sessions but FaultState != 1
func (mc *MetricsCollector) calculateBlackHoleMachines(ctx context.Context, timestamp time.Time) (datapoint.DataPoint, error) {
	// Get all machines
	machines, err := mc.client.GetMachines(ctx, time.Time{})
	if err != nil {
		return datapoint.DataPoint{}, err
	}
	
	// Count machines that have sessions but are faulty
	count := 0
	for _, machine := range machines {
		if machine.SessionCount > 0 && machine.FaultState != FaultStateNone {
			count++
			mc.logger.Debug().
				Str("machine", machine.MachineName).
				Int("sessions", machine.SessionCount).
				Int("fault_state", machine.FaultState).
				Msg("Found black hole machine")
		}
	}
	
	return datapoint.DataPoint{
		Name:      "black_hole_machines_count",
		Value:     float32(count),
		Timestamp: timestamp,
		Tags: []tags.Tag{
			{Key: "metric_type", Value: "failures"},
		},
	}, nil
}

// TODO: Implement application_failures_count when we have ApplicationErrors endpoint