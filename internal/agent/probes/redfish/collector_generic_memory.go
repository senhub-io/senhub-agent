package redfish

import (
	"context"
	"encoding/json"
	"fmt"
	"senhub-agent.go/probesdk/datastore"
	"senhub-agent.go/probesdk/tags"
	"time"
)

// collectMemoryMetrics gathers memory metrics
func (c *GenericCollector) collectMemoryMetrics(ctx context.Context, timestamp time.Time) ([]data_store.DataPoint, error) {
	if len(c.systems) == 0 {
		return nil, fmt.Errorf("no systems found")
	}

	var datapoints []data_store.DataPoint

	// Collect metrics for each system
	for _, systemPath := range c.systems {
		// Get memory collection
		memoryPath := systemPath + "/Memory"
		resp, err := c.client.Get(ctx, memoryPath)
		if err != nil {
			c.logger.Warn().
				Err(err).
				Str("path", memoryPath).
				Msg("Failed to get memory collection, skipping")
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

		// Process each memory module
		for _, member := range resp.Members {
			dimPath, ok := member["@odata.id"]
			if !ok {
				continue
			}

			// Get memory module details
			dimResp, err := c.client.Get(ctx, dimPath)
			if err != nil {
				c.logger.Warn().
					Err(err).
					Str("path", dimPath).
					Msg("Failed to get memory module details, skipping")
				continue
			}

			// Create memory-specific tags following REDFISH-TAGS.md conventions
			dimTags := append([]tags.Tag{}, systemTags...)
			dimTags = append(dimTags, tags.Tag{Key: "memory_id", Value: dimResp.ID})
			dimTags = append(dimTags, tags.Tag{Key: "memory_name", Value: dimResp.Name})

			if dimResp.Manufacturer != "" {
				dimTags = append(dimTags, tags.Tag{Key: "manufacturer", Value: dimResp.Manufacturer})
			}
			if dimResp.SerialNumber != "" {
				dimTags = append(dimTags, tags.Tag{Key: "serial_number", Value: dimResp.SerialNumber})
			}
			if dimResp.PartNumber != "" {
				dimTags = append(dimTags, tags.Tag{Key: "part_number", Value: dimResp.PartNumber})
			}
			if dimResp.AssetTag != "" {
				dimTags = append(dimTags, tags.Tag{Key: "asset_tag", Value: dimResp.AssetTag})
			}
			if dimResp.SKU != "" {
				dimTags = append(dimTags, tags.Tag{Key: "sku", Value: dimResp.SKU})
			}
			if dimResp.Status != nil && dimResp.Status.State != "" {
				dimTags = append(dimTags, tags.Tag{Key: "state", Value: dimResp.Status.State})
			}

			// Extract memory metrics from response
			var dimData struct {
				CapacityMiB       int     `json:"CapacityMiB"`
				OperatingSpeedMhz int     `json:"OperatingSpeedMhz"`
				MemoryType        string  `json:"MemoryDeviceType"`
				DataWidthBits     int     `json:"DataWidthBits"`
				RankCount         int     `json:"RankCount"`
				Status            *Status `json:"Status"`
				BaseModuleType    string  `json:"BaseModuleType"`
				BusWidthBits      int     `json:"BusWidthBits"`
				Location          struct {
					Socket           int `json:"Socket"`
					MemoryController int `json:"MemoryController"`
					Channel          int `json:"Channel"`
					Slot             int `json:"Slot"`
				} `json:"Location"`
				Links struct {
					Metrics struct {
						OdataID string `json:"@odata.id"`
					} `json:"Metrics"`
				} `json:"Links"`
				MemoryLocation struct {
					Socket           int `json:"Socket"`
					MemoryController int `json:"MemoryController"`
					Channel          int `json:"Channel"`
					Slot             int `json:"Slot"`
				} `json:"MemoryLocation"`
				AllowedSpeedsMHz   []int  `json:"AllowedSpeedsMHz"`
				ConfiguredSpeedMHz int    `json:"ConfiguredSpeedMHz"`
				MaxTDPMilliWatts   int    `json:"MaxTDPMilliWatts"`
				CacheSizeMiB       int    `json:"CacheSizeMiB"`
				LogicalSizeMiB     int    `json:"LogicalSizeMiB"`
				ErrorCorrection    string `json:"ErrorCorrection"`
			}
			rawJSON, _ := json.Marshal(dimResp)
			if err := json.Unmarshal(rawJSON, &dimData); err != nil {
				c.logger.Warn().
					Err(err).
					Msg("Failed to parse memory module data")
				continue
			}

			// Add memory type to tags if available
			if dimData.MemoryType != "" {
				dimTags = append(dimTags, tags.Tag{Key: "memory_type", Value: dimData.MemoryType})
			}
			if dimData.BaseModuleType != "" {
				dimTags = append(dimTags, tags.Tag{Key: "module_type", Value: dimData.BaseModuleType})
			}
			if dimData.ErrorCorrection != "" {
				dimTags = append(dimTags, tags.Tag{Key: "error_correction", Value: dimData.ErrorCorrection})
			}

			// Add memory location info
			// First check newer Location structure
			if dimData.Location.Socket > 0 {
				dimTags = append(dimTags, tags.Tag{Key: "socket", Value: fmt.Sprintf("%d", dimData.Location.Socket)})
				dimTags = append(dimTags, tags.Tag{Key: "memory_controller", Value: fmt.Sprintf("%d", dimData.Location.MemoryController)})
				dimTags = append(dimTags, tags.Tag{Key: "channel", Value: fmt.Sprintf("%d", dimData.Location.Channel)})
				dimTags = append(dimTags, tags.Tag{Key: "slot", Value: fmt.Sprintf("%d", dimData.Location.Slot)})
			} else if dimData.MemoryLocation.Socket > 0 {
				// Fall back to older MemoryLocation structure
				dimTags = append(dimTags, tags.Tag{Key: "socket", Value: fmt.Sprintf("%d", dimData.MemoryLocation.Socket)})
				dimTags = append(dimTags, tags.Tag{Key: "memory_controller", Value: fmt.Sprintf("%d", dimData.MemoryLocation.MemoryController)})
				dimTags = append(dimTags, tags.Tag{Key: "channel", Value: fmt.Sprintf("%d", dimData.MemoryLocation.Channel)})
				dimTags = append(dimTags, tags.Tag{Key: "slot", Value: fmt.Sprintf("%d", dimData.MemoryLocation.Slot)})
			}

			// Add memory capacity
			if dimData.CapacityMiB > 0 {
				datapoints = append(datapoints, data_store.DataPoint{
					Name:      "hardware.memory.capacity",
					Timestamp: timestamp,
					Value:     float32(dimData.CapacityMiB),
					Tags:      append(dimTags, tags.Tag{Key: "unit", Value: "MiB"}),
				})
			}

			// Add logical size if available and different from capacity
			if dimData.LogicalSizeMiB > 0 && dimData.LogicalSizeMiB != dimData.CapacityMiB {
				datapoints = append(datapoints, data_store.DataPoint{
					Name:      "hardware.memory.logical_size",
					Timestamp: timestamp,
					Value:     float32(dimData.LogicalSizeMiB),
					Tags:      append(dimTags, tags.Tag{Key: "unit", Value: "MiB"}),
				})
			}

			// Add cache size if available
			if dimData.CacheSizeMiB > 0 {
				datapoints = append(datapoints, data_store.DataPoint{
					Name:      "hardware.memory.cache_size",
					Timestamp: timestamp,
					Value:     float32(dimData.CacheSizeMiB),
					Tags:      append(dimTags, tags.Tag{Key: "unit", Value: "MiB"}),
				})
			}

			// Add memory operating speed
			if dimData.OperatingSpeedMhz > 0 {
				datapoints = append(datapoints, data_store.DataPoint{
					Name:      "hardware.memory.speed",
					Timestamp: timestamp,
					Value:     float32(dimData.OperatingSpeedMhz),
					Tags:      append(dimTags, tags.Tag{Key: "unit", Value: "MHz"}),
				})
			}

			// Add configured speed if different from operating speed
			if dimData.ConfiguredSpeedMHz > 0 && dimData.ConfiguredSpeedMHz != dimData.OperatingSpeedMhz {
				datapoints = append(datapoints, data_store.DataPoint{
					Name:      "hardware.memory.configured_speed",
					Timestamp: timestamp,
					Value:     float32(dimData.ConfiguredSpeedMHz),
					Tags:      append(dimTags, tags.Tag{Key: "unit", Value: "MHz"}),
				})
			}

			// Add data width
			if dimData.DataWidthBits > 0 {
				datapoints = append(datapoints, data_store.DataPoint{
					Name:      "hardware.memory.data_width",
					Timestamp: timestamp,
					Value:     float32(dimData.DataWidthBits),
					Tags:      append(dimTags, tags.Tag{Key: "unit", Value: "bits"}),
				})
			}

			// Add bus width
			if dimData.BusWidthBits > 0 {
				datapoints = append(datapoints, data_store.DataPoint{
					Name:      "hardware.memory.bus_width",
					Timestamp: timestamp,
					Value:     float32(dimData.BusWidthBits),
					Tags:      append(dimTags, tags.Tag{Key: "unit", Value: "bits"}),
				})
			}

			// Add rank count
			if dimData.RankCount > 0 {
				datapoints = append(datapoints, data_store.DataPoint{
					Name:      "hardware.memory.rank_count",
					Timestamp: timestamp,
					Value:     float32(dimData.RankCount),
					Tags:      dimTags,
				})
			}

			// Add max TDP
			if dimData.MaxTDPMilliWatts > 0 {
				datapoints = append(datapoints, data_store.DataPoint{
					Name:      "hardware.memory.max_tdp",
					Timestamp: timestamp,
					Value:     float32(dimData.MaxTDPMilliWatts),
					Tags:      append(dimTags, tags.Tag{Key: "unit", Value: "milliwatts"}),
				})
			}

			// Add health state if available
			if dimData.Status != nil {
				health := mapHealthState(dimData.Status.Health)
				datapoints = append(datapoints, data_store.DataPoint{
					Name:      "hardware.memory.health",
					Timestamp: timestamp,
					Value:     float32(health),
					Tags:      dimTags,
				})
			}

			// Check for memory metrics endpoint
			metricsPath := ""
			// Try to get from Links section first
			if dimData.Links.Metrics.OdataID != "" {
				metricsPath = dimData.Links.Metrics.OdataID
			} else {
				// Try standard path as a fallback
				metricsPath = dimPath + "/Metrics"
			}

			if metricsPath != "" {
				// Get memory metrics
				metricsResp, err := c.client.Get(ctx, metricsPath)
				if err != nil {
					c.logger.Debug().
						Err(err).
						Str("path", metricsPath).
						Msg("MemoryMetrics endpoint not available")
				} else {
					// Parse memory metrics
					var metricsData struct {
						BlockSizeBytes             int   `json:"BlockSizeBytes"`
						CurrentPeriodBlocksRead    int64 `json:"CurrentPeriodBlocksRead"`
						CurrentPeriodBlocksWritten int64 `json:"CurrentPeriodBlocksWritten"`
						LifeTime                   struct {
							BlocksRead    int64 `json:"BlocksRead"`
							BlocksWritten int64 `json:"BlocksWritten"`
						} `json:"LifeTime"`
						OperatingSpeedMHz  int     `json:"OperatingSpeedMHz"`
						TemperatureCelsius float32 `json:"TemperatureCelsius"`
						ThrottledCycles    int64   `json:"ThrottledCycles"`
						BandwidthPercent   float32 `json:"BandwidthPercent"`
						ThermalMargin      float32 `json:"ThermalMargin"`
						ConsumedPowerWatts float32 `json:"ConsumedPowerWatts"`
						AlarmTrips         struct {
							Temperature         bool `json:"Temperature"`
							Spares              bool `json:"Spares"`
							Uncorrectable       bool `json:"Uncorrectable"`
							CorrectableECCError bool `json:"CorrectableECCError"`
						} `json:"AlarmTrips"`
						CorrectableECCErrorCount   int64                  `json:"CorrectableECCErrorCount"`
						UncorrectableECCErrorCount int64                  `json:"UncorrectableECCErrorCount"`
						Oem                        map[string]interface{} `json:"Oem"`
					}

					rawJSON, _ := json.Marshal(metricsResp)
					if err := json.Unmarshal(rawJSON, &metricsData); err != nil {
						c.logger.Warn().
							Err(err).
							Msg("Failed to parse memory metrics data")
					} else {
						// Add memory temperature
						if metricsData.TemperatureCelsius > 0 {
							datapoints = append(datapoints, data_store.DataPoint{
								Name:      "hardware.memory.temperature",
								Timestamp: timestamp,
								Value:     metricsData.TemperatureCelsius,
								Tags:      append(dimTags, tags.Tag{Key: "unit", Value: "celsius"}),
							})
						}

						// Add thermal margin
						if metricsData.ThermalMargin > 0 {
							datapoints = append(datapoints, data_store.DataPoint{
								Name:      "hardware.memory.thermal_margin",
								Timestamp: timestamp,
								Value:     metricsData.ThermalMargin,
								Tags:      append(dimTags, tags.Tag{Key: "unit", Value: "celsius"}),
							})
						}

						// Add power consumption
						if metricsData.ConsumedPowerWatts > 0 {
							datapoints = append(datapoints, data_store.DataPoint{
								Name:      "hardware.memory.power_consumption",
								Timestamp: timestamp,
								Value:     metricsData.ConsumedPowerWatts,
								Tags:      append(dimTags, tags.Tag{Key: "unit", Value: "watt"}),
							})
						}

						// Add bandwidth utilization
						if metricsData.BandwidthPercent > 0 {
							datapoints = append(datapoints, data_store.DataPoint{
								Name:      "hardware.memory.bandwidth_utilization",
								Timestamp: timestamp,
								Value:     metricsData.BandwidthPercent,
								Tags:      append(dimTags, tags.Tag{Key: "unit", Value: "percent"}),
							})
						}

						// Add throttled cycles
						if metricsData.ThrottledCycles > 0 {
							datapoints = append(datapoints, data_store.DataPoint{
								Name:      "hardware.memory.throttled_cycles",
								Timestamp: timestamp,
								Value:     float32(metricsData.ThrottledCycles),
								Tags:      dimTags,
							})
						}

						// Add error counts
						if metricsData.CorrectableECCErrorCount > 0 {
							datapoints = append(datapoints, data_store.DataPoint{
								Name:      "hardware.memory.correctable_ecc_errors",
								Timestamp: timestamp,
								Value:     float32(metricsData.CorrectableECCErrorCount),
								Tags:      dimTags,
							})
						}

						if metricsData.UncorrectableECCErrorCount > 0 {
							datapoints = append(datapoints, data_store.DataPoint{
								Name:      "hardware.memory.uncorrectable_ecc_errors",
								Timestamp: timestamp,
								Value:     float32(metricsData.UncorrectableECCErrorCount),
								Tags:      dimTags,
							})
						}

						// Add alarm status
						// Convert boolean alarms to numeric values (0 = false, 1 = true)
						temperatureAlarm := 0.0
						if metricsData.AlarmTrips.Temperature {
							temperatureAlarm = 1.0
						}
						datapoints = append(datapoints, data_store.DataPoint{
							Name:      "hardware.memory.alarm.temperature",
							Timestamp: timestamp,
							Value:     float32(temperatureAlarm),
							Tags:      dimTags,
						})

						sparesAlarm := 0.0
						if metricsData.AlarmTrips.Spares {
							sparesAlarm = 1.0
						}
						datapoints = append(datapoints, data_store.DataPoint{
							Name:      "hardware.memory.alarm.spares",
							Timestamp: timestamp,
							Value:     float32(sparesAlarm),
							Tags:      dimTags,
						})

						uncorrectableAlarm := 0.0
						if metricsData.AlarmTrips.Uncorrectable {
							uncorrectableAlarm = 1.0
						}
						datapoints = append(datapoints, data_store.DataPoint{
							Name:      "hardware.memory.alarm.uncorrectable",
							Timestamp: timestamp,
							Value:     float32(uncorrectableAlarm),
							Tags:      dimTags,
						})

						correctableECCAlarm := 0.0
						if metricsData.AlarmTrips.CorrectableECCError {
							correctableECCAlarm = 1.0
						}
						datapoints = append(datapoints, data_store.DataPoint{
							Name:      "hardware.memory.alarm.correctable_ecc",
							Timestamp: timestamp,
							Value:     float32(correctableECCAlarm),
							Tags:      dimTags,
						})

						// Add IO activity metrics
						if metricsData.BlockSizeBytes > 0 {
							// Store block size as a metric
							datapoints = append(datapoints, data_store.DataPoint{
								Name:      "hardware.memory.block_size",
								Timestamp: timestamp,
								Value:     float32(metricsData.BlockSizeBytes),
								Tags:      append(dimTags, tags.Tag{Key: "unit", Value: "bytes"}),
							})
						}

						// Current period IO metrics
						if metricsData.CurrentPeriodBlocksRead > 0 {
							datapoints = append(datapoints, data_store.DataPoint{
								Name:      "hardware.memory.current_period.blocks_read",
								Timestamp: timestamp,
								Value:     float32(metricsData.CurrentPeriodBlocksRead),
								Tags:      dimTags,
							})
						}

						if metricsData.CurrentPeriodBlocksWritten > 0 {
							datapoints = append(datapoints, data_store.DataPoint{
								Name:      "hardware.memory.current_period.blocks_written",
								Timestamp: timestamp,
								Value:     float32(metricsData.CurrentPeriodBlocksWritten),
								Tags:      dimTags,
							})
						}

						// Lifetime IO metrics
						if metricsData.LifeTime.BlocksRead > 0 {
							datapoints = append(datapoints, data_store.DataPoint{
								Name:      "hardware.memory.lifetime.blocks_read",
								Timestamp: timestamp,
								Value:     float32(metricsData.LifeTime.BlocksRead),
								Tags:      dimTags,
							})
						}

						if metricsData.LifeTime.BlocksWritten > 0 {
							datapoints = append(datapoints, data_store.DataPoint{
								Name:      "hardware.memory.lifetime.blocks_written",
								Timestamp: timestamp,
								Value:     float32(metricsData.LifeTime.BlocksWritten),
								Tags:      dimTags,
							})
						}

						// Check for OEM-specific metrics
						if len(metricsData.Oem) > 0 {
							// Log OEM data for debugging - this helps identify
							// vendor-specific metrics in future iterations
							c.logger.Debug().
								Interface("oem_data", metricsData.Oem).
								Msg("Found OEM memory metrics data")

							// Extract Dell-specific metrics if present
							if dell, ok := metricsData.Oem["Dell"]; ok {
								dellData, _ := json.Marshal(dell)
								var dellMetrics struct {
									RemainingSpares int `json:"RemainingSpares"`
									UsedSpares      int `json:"UsedSpares"`
									AvailableSpare  int `json:"AvailableSpare"`
								}

								if err := json.Unmarshal(dellData, &dellMetrics); err == nil {
									if dellMetrics.RemainingSpares > 0 {
										datapoints = append(datapoints, data_store.DataPoint{
											Name:      "hardware.memory.dell.remaining_spares",
											Timestamp: timestamp,
											Value:     float32(dellMetrics.RemainingSpares),
											Tags:      dimTags,
										})
									}

									if dellMetrics.UsedSpares > 0 {
										datapoints = append(datapoints, data_store.DataPoint{
											Name:      "hardware.memory.dell.used_spares",
											Timestamp: timestamp,
											Value:     float32(dellMetrics.UsedSpares),
											Tags:      dimTags,
										})
									}
								}
							}

							// Extract HPE-specific metrics if present
							if hpe, ok := metricsData.Oem["Hpe"]; ok {
								hpeData, _ := json.Marshal(hpe)
								var hpeMetrics struct {
									MinimumOperatingVoltageMillivolts int `json:"MinimumOperatingVoltageMillivolts"`
									MaximumOperatingVoltageMillivolts int `json:"MaximumOperatingVoltageMillivolts"`
									CurrentOperatingVoltageMillivolts int `json:"CurrentOperatingVoltageMillivolts"`
								}

								if err := json.Unmarshal(hpeData, &hpeMetrics); err == nil {
									if hpeMetrics.CurrentOperatingVoltageMillivolts > 0 {
										datapoints = append(datapoints, data_store.DataPoint{
											Name:      "hardware.memory.hpe.current_voltage",
											Timestamp: timestamp,
											Value:     float32(hpeMetrics.CurrentOperatingVoltageMillivolts),
											Tags:      append(dimTags, tags.Tag{Key: "unit", Value: "millivolts"}),
										})
									}

									if hpeMetrics.MinimumOperatingVoltageMillivolts > 0 {
										datapoints = append(datapoints, data_store.DataPoint{
											Name:      "hardware.memory.hpe.min_voltage",
											Timestamp: timestamp,
											Value:     float32(hpeMetrics.MinimumOperatingVoltageMillivolts),
											Tags:      append(dimTags, tags.Tag{Key: "unit", Value: "millivolts"}),
										})
									}

									if hpeMetrics.MaximumOperatingVoltageMillivolts > 0 {
										datapoints = append(datapoints, data_store.DataPoint{
											Name:      "hardware.memory.hpe.max_voltage",
											Timestamp: timestamp,
											Value:     float32(hpeMetrics.MaximumOperatingVoltageMillivolts),
											Tags:      append(dimTags, tags.Tag{Key: "unit", Value: "millivolts"}),
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
