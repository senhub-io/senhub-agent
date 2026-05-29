package redfish

import (
	"context"
	"encoding/json"
	"fmt"
	"senhub-agent.go/probesdk/datastore"
	"senhub-agent.go/probesdk/logger"
	"senhub-agent.go/probesdk/tags"
	"time"
)

// LenovoCollector implements Lenovo-specific Redfish collection
type LenovoCollector struct {
	*GenericCollector
	// Lenovo specific fields
	xccVersion string
	serverType string
	mtm        string // Machine Type Model
}

// NewLenovoCollector creates a new collector for Lenovo servers
func NewLenovoCollector(endpoint, username, password string, logger *logger.Logger, verifySSL bool) (RedfishCollector, error) {
	// First create a generic collector as the base
	genericCollector, err := NewGenericCollector(endpoint, username, password, logger, verifySSL)
	if err != nil {
		return nil, err
	}

	// Cast to use the embedded fields and methods
	gc, ok := genericCollector.(*GenericCollector)
	if !ok {
		return nil, fmt.Errorf("unexpected collector type, cannot create Lenovo collector")
	}

	// Create the Lenovo collector with the generic one embedded
	collector := &LenovoCollector{
		GenericCollector: gc,
	}

	// Override the vendor type
	collector.vendorType = VendorLenovo

	return collector, nil
}

// Connect implements Lenovo-specific connection logic
func (c *LenovoCollector) Connect(ctx context.Context) error {
	// First use the generic connect method
	if err := c.GenericCollector.Connect(ctx); err != nil {
		return err
	}

	// Get Lenovo-specific information
	if err := c.getLenovoInfo(ctx); err != nil {
		c.logger.Warn().Err(err).Msg("Failed to get Lenovo-specific info")
		// Continue anyway, as this is not critical
	}

	return nil
}

// getLenovoInfo retrieves Lenovo-specific information like XCC version
func (c *LenovoCollector) getLenovoInfo(ctx context.Context) error {
	// Try to get Lenovo-specific manager (XCC) info
	resp, err := c.client.Get(ctx, "Managers/1")
	if err != nil {
		// Try alternative path
		resp, err = c.client.Get(ctx, "Managers/BMC")
		if err != nil {
			return fmt.Errorf("failed to get XCC info: %v", err)
		}
	}

	// Extract XCC version
	c.xccVersion = resp.FirmwareVersion

	// Get system information if available
	if len(c.systems) > 0 {
		sysResp, err := c.client.Get(ctx, c.systems[0])
		if err == nil {
			// Extract Model (MTM)
			c.mtm = sysResp.Model

			// Try to get Lenovo-specific OEM data
			if sysResp.Oem != nil {
				lenovoData, hasLenovo := sysResp.Oem["Lenovo"]
				if hasLenovo {
					lenovoOem, ok := lenovoData.(map[string]interface{})
					if ok {
						// Get server type if available
						if serverType, has := lenovoOem["ProductName"]; has {
							c.serverType = fmt.Sprintf("%v", serverType)
						}
					}
				}
			}
		}
	}

	c.logger.Debug().
		Str("xcc_version", c.xccVersion).
		Str("mtm", c.mtm).
		Str("server_type", c.serverType).
		Msg("Retrieved Lenovo-specific information")

	return nil
}

// GetSupportedCollections returns Lenovo-specific collection capabilities
func (c *LenovoCollector) GetSupportedCollections() []CollectionType {
	// Lenovo supports all generic collections plus some extras
	return []CollectionType{
		CollectionSystem,
		CollectionThermal,
		CollectionPower,
		CollectionProcessor,
		CollectionMemory,
		CollectionStorage,
		CollectionDrives,
		CollectionNetworkAdapter,
	}
}

// IsSupported checks if a specific collection type is supported
func (c *LenovoCollector) IsSupported(collectionType CollectionType) bool {
	switch collectionType {
	case CollectionSystem, CollectionThermal, CollectionPower,
		CollectionProcessor, CollectionMemory, CollectionStorage,
		CollectionDrives, CollectionNetworkAdapter:
		return true
	default:
		return false
	}
}

// CollectMetrics gathers Lenovo-specific metrics
func (c *LenovoCollector) CollectMetrics(ctx context.Context, collectionType CollectionType, timestamp time.Time) ([]data_store.DataPoint, error) {
	// For most collection types, use the generic collector
	switch collectionType {
	case CollectionSystem:
		return c.collectSystemMetrics(ctx, timestamp)
	case CollectionThermal:
		return c.collectThermalMetrics(ctx, timestamp)
	case CollectionStorage, CollectionDrives:
		return c.collectStorageMetrics(ctx, timestamp)
	default:
		// For other types, delegate to the generic collector
		return c.GenericCollector.CollectMetrics(ctx, collectionType, timestamp)
	}
}

