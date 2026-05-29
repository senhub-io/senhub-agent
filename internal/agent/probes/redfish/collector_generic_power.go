package redfish

import (
	"context"
	"encoding/json"
	"fmt"
	"senhub-agent.go/probesdk/datastore"
	"senhub-agent.go/probesdk/tags"
	"strings"
	"time"
)

// collectPowerMetrics gathers power-related metrics
func (c *GenericCollector) collectPowerMetrics(ctx context.Context, timestamp time.Time) ([]data_store.DataPoint, error) {
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
		// Get power data for this chassis
		powerPath := chassisPath + "/Power"
		resp, err := c.client.Get(ctx, powerPath)
		if err != nil {
			c.logger.Warn().
				Err(err).
				Str("path", powerPath).
				Msg("Failed to get power data, skipping")
			continue
		}

		// Extract chassis ID and name for tags - follow REDFISH-TAGS.md conventions
		chassisResp, err := c.client.Get(ctx, chassisPath)
		if err != nil {
			c.logger.Warn().
				Err(err).
				Str("path", chassisPath).
				Msg("Failed to get chassis details")
			continue // Skip this chassis if we can't get its details
		}

		// Create common chassis tags using helper function
		chassisTags := c.createChassisBaseTags(chassisResp)

		// Add host tag if available
		if hostName != "" {
			chassisTags = append(chassisTags, tags.Tag{Key: "host", Value: hostName})
		}

		// Process power supplies
		for i, psu := range resp.PowerSupplies {
			var psuReading struct {
				Name               string  `json:"Name"`
				PowerOutputWatts   float32 `json:"PowerOutputWatts"`
				LineInputVoltage   float32 `json:"LineInputVoltage"`
				PowerCapacityWatts float32 `json:"PowerCapacityWatts"`
				Model              string  `json:"Model"`
				Manufacturer       string  `json:"Manufacturer"`
				SerialNumber       string  `json:"SerialNumber"`
				Status             *Status `json:"Status"`
				FirmwareVersion    string  `json:"FirmwareVersion"`
				InputRanges        string  `json:"InputRanges"`
				EfficiencyPercent  float32 `json:"EfficiencyPercent"`
				Location           struct {
					PartLocation struct {
						ServiceLabel string `json:"ServiceLabel"`
					} `json:"PartLocation"`
				} `json:"Location"`
			}
			rawJSON, _ := json.Marshal(psu)
			if err := json.Unmarshal(rawJSON, &psuReading); err != nil {
				c.logger.Warn().
					Err(err).
					Int("index", i).
					Msg("Failed to parse power supply data")
				continue
			}

			// Create PSU-specific tags following REDFISH-TAGS.md conventions
			psuTags := append([]tags.Tag{}, chassisTags...)
			psuTags = append(psuTags, tags.Tag{Key: "psu_name", Value: psuReading.Name})

			// Try to detect controller from PSU name
			if strings.Contains(strings.ToLower(psuReading.Name), "left") ||
				strings.Contains(strings.ToLower(psuReading.Name), "a") {
				psuTags = append(psuTags, tags.Tag{Key: "controller", Value: "A"})
			} else if strings.Contains(strings.ToLower(psuReading.Name), "right") ||
				strings.Contains(strings.ToLower(psuReading.Name), "b") {
				psuTags = append(psuTags, tags.Tag{Key: "controller", Value: "B"})
			}

			if psuReading.Model != "" {
				psuTags = append(psuTags, tags.Tag{Key: "model", Value: psuReading.Model})
			}
			if psuReading.Manufacturer != "" {
				psuTags = append(psuTags, tags.Tag{Key: "manufacturer", Value: psuReading.Manufacturer})
			}
			if psuReading.SerialNumber != "" {
				psuTags = append(psuTags, tags.Tag{Key: "serial_number", Value: psuReading.SerialNumber})
			}

			// Add service label if available
			if psuReading.Location.PartLocation.ServiceLabel != "" {
				psuTags = append(psuTags, tags.Tag{Key: "service_label", Value: psuReading.Location.PartLocation.ServiceLabel})
			}

			// Add firmware version if available
			if psuReading.FirmwareVersion != "" {
				psuTags = append(psuTags, tags.Tag{Key: "psu_firmware", Value: psuReading.FirmwareVersion})
			}

			// Add input ranges if available
			if psuReading.InputRanges != "" {
				psuTags = append(psuTags, tags.Tag{Key: "input_ranges", Value: psuReading.InputRanges})
			}

			// Add PSU power output (if available)
			if psuReading.PowerOutputWatts > 0 {
				datapoints = append(datapoints, data_store.DataPoint{
					Name:      "hardware.power.usage",
					Timestamp: timestamp,
					Value:     psuReading.PowerOutputWatts,
					Tags:      psuTags,
				})
			}

			// Add PSU input voltage (if available)
			if psuReading.LineInputVoltage > 0 {
				datapoints = append(datapoints, data_store.DataPoint{
					Name:      "hardware.power.input_voltage",
					Timestamp: timestamp,
					Value:     psuReading.LineInputVoltage,
					Tags:      psuTags,
				})
			}

			// Add PSU capacity (if available)
			if psuReading.PowerCapacityWatts > 0 {
				datapoints = append(datapoints, data_store.DataPoint{
					Name:      "hardware.power.limit",
					Timestamp: timestamp,
					Value:     psuReading.PowerCapacityWatts,
					Tags:      psuTags,
				})
			}

			// Add health state if available
			if psuReading.Status != nil {
				health := mapHealthState(psuReading.Status.Health)
				datapoints = append(datapoints, data_store.DataPoint{
					Name:      "hardware.power.health",
					Timestamp: timestamp,
					Value:     float32(health),
					Tags:      psuTags,
				})
			}
		}

		// Process power control (overall power consumption)
		for i, pc := range resp.PowerControl {
			var pcReading struct {
				PowerConsumedWatts  float32 `json:"PowerConsumedWatts"`
				PowerRequestedWatts float32 `json:"PowerRequestedWatts"`
				PowerAvailableWatts float32 `json:"PowerAvailableWatts"`
				PowerCapacityWatts  float32 `json:"PowerCapacityWatts"`
			}
			rawJSON, _ := json.Marshal(pc)
			if err := json.Unmarshal(rawJSON, &pcReading); err != nil {
				c.logger.Warn().
					Err(err).
					Int("index", i).
					Msg("Failed to parse power control data")
				continue
			}

			// Add power consumption
			if pcReading.PowerConsumedWatts > 0 {
				datapoints = append(datapoints, data_store.DataPoint{
					Name:      "hardware.power.consumption",
					Timestamp: timestamp,
					Value:     pcReading.PowerConsumedWatts,
					Tags:      chassisTags,
				})
			}

			// Add power capacity
			if pcReading.PowerCapacityWatts > 0 {
				datapoints = append(datapoints, data_store.DataPoint{
					Name:      "hardware.power.capacity",
					Timestamp: timestamp,
					Value:     pcReading.PowerCapacityWatts,
					Tags:      chassisTags,
				})
			}
		}
	}

	return datapoints, nil
}
