package redfish

import (
	"context"
	"fmt"
	"senhub-agent.go/internal/agent/services/data_store"
	"time"
)

// collectThermalMetrics gathers thermal metrics (temperatures, fans)
func (c *GenericCollector) collectThermalMetrics(ctx context.Context, timestamp time.Time) ([]data_store.DataPoint, error) {
	if len(c.chassis) == 0 {
		return nil, fmt.Errorf("no chassis found")
	}

	var datapoints []data_store.DataPoint

	// Thermal metrics are intentionally disabled for consistency across all strategies
	// Temperature and fan metrics collection has been removed to avoid naming inconsistencies
	// This function returns empty datapoints as thermal monitoring is handled differently

	return datapoints, nil
}