// collectSystemMetrics gathers Lenovo-specific system metrics
func (c *LenovoCollector) collectSystemMetrics(ctx context.Context, timestamp time.Time) ([]data_store.DataPoint, error) {
	// Get the generic system metrics first
	datapoints, err := c.GenericCollector.collectSystemMetrics(ctx, timestamp)
	if err != nil {
		return nil, err
	}

	// Add Lenovo-specific system datapoints
	for _, systemPath := range c.systems {
		// Get Lenovo OEM data for this system
		resp, err := c.client.Get(ctx, systemPath)
		if err != nil {
			c.logger.Warn().
				Err(err).
				Str("path", systemPath).
				Msg("Failed to get Lenovo system details")
			continue
		}

		// Extract system tags
		systemTags := []tags.Tag{
			{Key: "system_id", Value: resp.ID},
			{Key: "system_name", Value: resp.Name},
			{Key: "vendor", Value: string(c.vendorType)},
			{Key: "xcc_version", Value: c.xccVersion},
		}

		if c.mtm != "" {
			systemTags = append(systemTags, tags.Tag{Key: "mtm", Value: c.mtm})
		}

		if c.serverType != "" {
			systemTags = append(systemTags, tags.Tag{Key: "server_type", Value: c.serverType})
		}

		// Add Lenovo-specific OEM data if available
		if resp.Oem != nil {
			lenovoData, hasLenovo := resp.Oem["Lenovo"]
			if hasLenovo {
				lenovoOem, ok := lenovoData.(map[string]interface{})
				if ok {
					// Add machine type if available
					if machineType, has := lenovoOem["MachineType"]; has {
						machineTypeStr := fmt.Sprintf("%v", machineType)
						systemTags = append(systemTags, tags.Tag{Key: "machine_type", Value: machineTypeStr})
					}

					// Add location details if available
					if location, has := lenovoOem["Location"]; has {
						locationStr := fmt.Sprintf("%v", location)
						systemTags = append(systemTags, tags.Tag{Key: "location", Value: locationStr})
					}
				}
			}
		}

		// Add XCC version datapoint if available
		if c.xccVersion != "" {
			// We don't have a numeric value for version, so we use 1.0 as a sentinel value
			// The actual version is in the tags
			datapoints = append(datapoints, data_store.DataPoint{
				Name:      "system.xcc_firmware",
				Timestamp: timestamp,
				Value:     1.0, // Sentinel value
				Tags:      systemTags,
			})
		}
	}

	return datapoints, nil
}

