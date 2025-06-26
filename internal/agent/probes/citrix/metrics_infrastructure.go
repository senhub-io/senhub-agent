package citrix

import (
	"context"
	"time"

	"senhub-agent.go/internal/agent/types/datapoint"
	"senhub-agent.go/internal/agent/tags"
)

// CollectInfrastructureMetrics collects all infrastructure-related metrics
func (mc *MetricsCollector) CollectInfrastructureMetrics(ctx context.Context, timestamp time.Time) ([]datapoint.DataPoint, error) {
	mc.logger.Debug().Msg("Collecting infrastructure metrics")
	
	var metrics []datapoint.DataPoint
	
	// Get all machines data (no time filter for current state metrics)
	machines, err := mc.client.GetMachines(ctx, time.Time{})
	if err != nil {
		mc.logger.Error().Err(err).Msg("Failed to get machines for infrastructure metrics")
		return nil, err
	}
	
	mc.logger.Debug().Int("machines_count", len(machines)).Msg("Retrieved machines for infrastructure metrics")
	
	// Calculate metrics directly inline for better performance
	var (
		totalMachines      = len(machines)
		registeredCount    = 0
		unregisteredCount  = 0
		faultyCount        = 0
		maintenanceCount   = 0
	)
	
	// Single pass through machines for all calculations
	for _, machine := range machines {
		switch machine.RegistrationState {
		case RegistrationStateRegistered:
			registeredCount++
		case RegistrationStateUnregistered:
			unregisteredCount++
		}
		
		if machine.FaultState != FaultStateNone {
			faultyCount++
		}
		
		if machine.InMaintenanceMode {
			maintenanceCount++
		}
	}
	
	// Create all metrics with proper units
	metrics = []datapoint.DataPoint{
		{
			Name:      "machines_total",
			Value:     float32(totalMachines),
			Timestamp: timestamp,
			Tags: []tags.Tag{
				{Key: "metric_type", Value: "infrastructure"},
			},
		},
		{
			Name:      "machines_registered",
			Value:     float32(registeredCount),
			Timestamp: timestamp,
			Tags: []tags.Tag{
				{Key: "metric_type", Value: "infrastructure"},
			},
		},
		{
			Name:      "unregistered_vda_count",
			Value:     float32(unregisteredCount),
			Timestamp: timestamp,
			Tags: []tags.Tag{
				{Key: "metric_type", Value: "infrastructure"},
			},
		},
		{
			Name:      "machines_faulty",
			Value:     float32(faultyCount),
			Timestamp: timestamp,
			Tags: []tags.Tag{
				{Key: "metric_type", Value: "infrastructure"},
			},
		},
		{
			Name:      "machines_maintenance",
			Value:     float32(maintenanceCount),
			Timestamp: timestamp,
			Tags: []tags.Tag{
				{Key: "metric_type", Value: "infrastructure"},
			},
		},
	}
	
	mc.logger.Info().
		Int("total", totalMachines).
		Int("registered", registeredCount).
		Int("unregistered", unregisteredCount).
		Int("faulty", faultyCount).
		Int("maintenance", maintenanceCount).
		Msg("✅ Infrastructure metrics calculated")
	
	return metrics, nil
}