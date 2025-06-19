package redfish

import (
	"context"
	"encoding/json"
	"fmt"
	"senhub-agent.go/internal/agent/services/data_store"
	"senhub-agent.go/internal/agent/tags"
	"time"
)

// collectProcessorMetrics gathers processor metrics
func (c *GenericCollector) collectProcessorMetrics(ctx context.Context, timestamp time.Time) ([]data_store.DataPoint, error) {
	if len(c.systems) == 0 {
		return nil, fmt.Errorf("no systems found")
	}

	var datapoints []data_store.DataPoint

	// Collect metrics for each system
	for _, systemPath := range c.systems {
		// Get processors collection
		processorsPath := systemPath + "/Processors"
		resp, err := c.client.Get(ctx, processorsPath)
		if err != nil {
			c.logger.Warn().
				Err(err).
				Str("path", processorsPath).
				Msg("Failed to get processors collection, skipping")
			continue
		}

		// Get system details for tags
		sysResp, err := c.client.Get(ctx, systemPath)
		if err != nil {
			c.logger.Warn().
				Err(err).
				Str("path", systemPath).
				Msg("Failed to get system details")
		}

		systemTags := []tags.Tag{
			{Key: "system_id", Value: sysResp.ID},
			{Key: "system_name", Value: sysResp.Name},
			{Key: "host", Value: sysResp.Name}, // Add host tag for easier correlation
		}

		// Process each processor
		for _, member := range resp.Members {
			processorPath, ok := member["@odata.id"]
			if !ok {
				continue
			}

			// Get processor details
			procResp, err := c.client.Get(ctx, processorPath)
			if err != nil {
				c.logger.Warn().
					Err(err).
					Str("path", processorPath).
					Msg("Failed to get processor details, skipping")
				continue
			}

			// Create processor-specific tags following REDFISH-TAGS.md conventions
			procTags := append([]tags.Tag{}, systemTags...)
			procTags = append(procTags, tags.Tag{Key: "processor_id", Value: procResp.ID})
			procTags = append(procTags, tags.Tag{Key: "processor_name", Value: procResp.Name})

			if procResp.Model != "" {
				procTags = append(procTags, tags.Tag{Key: "model", Value: procResp.Model})
			}
			if procResp.Manufacturer != "" {
				procTags = append(procTags, tags.Tag{Key: "manufacturer", Value: procResp.Manufacturer})
			}
			if procResp.SerialNumber != "" {
				procTags = append(procTags, tags.Tag{Key: "serial_number", Value: procResp.SerialNumber})
			}
			if procResp.PartNumber != "" {
				procTags = append(procTags, tags.Tag{Key: "part_number", Value: procResp.PartNumber})
			}
			if procResp.AssetTag != "" {
				procTags = append(procTags, tags.Tag{Key: "asset_tag", Value: procResp.AssetTag})
			}
			if procResp.SKU != "" {
				procTags = append(procTags, tags.Tag{Key: "sku", Value: procResp.SKU})
			}
			if procResp.Status != nil && procResp.Status.State != "" {
				procTags = append(procTags, tags.Tag{Key: "state", Value: procResp.Status.State})
			}

			// Extract processor metrics from response
			var procData struct {
				TotalCores            int     `json:"TotalCores"`
				TotalThreads          int     `json:"TotalThreads"`
				MaxSpeedMHz           int     `json:"MaxSpeedMHz"`
				InstructionSet        string  `json:"InstructionSet"`
				Status                *Status `json:"Status"`
				Socket                string  `json:"Socket"`
				ProcessorType         string  `json:"ProcessorType"`
				ProcessorArchitecture string  `json:"ProcessorArchitecture"`
				Links                 struct {
					Metrics struct {
						OdataID string `json:"@odata.id"`
					} `json:"Metrics"`
				} `json:"Links"`
			}
			rawJSON, _ := json.Marshal(procResp)
			if err := json.Unmarshal(rawJSON, &procData); err != nil {
				c.logger.Warn().
					Err(err).
					Msg("Failed to parse processor data")
				continue
			}

			// Add extra processor tags if available
			if procData.ProcessorType != "" {
				procTags = append(procTags, tags.Tag{Key: "processor_type", Value: procData.ProcessorType})
			}
			if procData.ProcessorArchitecture != "" {
				procTags = append(procTags, tags.Tag{Key: "architecture", Value: procData.ProcessorArchitecture})
			}
			if procData.Socket != "" {
				procTags = append(procTags, tags.Tag{Key: "socket", Value: procData.Socket})
			}
			if procData.InstructionSet != "" {
				procTags = append(procTags, tags.Tag{Key: "instruction_set", Value: procData.InstructionSet})
			}

			// Add processor core count
			if procData.TotalCores > 0 {
				datapoints = append(datapoints, data_store.DataPoint{
					Name:      "hardware.cpu.cores",
					Timestamp: timestamp,
					Value:     float32(procData.TotalCores),
					Tags:      procTags,
				})
			}

			// Add processor thread count
			if procData.TotalThreads > 0 {
				datapoints = append(datapoints, data_store.DataPoint{
					Name:      "hardware.cpu.threads",
					Timestamp: timestamp,
					Value:     float32(procData.TotalThreads),
					Tags:      procTags,
				})
			}

			// Add processor max speed
			if procData.MaxSpeedMHz > 0 {
				datapoints = append(datapoints, data_store.DataPoint{
					Name:      "hardware.cpu.max_speed",
					Timestamp: timestamp,
					Value:     float32(procData.MaxSpeedMHz),
					Tags:      append(procTags, tags.Tag{Key: "unit", Value: "MHz"}),
				})
			}

			// Add health state if available
			if procData.Status != nil {
				health := mapHealthState(procData.Status.Health)
				datapoints = append(datapoints, data_store.DataPoint{
					Name:      "hardware.cpu.health",
					Timestamp: timestamp,
					Value:     float32(health),
					Tags:      procTags,
				})
			}

			// Check for processor metrics endpoint
			metricsPath := ""
			// Try to get from Links section first
			if procData.Links.Metrics.OdataID != "" {
				metricsPath = procData.Links.Metrics.OdataID
			} else {
				// Try standard path as a fallback
				metricsPath = processorPath + "/Metrics"
			}

			if metricsPath != "" {
				// Get processor metrics
				metricsResp, err := c.client.Get(ctx, metricsPath)
				if err != nil {
					c.logger.Debug().
						Err(err).
						Str("path", metricsPath).
						Msg("ProcessorMetrics endpoint not available")
				} else {
					// Parse processor metrics data
					var metricsData struct {
						AverageFrequencyMHz float32                `json:"AverageFrequencyMHz"`
						CurrentSpeedMHz     float32                `json:"CurrentSpeedMHz"`
						ThrottlingCelsius   float32                `json:"ThrottlingCelsius"`
						TemperatureCelsius  float32                `json:"TemperatureCelsius"`
						ConsumedPowerWatt   float32                `json:"ConsumedPowerWatt"`
						ThermalMargin       float32                `json:"ThermalMargin"`
						PowerLimit          float32                `json:"PowerLimit"`
						Oem                 map[string]interface{} `json:"Oem"`
						// Utilization metrics
						CPUUtilization float32 `json:"CPUUtilization"`
						KernelPercent  float32 `json:"KernelPercent"`
						UserPercent    float32 `json:"UserPercent"`
						IOWaitPercent  float32 `json:"IOWaitPercent"`
						// Cache metrics
						CacheMetrics struct {
							L1CacheMetrics struct {
								OccupancyBytes float32 `json:"OccupancyBytes"`
								HitRatio       float32 `json:"HitRatio"`
								MissRatio      float32 `json:"MissRatio"`
							} `json:"L1CacheMetrics"`
							L2CacheMetrics struct {
								OccupancyBytes float32 `json:"OccupancyBytes"`
								HitRatio       float32 `json:"HitRatio"`
								MissRatio      float32 `json:"MissRatio"`
							} `json:"L2CacheMetrics"`
							L3CacheMetrics struct {
								OccupancyBytes float32 `json:"OccupancyBytes"`
								HitRatio       float32 `json:"HitRatio"`
								MissRatio      float32 `json:"MissRatio"`
							} `json:"L3CacheMetrics"`
						} `json:"CacheMetrics"`
					}

					rawJSON, _ := json.Marshal(metricsResp)
					if err := json.Unmarshal(rawJSON, &metricsData); err != nil {
						c.logger.Warn().
							Err(err).
							Msg("Failed to parse processor metrics data")
					} else {
						// Add current processor speed
						if metricsData.CurrentSpeedMHz > 0 {
							datapoints = append(datapoints, data_store.DataPoint{
								Name:      "hardware.cpu.current_speed",
								Timestamp: timestamp,
								Value:     metricsData.CurrentSpeedMHz,
								Tags:      append(procTags, tags.Tag{Key: "unit", Value: "MHz"}),
							})
						}

						// Add average processor frequency
						if metricsData.AverageFrequencyMHz > 0 {
							datapoints = append(datapoints, data_store.DataPoint{
								Name:      "hardware.cpu.average_frequency",
								Timestamp: timestamp,
								Value:     metricsData.AverageFrequencyMHz,
								Tags:      append(procTags, tags.Tag{Key: "unit", Value: "MHz"}),
							})
						}

						// Add processor temperature
						if metricsData.TemperatureCelsius > 0 {
							datapoints = append(datapoints, data_store.DataPoint{
								Name:      "hardware.cpu.temperature",
								Timestamp: timestamp,
								Value:     metricsData.TemperatureCelsius,
								Tags:      append(procTags, tags.Tag{Key: "unit", Value: "celsius"}),
							})
						}

						// Add processor thermal throttling temperature
						if metricsData.ThrottlingCelsius > 0 {
							datapoints = append(datapoints, data_store.DataPoint{
								Name:      "hardware.cpu.throttling_temperature",
								Timestamp: timestamp,
								Value:     metricsData.ThrottlingCelsius,
								Tags:      append(procTags, tags.Tag{Key: "unit", Value: "celsius"}),
							})
						}

						// Add thermal margin
						if metricsData.ThermalMargin > 0 {
							datapoints = append(datapoints, data_store.DataPoint{
								Name:      "hardware.cpu.thermal_margin",
								Timestamp: timestamp,
								Value:     metricsData.ThermalMargin,
								Tags:      append(procTags, tags.Tag{Key: "unit", Value: "celsius"}),
							})
						}

						// Add processor power consumption
						if metricsData.ConsumedPowerWatt > 0 {
							datapoints = append(datapoints, data_store.DataPoint{
								Name:      "hardware.cpu.power_consumption",
								Timestamp: timestamp,
								Value:     metricsData.ConsumedPowerWatt,
								Tags:      append(procTags, tags.Tag{Key: "unit", Value: "watt"}),
							})
						}

						// Add processor power limit
						if metricsData.PowerLimit > 0 {
							datapoints = append(datapoints, data_store.DataPoint{
								Name:      "hardware.cpu.power_limit",
								Timestamp: timestamp,
								Value:     metricsData.PowerLimit,
								Tags:      append(procTags, tags.Tag{Key: "unit", Value: "watt"}),
							})
						}

						// Add CPU utilization metrics
						if metricsData.CPUUtilization > 0 {
							datapoints = append(datapoints, data_store.DataPoint{
								Name:      "hardware.cpu.utilization",
								Timestamp: timestamp,
								Value:     metricsData.CPUUtilization,
								Tags:      append(procTags, tags.Tag{Key: "unit", Value: "percent"}),
							})
						}

						// Add kernel percent utilization
						if metricsData.KernelPercent > 0 {
							datapoints = append(datapoints, data_store.DataPoint{
								Name:      "hardware.cpu.kernel_percent",
								Timestamp: timestamp,
								Value:     metricsData.KernelPercent,
								Tags:      append(procTags, tags.Tag{Key: "unit", Value: "percent"}),
							})
						}

						// Add user percent utilization
						if metricsData.UserPercent > 0 {
							datapoints = append(datapoints, data_store.DataPoint{
								Name:      "hardware.cpu.user_percent",
								Timestamp: timestamp,
								Value:     metricsData.UserPercent,
								Tags:      append(procTags, tags.Tag{Key: "unit", Value: "percent"}),
							})
						}

						// Add IO wait percent
						if metricsData.IOWaitPercent > 0 {
							datapoints = append(datapoints, data_store.DataPoint{
								Name:      "hardware.cpu.io_wait_percent",
								Timestamp: timestamp,
								Value:     metricsData.IOWaitPercent,
								Tags:      append(procTags, tags.Tag{Key: "unit", Value: "percent"}),
							})
						}

						// Add cache metrics if available
						// L1 Cache
						if metricsData.CacheMetrics.L1CacheMetrics.OccupancyBytes > 0 {
							l1Tags := append(procTags, tags.Tag{Key: "cache_level", Value: "L1"}, tags.Tag{Key: "unit", Value: "bytes"})
							datapoints = append(datapoints, data_store.DataPoint{
								Name:      "hardware.cpu.cache.occupancy",
								Timestamp: timestamp,
								Value:     metricsData.CacheMetrics.L1CacheMetrics.OccupancyBytes,
								Tags:      l1Tags,
							})
						}

						if metricsData.CacheMetrics.L1CacheMetrics.HitRatio > 0 {
							l1Tags := append(procTags, tags.Tag{Key: "cache_level", Value: "L1"}, tags.Tag{Key: "unit", Value: "ratio"})
							datapoints = append(datapoints, data_store.DataPoint{
								Name:      "hardware.cpu.cache.hit_ratio",
								Timestamp: timestamp,
								Value:     metricsData.CacheMetrics.L1CacheMetrics.HitRatio,
								Tags:      l1Tags,
							})
						}

						// L2 Cache
						if metricsData.CacheMetrics.L2CacheMetrics.OccupancyBytes > 0 {
							l2Tags := append(procTags, tags.Tag{Key: "cache_level", Value: "L2"}, tags.Tag{Key: "unit", Value: "bytes"})
							datapoints = append(datapoints, data_store.DataPoint{
								Name:      "hardware.cpu.cache.occupancy",
								Timestamp: timestamp,
								Value:     metricsData.CacheMetrics.L2CacheMetrics.OccupancyBytes,
								Tags:      l2Tags,
							})
						}

						if metricsData.CacheMetrics.L2CacheMetrics.HitRatio > 0 {
							l2Tags := append(procTags, tags.Tag{Key: "cache_level", Value: "L2"}, tags.Tag{Key: "unit", Value: "ratio"})
							datapoints = append(datapoints, data_store.DataPoint{
								Name:      "hardware.cpu.cache.hit_ratio",
								Timestamp: timestamp,
								Value:     metricsData.CacheMetrics.L2CacheMetrics.HitRatio,
								Tags:      l2Tags,
							})
						}

						// L3 Cache
						if metricsData.CacheMetrics.L3CacheMetrics.OccupancyBytes > 0 {
							l3Tags := append(procTags, tags.Tag{Key: "cache_level", Value: "L3"}, tags.Tag{Key: "unit", Value: "bytes"})
							datapoints = append(datapoints, data_store.DataPoint{
								Name:      "hardware.cpu.cache.occupancy",
								Timestamp: timestamp,
								Value:     metricsData.CacheMetrics.L3CacheMetrics.OccupancyBytes,
								Tags:      l3Tags,
							})
						}

						if metricsData.CacheMetrics.L3CacheMetrics.HitRatio > 0 {
							l3Tags := append(procTags, tags.Tag{Key: "cache_level", Value: "L3"}, tags.Tag{Key: "unit", Value: "ratio"})
							datapoints = append(datapoints, data_store.DataPoint{
								Name:      "hardware.cpu.cache.hit_ratio",
								Timestamp: timestamp,
								Value:     metricsData.CacheMetrics.L3CacheMetrics.HitRatio,
								Tags:      l3Tags,
							})
						}

						// Check for OEM-specific metrics
						// Dell or HPE might have specific metrics under the OEM section
						if len(metricsData.Oem) > 0 {
							// Log OEM data for debugging - this can help identify
							// vendor-specific metrics in future iterations
							c.logger.Debug().
								Interface("oem_data", metricsData.Oem).
								Msg("Found OEM processor metrics data")

							// Extract Dell-specific metrics if present
							if dell, ok := metricsData.Oem["Dell"]; ok {
								dellData, _ := json.Marshal(dell)
								var dellMetrics struct {
									CPUUtilization float32 `json:"CPUUtilization"`
									IOWaitTime     float32 `json:"IOWaitTime"`
								}

								if err := json.Unmarshal(dellData, &dellMetrics); err == nil {
									if dellMetrics.CPUUtilization > 0 {
										datapoints = append(datapoints, data_store.DataPoint{
											Name:      "hardware.cpu.utilization.dell",
											Timestamp: timestamp,
											Value:     dellMetrics.CPUUtilization,
											Tags:      append(procTags, tags.Tag{Key: "unit", Value: "percent"}),
										})
									}
								}
							}

							// Extract HPE-specific metrics if present
							if hpe, ok := metricsData.Oem["Hpe"]; ok {
								hpeData, _ := json.Marshal(hpe)
								var hpeMetrics struct {
									CPUUtilization float32 `json:"CPUUtilization"`
									CState         float32 `json:"CState"`
									PState         float32 `json:"PState"`
								}

								if err := json.Unmarshal(hpeData, &hpeMetrics); err == nil {
									if hpeMetrics.CPUUtilization > 0 {
										datapoints = append(datapoints, data_store.DataPoint{
											Name:      "hardware.cpu.utilization.hpe",
											Timestamp: timestamp,
											Value:     hpeMetrics.CPUUtilization,
											Tags:      append(procTags, tags.Tag{Key: "unit", Value: "percent"}),
										})
									}
								}
							}
						}
					}
				}
			}
		}
	}

	return datapoints, nil
}
