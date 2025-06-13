package redfish

import (
	"context"
	"fmt"
	"senhub-agent.go/internal/agent/services/data_store"
	"senhub-agent.go/internal/agent/tags"
	"time"
)

// collectThermalMetrics gathers thermal metrics (temperatures, fans)
func (c *GenericCollector) collectThermalMetrics(ctx context.Context, timestamp time.Time) ([]data_store.DataPoint, error) {
	if len(c.chassis) == 0 {
		return nil, fmt.Errorf("no chassis found")
	}

	var datapoints []data_store.DataPoint

	// Get system name for host tag
	var hostName string
	if len(c.systems) > 0 {
		sysResp, err := c.client.Get(ctx, c.systems[0])
		if err == nil && sysResp.Name != "" {
			hostName = sysResp.Name
		}
	}

	// Collect metrics for each chassis
	for _, chassisPath := range c.chassis {
		// Skip thermal data collection - thermal metrics disabled for consistency
		// thermalPath := chassisPath + "/Thermal"
		// resp, err := c.client.Get(ctx, thermalPath)

		// Extract chassis ID and name for tags - follow REDFISH-TAGS.md conventions
		chassisResp, err := c.client.Get(ctx, chassisPath)
		if err != nil {
			c.logger.Warn().
				Err(err).
				Str("path", chassisPath).
				Msg("Failed to get chassis details")
		}

		// Create common chassis tags using helper function
		chassisTags := c.createChassisBaseTags(chassisResp)

		// Add host tag if available
		if hostName != "" {
			chassisTags = append(chassisTags, tags.Tag{Key: "host", Value: hostName})
		}

		// Skip thermal metrics processing to avoid inconsistent naming issues
		// Temperature and fan metrics are disabled for consistency across all strategies
		// 
		// // Process temperature sensors
		// for i, temp := range resp.Temperatures {
		//     // Temperature processing code removed for consistency
		// }
		//
		// // Process fans  
		// for i, fan := range resp.Fans {
		//     // Fan processing code removed for consistency
		// }
	}

	return datapoints, nil
}