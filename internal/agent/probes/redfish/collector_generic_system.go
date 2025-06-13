package redfish

import (
	"context"
	"encoding/json"
	"fmt"
	"senhub-agent.go/internal/agent/services/data_store"
	"senhub-agent.go/internal/agent/tags"
	"time"
)

// collectSystemMetrics gathers system-level metrics
func (c *GenericCollector) collectSystemMetrics(ctx context.Context, timestamp time.Time) ([]data_store.DataPoint, error) {
	if len(c.systems) == 0 {
		return nil, fmt.Errorf("no systems found")
	}

	var datapoints []data_store.DataPoint

	// Collect metrics for each system
	for _, systemPath := range c.systems {
		resp, err := c.client.Get(ctx, systemPath)
		if err != nil {
			return nil, fmt.Errorf("failed to get system details: %v", err)
		}

		// Use configured endpoint for tags - this is the actual endpoint URL
		// resp.Name contains the system name (like "CN0TYNP0SGW004AS000NA00") 
		// but we want the configured endpoint URL for the endpoint tag
		endpoint := c.endpoint

		// Create base system tags using helper function
		systemTags := c.createBaseSystemTags(resp, endpoint)
		
		// Add additional system-specific tags
		if resp.SKU != "" {
			systemTags = append(systemTags, tags.Tag{Key: "sku", Value: resp.SKU})
		}
		if resp.PartNumber != "" {
			systemTags = append(systemTags, tags.Tag{Key: "part_number", Value: resp.PartNumber})
		}
		// Note: IndicatorLED might not be available in all Redfish implementations
		var rawData map[string]interface{}
		if err := json.Unmarshal(resp.Raw, &rawData); err == nil {
			if ledValue, ok := rawData["IndicatorLED"].(string); ok && ledValue != "" {
				systemTags = append(systemTags, tags.Tag{Key: "indicator_led", Value: ledValue})
			}
		}
		
		// Add collection tag for system metrics
		systemTagsWithCollection := addCollectionTag(systemTags, "system")

		// Add health state metric
		if resp.Status != nil {
			health := mapHealthState(resp.Status.Health)
			datapoints = append(datapoints, data_store.DataPoint{
				Name:      "hardware.system.health",
				Timestamp: timestamp,
				Value:     float32(health),
				Tags:      systemTagsWithCollection,
			})
		}

		// Add power state metric
		if resp.PowerState != "" {
			powerState := mapPowerState(resp.PowerState)
			datapoints = append(datapoints, data_store.DataPoint{
				Name:      "hardware.system.power.state",
				Timestamp: timestamp,
				Value:     float32(powerState),
				Tags:      systemTagsWithCollection,
			})
		}

		// Extract processor summary if available
		if resp.ProcessorSummary != nil {
			// Convert to a concrete type for easier access
			var procSummary struct {
				Count  int     `json:"Count"`
				Model  string  `json:"Model"`
				Status *Status `json:"Status"`
			}
			rawJSON, _ := json.Marshal(resp.ProcessorSummary)
			if err := json.Unmarshal(rawJSON, &procSummary); err == nil {
				// Create processor-specific tags with collection
				processorTags := addCollectionTag(systemTags, "processor")
				
				datapoints = append(datapoints, data_store.DataPoint{
					Name:      "hardware.system.cpu.count",
					Timestamp: timestamp,
					Value:     float32(procSummary.Count),
					Tags:      processorTags,
				})

				// Add processor health if available
				if procSummary.Status != nil && procSummary.Status.Health != "" {
					datapoints = append(datapoints, data_store.DataPoint{
						Name:      "hardware.system.cpu.health",
						Timestamp: timestamp,
						Value:     float32(mapHealthState(procSummary.Status.Health)),
						Tags:      processorTags,
					})
				}
			}
		}

		// Extract memory summary if available
		if resp.MemorySummary != nil {
			// Convert to a concrete type for easier access
			var memSummary struct {
				TotalSystemMemoryGiB float32 `json:"TotalSystemMemoryGiB"`
				Status               *Status `json:"Status"`
			}
			rawJSON, _ := json.Marshal(resp.MemorySummary)
			if err := json.Unmarshal(rawJSON, &memSummary); err == nil {
				// Create memory-specific tags with collection
				memoryTags := addCollectionTag(systemTags, "memory")
				
				datapoints = append(datapoints, data_store.DataPoint{
					Name:      "hardware.system.memory.size",
					Timestamp: timestamp,
					Value:     memSummary.TotalSystemMemoryGiB,
					Tags:      memoryTags,
				})

				// Add memory health if available
				if memSummary.Status != nil && memSummary.Status.Health != "" {
					datapoints = append(datapoints, data_store.DataPoint{
						Name:      "hardware.system.memory.health",
						Timestamp: timestamp,
						Value:     float32(mapHealthState(memSummary.Status.Health)),
						Tags:      memoryTags,
					})
				}
			}
		}
	}

	return datapoints, nil
}