// collectThermalMetrics gathers Lenovo-specific thermal metrics
func (c *LenovoCollector) collectThermalMetrics(ctx context.Context, timestamp time.Time) ([]data_store.DataPoint, error) {
	// Get the generic thermal metrics first
	datapoints, err := c.GenericCollector.collectThermalMetrics(ctx, timestamp)
	if err != nil {
		return nil, err
	}

	// Try to get Lenovo-specific environmental metrics
	// Lenovo often has additional OEM-specific data
	if len(c.chassis) > 0 {
		envPath := c.chassis[0] + "/Oem/Lenovo/EnvironmentalMetrics"
		envResp, err := c.client.GetRaw(ctx, envPath)
		if err == nil {
			// Parse the raw response
			var envData map[string]interface{}
			if err := json.Unmarshal(envResp, &envData); err == nil {
				// Extract chassis info for tags
				chassisResp, err := c.client.Get(ctx, c.chassis[0])
				if err != nil {
					c.logger.Warn().
						Err(err).
						Str("path", c.chassis[0]).
						Msg("Failed to get chassis details")
				}

				chassisTags := []tags.Tag{
					{Key: "chassis_id", Value: chassisResp.ID},
					{Key: "chassis_name", Value: chassisResp.Name},
					{Key: "vendor", Value: string(c.vendorType)},
				}

				// Extract additional fan metrics
				if fanMetrics, hasFans := envData["FanMetrics"]; hasFans {
					fans, ok := fanMetrics.([]interface{})
					if ok {
						for _, fan := range fans {
							fanData, ok := fan.(map[string]interface{})
							if !ok {
								continue
							}

							// Get fan name
							var fanName string
							if name, hasName := fanData["Name"]; hasName {
								fanName = fmt.Sprintf("%v", name)
							} else {
								continue
							}

							// Create fan-specific tags
							fanTags := append([]tags.Tag{}, chassisTags...)
							fanTags = append(fanTags, tags.Tag{Key: "fan_name", Value: fanName})

							// Get fan speed percentage
							if speedPercent, hasSpeed := fanData["SpeedPercent"]; hasSpeed {
								if percent, ok := speedPercent.(float64); ok {
									datapoints = append(datapoints, data_store.DataPoint{
										Name:      "thermal.fan_speed_percent",
										Timestamp: timestamp,
										Value:     float32(percent),
										Tags:      fanTags,
									})
								}
							}
						}
					}
				}

				// Extract additional temperature metrics
				if tempMetrics, hasTemps := envData["TemperatureMetrics"]; hasTemps {
					temps, ok := tempMetrics.([]interface{})
					if ok {
						for _, temp := range temps {
							tempData, ok := temp.(map[string]interface{})
							if !ok {
								continue
							}

							// Get temperature sensor name
							var sensorName string
							if name, hasName := tempData["Name"]; hasName {
								sensorName = fmt.Sprintf("%v", name)
							} else {
								continue
							}

							// Create sensor-specific tags
							sensorTags := append([]tags.Tag{}, chassisTags...)
							sensorTags = append(sensorTags, tags.Tag{Key: "sensor_name", Value: sensorName})

							// Add location if available
							if location, hasLocation := tempData["PhysicalContext"]; hasLocation {
								locationStr := fmt.Sprintf("%v", location)
								sensorTags = append(sensorTags, tags.Tag{Key: "physical_context", Value: locationStr})
							}

							// Get temperature reading
							if reading, hasReading := tempData["ReadingCelsius"]; hasReading {
								if celsius, ok := reading.(float64); ok {
									datapoints = append(datapoints, data_store.DataPoint{
										Name:      "thermal.temperature",
										Timestamp: timestamp,
										Value:     float32(celsius),
										Tags:      sensorTags,
									})
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

// collectStorageMetrics gathers Lenovo-specific storage metrics
func (c *LenovoCollector) collectStorageMetrics(ctx context.Context, timestamp time.Time) ([]data_store.DataPoint, error) {
	// Lenovo generally follows the standard Redfish schema for storage
	// Use the generic storage collection method
	datapoints, err := c.GenericCollector.collectStorageMetrics(ctx, timestamp)
	if err != nil {
		return nil, err
	}

	// Try to get Lenovo-specific RAID controller status
	if len(c.systems) > 0 {
		raidPath := c.systems[0] + "/Oem/Lenovo/RaidCard"
		raidResp, err := c.client.GetRaw(ctx, raidPath)
		if err == nil {
			// Parse the raw response
			var raidData map[string]interface{}
			if err := json.Unmarshal(raidResp, &raidData); err == nil {
				// Extract system info for tags
				sysResp, err := c.client.Get(ctx, c.systems[0])
				if err != nil {
					c.logger.Warn().
						Err(err).
						Str("path", c.systems[0]).
						Msg("Failed to get system details")
				}

				systemTags := []tags.Tag{
					{Key: "system_id", Value: sysResp.ID},
					{Key: "system_name", Value: sysResp.Name},
					{Key: "vendor", Value: string(c.vendorType)},
				}

				// Extract RAID controller metrics
				if controllers, hasControllers := raidData["Members"]; hasControllers {
					controllersList, ok := controllers.([]interface{})
					if ok {
						for _, controller := range controllersList {
							controllerData, ok := controller.(map[string]interface{})
							if !ok {
								continue
							}

							// Get controller name
							var controllerName string
							if name, hasName := controllerData["Name"]; hasName {
								controllerName = fmt.Sprintf("%v", name)
							} else if id, hasID := controllerData["Id"]; hasID {
								controllerName = fmt.Sprintf("Controller %v", id)
							} else {
								continue
							}

							// Create controller-specific tags
							controllerTags := append([]tags.Tag{}, systemTags...)
							controllerTags = append(controllerTags, tags.Tag{Key: "controller_name", Value: controllerName})

							// Get model if available
							if model, hasModel := controllerData["Model"]; hasModel {
								modelStr := fmt.Sprintf("%v", model)
								controllerTags = append(controllerTags, tags.Tag{Key: "model", Value: modelStr})
							}

							// Get health status
							var healthState int
							if status, hasStatus := controllerData["Status"]; hasStatus {
								statusObj, ok := status.(map[string]interface{})
								if ok {
									if health, hasHealth := statusObj["Health"]; hasHealth {
										healthStr := fmt.Sprintf("%v", health)
										healthState = mapHealthState(healthStr)
									}
								}
							}

							// Add health datapoint
							datapoints = append(datapoints, data_store.DataPoint{
								Name:      "storage.controller.health",
								Timestamp: timestamp,
								Value:     float32(healthState),
								Tags:      controllerTags,
							})
						}
					}
				}
			}
		}
	}

	return datapoints, nil
}
