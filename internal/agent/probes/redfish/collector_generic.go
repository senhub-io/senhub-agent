package redfish

import (
	"context"
	"encoding/json"
	"fmt"
	"senhub-agent.go/internal/agent/services/data_store"
	"senhub-agent.go/internal/agent/services/logger"
	"senhub-agent.go/internal/agent/tags"
	"strconv"
	"strings"
	"time"
)

// GenericCollector provides a base implementation of the RedfishCollector interface
type GenericCollector struct {
	client          *RedfishClient
	vendorType      VendorType
	systems         []string
	chassis         []string
	logger          *logger.Logger
	redfishVersion  string              // Redfish API version
	schemaVersions  map[string]string   // Schema versions by schema name
	oemVersionData  map[string]interface{} // OEM-specific version data
}

// NewGenericCollector creates a new generic Redfish collector
func NewGenericCollector(endpoint, username, password string, logger *logger.Logger, verifySSL bool) (RedfishCollector, error) {
	client, err := NewRedfishClient(endpoint, username, password, logger, verifySSL)
	if err != nil {
		return nil, fmt.Errorf("failed to create Redfish client: %v", err)
	}

	return &GenericCollector{
		client:         client,
		vendorType:     VendorGeneric, // Default to generic, will be updated after detection
		logger:         logger,
		schemaVersions: make(map[string]string),
		oemVersionData: make(map[string]interface{}),
	}, nil
}

// GetVendorType returns the vendor type this collector handles
func (c *GenericCollector) GetVendorType() VendorType {
	return c.vendorType
}

// Connect establishes a connection to the Redfish API endpoint and discover resources
func (c *GenericCollector) Connect(ctx context.Context) error {
	// Connect to Redfish API
	if err := c.client.Connect(ctx); err != nil {
		return fmt.Errorf("failed to connect to Redfish API: %v", err)
	}

	// Detect Redfish and schema versions
	versionInfo, err := c.client.DetectRedfishVersions(ctx)
	if err != nil {
		c.logger.Warn().
			Err(err).
			Msg("Failed to detect Redfish versions, continuing with limited compatibility")
	} else {
		c.redfishVersion = versionInfo.RedfishVersion
		c.schemaVersions = versionInfo.SchemaVersions
		c.oemVersionData = versionInfo.OemVersions

		c.logger.Info().
			Str("redfish_version", versionInfo.RedfishVersion).
			Interface("schema_versions", versionInfo.SchemaVersions).
			Msg("Detected Redfish specifications")
	}

	// Discover systems
	if err := c.discoverSystems(ctx); err != nil {
		return fmt.Errorf("failed to discover systems: %v", err)
	}

	// Discover chassis
	if err := c.discoverChassis(ctx); err != nil {
		return fmt.Errorf("failed to discover chassis: %v", err)
	}

	// Detect vendor if not already set
	if c.vendorType == VendorGeneric {
		if err := c.detectVendor(ctx); err != nil {
			c.logger.Warn().Err(err).Msg("Failed to detect vendor, using generic implementation")
			// Continue with generic implementation
		}
	}

	return nil
}

// discoverSystems finds available systems in the Redfish API
func (c *GenericCollector) discoverSystems(ctx context.Context) error {
	// Get systems collection
	resp, err := c.client.Get(ctx, "Systems")
	if err != nil {
		return fmt.Errorf("failed to get systems: %v", err)
	}

	// Extract system paths from members
	c.systems = make([]string, 0, len(resp.Members))
	for _, member := range resp.Members {
		if id, ok := member["@odata.id"]; ok {
			// Remove the /redfish/v1/ prefix from the path if it exists
			normalizedPath := strings.TrimPrefix(id, "/redfish/v1/")
			c.systems = append(c.systems, normalizedPath)
		}
	}

	c.logger.Debug().
		Int("count", len(c.systems)).
		Msg("Discovered systems")

	return nil
}

// discoverChassis finds available chassis in the Redfish API
func (c *GenericCollector) discoverChassis(ctx context.Context) error {
	// Get chassis collection
	resp, err := c.client.Get(ctx, "Chassis")
	if err != nil {
		return fmt.Errorf("failed to get chassis: %v", err)
	}

	// Extract chassis paths from members
	c.chassis = make([]string, 0, len(resp.Members))
	for _, member := range resp.Members {
		if id, ok := member["@odata.id"]; ok {
			// Remove the /redfish/v1/ prefix from the path if it exists
			normalizedPath := strings.TrimPrefix(id, "/redfish/v1/")
			c.chassis = append(c.chassis, normalizedPath)
		}
	}

	c.logger.Debug().
		Int("count", len(c.chassis)).
		Msg("Discovered chassis")

	return nil
}

// detectVendor attempts to determine the hardware vendor
func (c *GenericCollector) detectVendor(ctx context.Context) error {
	// Check if this is a storage device first (some don't have systems collection)
	storageResp, err := c.client.Get(ctx, "Storage")
	if err == nil && len(storageResp.Members) > 0 {
		// Check for Seagate/PowerVault specific info
		rootResp, err := c.client.Get(ctx, "")
		if err == nil && rootResp.Oem != nil {
			rawJSON, _ := json.Marshal(rootResp.Oem)
			var oemData map[string]interface{}
			if err := json.Unmarshal(rawJSON, &oemData); err == nil {
				// Check for Seagate or other storage vendors in OEM data
				if _, hasSeagate := oemData["Seagate"]; hasSeagate {
					c.vendorType = VendorStorage
					c.logger.Info().Msg("Detected storage system based on Seagate OEM data")
					return nil
				}
			}
		}
	}

	// If no systems are found, we can't detect the vendor using systems approach
	if len(c.systems) == 0 {
		// Check if we have storage collection instead
		if storageResp != nil && len(storageResp.Members) > 0 {
			c.vendorType = VendorStorage
			c.logger.Info().Msg("Detected storage system based on Storage collection")
			return nil
		}
		return fmt.Errorf("no systems found to detect vendor")
	}

	// Get first system details
	resp, err := c.client.Get(ctx, c.systems[0])
	if err != nil {
		return fmt.Errorf("failed to get system details: %v", err)
	}

	// Try to detect vendor from manufacturer
	if resp.Manufacturer != "" {
		manufacturer := resp.Manufacturer
		switch {
		case containsIgnoreCase(manufacturer, "hp") || containsIgnoreCase(manufacturer, "hewlett packard"):
			c.vendorType = VendorHPE
		case containsIgnoreCase(manufacturer, "dell"):
			// Check if it's a PowerVault storage device
			if containsIgnoreCase(resp.Model, "powervault") || containsIgnoreCase(resp.Model, "me") {
				c.vendorType = VendorStorage
			} else {
				c.vendorType = VendorDell
			}
		case containsIgnoreCase(manufacturer, "lenovo"):
			c.vendorType = VendorLenovo
		case containsIgnoreCase(manufacturer, "cisco"):
			c.vendorType = VendorCisco
		case containsIgnoreCase(manufacturer, "huawei"):
			c.vendorType = VendorHuawei
		case containsIgnoreCase(manufacturer, "fujitsu"):
			c.vendorType = VendorFujitsu
		case containsIgnoreCase(manufacturer, "supermicro"):
			c.vendorType = VendorSupermicro
		}
	}

	// If vendor is still not detected, try OEM data
	if c.vendorType == VendorGeneric && resp.Oem != nil {
		// Check for known OEM keys
		for key := range resp.Oem {
			switch {
			case containsIgnoreCase(key, "hp") || containsIgnoreCase(key, "hpe"):
				c.vendorType = VendorHPE
			case containsIgnoreCase(key, "dell"):
				c.vendorType = VendorDell
			case containsIgnoreCase(key, "lenovo"):
				c.vendorType = VendorLenovo
			case containsIgnoreCase(key, "cisco"):
				c.vendorType = VendorCisco
			case containsIgnoreCase(key, "seagate"):
				c.vendorType = VendorStorage
			}
		}
	}

	c.logger.Info().
		Str("vendor", string(c.vendorType)).
		Str("manufacturer", resp.Manufacturer).
		Msg("Detected vendor")

	return nil
}

// Disconnect closes the connection
func (c *GenericCollector) Disconnect(ctx context.Context) error {
	return c.client.Disconnect(ctx)
}

// CollectMetrics gathers metrics for the specified collection type
func (c *GenericCollector) CollectMetrics(ctx context.Context, collectionType CollectionType, timestamp time.Time) ([]data_store.DataPoint, error) {
	switch collectionType {
	case CollectionSystem:
		return c.collectSystemMetrics(ctx, timestamp)
	case CollectionThermal:
		return c.collectThermalMetrics(ctx, timestamp)
	case CollectionPower:
		return c.collectPowerMetrics(ctx, timestamp)
	case CollectionProcessor:
		return c.collectProcessorMetrics(ctx, timestamp)
	case CollectionMemory:
		return c.collectMemoryMetrics(ctx, timestamp)
	default:
		return nil, fmt.Errorf("unsupported collection type: %s", collectionType)
	}
}

// IsSupported checks if a specific collection type is supported
func (c *GenericCollector) IsSupported(collectionType CollectionType) bool {
	// Generic implementation supports basic collections
	switch collectionType {
	case CollectionSystem, CollectionThermal, CollectionPower:
		return true
	case CollectionProcessor, CollectionMemory:
		return true
	default:
		return false
	}
}

// GetSupportedCollections returns all supported collection types
func (c *GenericCollector) GetSupportedCollections() []CollectionType {
	return []CollectionType{
		CollectionSystem,
		CollectionThermal,
		CollectionPower,
		CollectionProcessor,
		CollectionMemory,
	}
}

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

		// Extract hostname - we'll use this both for tags and to associate with other metrics
		hostname := resp.Name

		// Extract system tags according to REDFISH-TAGS.md conventions
		systemTags := []tags.Tag{
			{Key: "system_id", Value: resp.ID},
			{Key: "system_name", Value: resp.Name},
			{Key: "host", Value: hostname},  // Add host tag
		}

		if resp.Manufacturer != "" {
			systemTags = append(systemTags, tags.Tag{Key: "manufacturer", Value: resp.Manufacturer})
		}
		if resp.Model != "" {
			systemTags = append(systemTags, tags.Tag{Key: "model", Value: resp.Model})
		}
		if resp.SerialNumber != "" {
			systemTags = append(systemTags, tags.Tag{Key: "serial_number", Value: resp.SerialNumber})
		}
		if resp.UUID != "" {
			systemTags = append(systemTags, tags.Tag{Key: "system_uuid", Value: resp.UUID})
		}
		if resp.SystemType != "" {
			systemTags = append(systemTags, tags.Tag{Key: "system_type", Value: resp.SystemType})
		}
		if resp.BiosVersion != "" {
			systemTags = append(systemTags, tags.Tag{Key: "bios_version", Value: resp.BiosVersion})
		}
		if resp.Status != nil && resp.Status.State != "" {
			systemTags = append(systemTags, tags.Tag{Key: "state", Value: resp.Status.State})
		}
		if resp.AssetTag != "" {
			systemTags = append(systemTags, tags.Tag{Key: "asset_tag", Value: resp.AssetTag})
		}
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

		// Add health state metric
		if resp.Status != nil {
			health := mapHealthState(resp.Status.Health)
			datapoints = append(datapoints, data_store.DataPoint{
				Name:      "hardware.system.health",
				Timestamp: timestamp,
				Value:     float32(health),
				Tags:      systemTags,
			})
		}

		// Add power state metric
		if resp.PowerState != "" {
			powerState := mapPowerState(resp.PowerState)
			datapoints = append(datapoints, data_store.DataPoint{
				Name:      "hardware.system.power.state",
				Timestamp: timestamp,
				Value:     float32(powerState),
				Tags:      systemTags,
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
				datapoints = append(datapoints, data_store.DataPoint{
					Name:      "hardware.system.cpu.count",
					Timestamp: timestamp,
					Value:     float32(procSummary.Count),
					Tags:      systemTags,
				})

				// Add processor health if available
				if procSummary.Status != nil && procSummary.Status.Health != "" {
					datapoints = append(datapoints, data_store.DataPoint{
						Name:      "hardware.system.cpu.health",
						Timestamp: timestamp,
						Value:     float32(mapHealthState(procSummary.Status.Health)),
						Tags:      systemTags,
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
				datapoints = append(datapoints, data_store.DataPoint{
					Name:      "hardware.system.memory.size",
					Timestamp: timestamp,
					Value:     memSummary.TotalSystemMemoryGiB,
					Tags:      systemTags,
				})

				// Add memory health if available
				if memSummary.Status != nil && memSummary.Status.Health != "" {
					datapoints = append(datapoints, data_store.DataPoint{
						Name:      "hardware.system.memory.health",
						Timestamp: timestamp,
						Value:     float32(mapHealthState(memSummary.Status.Health)),
						Tags:      systemTags,
					})
				}
			}
		}
	}

	return datapoints, nil
}

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
		// Get thermal data for this chassis
		thermalPath := chassisPath + "/Thermal"
		resp, err := c.client.Get(ctx, thermalPath)
		if err != nil {
			c.logger.Warn().
				Err(err).
				Str("path", thermalPath).
				Msg("Failed to get thermal data, skipping")
			continue
		}

		// Extract chassis ID and name for tags - follow REDFISH-TAGS.md conventions
		chassisResp, err := c.client.Get(ctx, chassisPath)
		if err != nil {
			c.logger.Warn().
				Err(err).
				Str("path", chassisPath).
				Msg("Failed to get chassis details")
		}

		chassisTags := []tags.Tag{
			{Key: "chassis_id", Value: chassisResp.ID},
			{Key: "chassis_name", Value: chassisResp.Name},
		}

		// Add additional chassis tags according to REDFISH-TAGS.md
		// Extract ChassisType from Raw data if available
		var chassisRawData map[string]interface{}
		if err := json.Unmarshal(chassisResp.Raw, &chassisRawData); err == nil {
			if chassisType, ok := chassisRawData["ChassisType"].(string); ok && chassisType != "" {
				chassisTags = append(chassisTags, tags.Tag{Key: "chassis_type", Value: chassisType})
			}
		}
		if chassisResp.Manufacturer != "" {
			chassisTags = append(chassisTags, tags.Tag{Key: "manufacturer", Value: chassisResp.Manufacturer})
		}
		if chassisResp.Model != "" {
			chassisTags = append(chassisTags, tags.Tag{Key: "model", Value: chassisResp.Model})
		}
		if chassisResp.SerialNumber != "" {
			chassisTags = append(chassisTags, tags.Tag{Key: "serial_number", Value: chassisResp.SerialNumber})
		}
		if chassisResp.PartNumber != "" {
			chassisTags = append(chassisTags, tags.Tag{Key: "part_number", Value: chassisResp.PartNumber})
		}
		if chassisResp.AssetTag != "" {
			chassisTags = append(chassisTags, tags.Tag{Key: "asset_tag", Value: chassisResp.AssetTag})
		}
		if chassisResp.SKU != "" {
			chassisTags = append(chassisTags, tags.Tag{Key: "sku", Value: chassisResp.SKU})
		}
		if chassisResp.Status != nil && chassisResp.Status.State != "" {
			chassisTags = append(chassisTags, tags.Tag{Key: "state", Value: chassisResp.Status.State})
		}

		// Get physical dimensions if available
		var chassisPhysical struct {
			HeightMm float32 `json:"HeightMm"`
			WidthMm float32 `json:"WidthMm"`
			DepthMm float32 `json:"DepthMm"`
			WeightKg float32 `json:"WeightKg"`
			LocationIndicatorActive bool `json:"LocationIndicatorActive"`
		}
		rawJSON, _ := json.Marshal(chassisResp)
		if err := json.Unmarshal(rawJSON, &chassisPhysical); err == nil {
			if chassisPhysical.HeightMm > 0 {
				chassisTags = append(chassisTags, tags.Tag{Key: "height_mm", Value: fmt.Sprintf("%g", chassisPhysical.HeightMm)})
			}
			if chassisPhysical.WidthMm > 0 {
				chassisTags = append(chassisTags, tags.Tag{Key: "width_mm", Value: fmt.Sprintf("%g", chassisPhysical.WidthMm)})
			}
			if chassisPhysical.DepthMm > 0 {
				chassisTags = append(chassisTags, tags.Tag{Key: "depth_mm", Value: fmt.Sprintf("%g", chassisPhysical.DepthMm)})
			}
			if chassisPhysical.WeightKg > 0 {
				chassisTags = append(chassisTags, tags.Tag{Key: "weight_kg", Value: fmt.Sprintf("%g", chassisPhysical.WeightKg)})
			}
			chassisTags = append(chassisTags, tags.Tag{Key: "location_indicator", Value: fmt.Sprintf("%t", chassisPhysical.LocationIndicatorActive)})
		}

		// Add host tag if available
		if hostName != "" {
			chassisTags = append(chassisTags, tags.Tag{Key: "host", Value: hostName})
		}

		// Process temperature sensors
		for i, temp := range resp.Temperatures {
			var tempReading struct {
				Name              string  `json:"Name"`
				ReadingCelsius    float32 `json:"ReadingCelsius"`
				UpperThreshold    float32 `json:"UpperThresholdCritical"`
				LowerThreshold    float32 `json:"LowerThresholdCritical"`
				PhysicalContext   string  `json:"PhysicalContext"`
				Status            *Status `json:"Status"`
				SensorNumber      int     `json:"SensorNumber"`
				RelatedItemCount  int     `json:"RelatedItem@odata.count"`
			}
			rawJSON, _ := json.Marshal(temp)
			if err := json.Unmarshal(rawJSON, &tempReading); err != nil {
				c.logger.Warn().
					Err(err).
					Int("index", i).
					Msg("Failed to parse temperature data")
				continue
			}

			// Skip if no reading available
			if tempReading.ReadingCelsius == 0 {
				continue
			}

			// Create sensor-specific tags following REDFISH-TAGS.md conventions
			sensorTags := append([]tags.Tag{}, chassisTags...)
			sensorTags = append(sensorTags, tags.Tag{Key: "sensor_name", Value: tempReading.Name})

			// Add sensor number if available
			if tempReading.SensorNumber != 0 {
				sensorTags = append(sensorTags, tags.Tag{Key: "sensor_number", Value: fmt.Sprintf("%d", tempReading.SensorNumber)})
			}

			// Look for controller name in sensor name
			if strings.Contains(strings.ToLower(tempReading.Name), "ctrl_a") ||
			   strings.Contains(strings.ToLower(tempReading.Name), "controller_a") {
				sensorTags = append(sensorTags, tags.Tag{Key: "controller", Value: "A"})
			} else if strings.Contains(strings.ToLower(tempReading.Name), "ctrl_b") ||
			          strings.Contains(strings.ToLower(tempReading.Name), "controller_b") {
				sensorTags = append(sensorTags, tags.Tag{Key: "controller", Value: "B"})
			}

			if tempReading.PhysicalContext != "" {
				sensorTags = append(sensorTags, tags.Tag{Key: "physical_context", Value: tempReading.PhysicalContext})
			}

			// Add related items count if available
			if tempReading.RelatedItemCount > 0 {
				sensorTags = append(sensorTags, tags.Tag{Key: "related_items_count", Value: fmt.Sprintf("%d", tempReading.RelatedItemCount)})
			}

			// Add temperature reading
			datapoints = append(datapoints, data_store.DataPoint{
				Name:      "hardware.temperature",
				Timestamp: timestamp,
				Value:     tempReading.ReadingCelsius,
				Tags:      sensorTags,
			})

			// Add health state if available
			if tempReading.Status != nil {
				health := mapHealthState(tempReading.Status.Health)
				datapoints = append(datapoints, data_store.DataPoint{
					Name:      "hardware.temperature.health",
					Timestamp: timestamp,
					Value:     float32(health),
					Tags:      sensorTags,
				})
			}
		}

		// Process fans
		for i, fan := range resp.Fans {
			var fanReading struct {
				Name           string  `json:"Name"`
				Reading        float32 `json:"Reading"`
				ReadingUnits   string  `json:"ReadingUnits"`
				PhysicalContext string  `json:"PhysicalContext"`
				Status         *Status `json:"Status"`
			}
			rawJSON, _ := json.Marshal(fan)
			if err := json.Unmarshal(rawJSON, &fanReading); err != nil {
				c.logger.Warn().
					Err(err).
					Int("index", i).
					Msg("Failed to parse fan data")
				continue
			}

			// Skip if no reading available
			if fanReading.Reading == 0 {
				continue
			}

			// Create fan-specific tags
			fanTags := append([]tags.Tag{}, chassisTags...)
			fanTags = append(fanTags, tags.Tag{Key: "fan_name", Value: fanReading.Name})

			if fanReading.PhysicalContext != "" {
				fanTags = append(fanTags, tags.Tag{Key: "physical_context", Value: fanReading.PhysicalContext})
			}

			if fanReading.ReadingUnits != "" {
				fanTags = append(fanTags, tags.Tag{Key: "units", Value: fanReading.ReadingUnits})
			}

			// Add fan reading
			datapoints = append(datapoints, data_store.DataPoint{
				Name:      "hardware.fan.speed",
				Timestamp: timestamp,
				Value:     fanReading.Reading,
				Tags:      fanTags,
			})

			// Add health state if available
			if fanReading.Status != nil {
				health := mapHealthState(fanReading.Status.Health)
				datapoints = append(datapoints, data_store.DataPoint{
					Name:      "hardware.fan.health",
					Timestamp: timestamp,
					Value:     float32(health),
					Tags:      fanTags,
				})
			}
		}
	}

	return datapoints, nil
}

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
		}

		chassisTags := []tags.Tag{
			{Key: "chassis_id", Value: chassisResp.ID},
			{Key: "chassis_name", Value: chassisResp.Name},
		}

		// Add additional chassis tags according to REDFISH-TAGS.md
		// Extract ChassisType from Raw data if available
		var chassisRawData map[string]interface{}
		if err := json.Unmarshal(chassisResp.Raw, &chassisRawData); err == nil {
			if chassisType, ok := chassisRawData["ChassisType"].(string); ok && chassisType != "" {
				chassisTags = append(chassisTags, tags.Tag{Key: "chassis_type", Value: chassisType})
			}
		}
		if chassisResp.Manufacturer != "" {
			chassisTags = append(chassisTags, tags.Tag{Key: "manufacturer", Value: chassisResp.Manufacturer})
		}
		if chassisResp.Model != "" {
			chassisTags = append(chassisTags, tags.Tag{Key: "model", Value: chassisResp.Model})
		}
		if chassisResp.SerialNumber != "" {
			chassisTags = append(chassisTags, tags.Tag{Key: "serial_number", Value: chassisResp.SerialNumber})
		}
		if chassisResp.PartNumber != "" {
			chassisTags = append(chassisTags, tags.Tag{Key: "part_number", Value: chassisResp.PartNumber})
		}
		if chassisResp.AssetTag != "" {
			chassisTags = append(chassisTags, tags.Tag{Key: "asset_tag", Value: chassisResp.AssetTag})
		}
		if chassisResp.SKU != "" {
			chassisTags = append(chassisTags, tags.Tag{Key: "sku", Value: chassisResp.SKU})
		}
		if chassisResp.Status != nil && chassisResp.Status.State != "" {
			chassisTags = append(chassisTags, tags.Tag{Key: "state", Value: chassisResp.Status.State})
		}

		// Get physical dimensions if available
		var chassisPhysical struct {
			HeightMm float32 `json:"HeightMm"`
			WidthMm float32 `json:"WidthMm"`
			DepthMm float32 `json:"DepthMm"`
			WeightKg float32 `json:"WeightKg"`
			LocationIndicatorActive bool `json:"LocationIndicatorActive"`
		}
		rawJSON, _ := json.Marshal(chassisResp)
		if err := json.Unmarshal(rawJSON, &chassisPhysical); err == nil {
			if chassisPhysical.HeightMm > 0 {
				chassisTags = append(chassisTags, tags.Tag{Key: "height_mm", Value: fmt.Sprintf("%g", chassisPhysical.HeightMm)})
			}
			if chassisPhysical.WidthMm > 0 {
				chassisTags = append(chassisTags, tags.Tag{Key: "width_mm", Value: fmt.Sprintf("%g", chassisPhysical.WidthMm)})
			}
			if chassisPhysical.DepthMm > 0 {
				chassisTags = append(chassisTags, tags.Tag{Key: "depth_mm", Value: fmt.Sprintf("%g", chassisPhysical.DepthMm)})
			}
			if chassisPhysical.WeightKg > 0 {
				chassisTags = append(chassisTags, tags.Tag{Key: "weight_kg", Value: fmt.Sprintf("%g", chassisPhysical.WeightKg)})
			}
			chassisTags = append(chassisTags, tags.Tag{Key: "location_indicator", Value: fmt.Sprintf("%t", chassisPhysical.LocationIndicatorActive)})
		}

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
				PowerConsumedWatts float32 `json:"PowerConsumedWatts"`
				PowerRequestedWatts float32 `json:"PowerRequestedWatts"`
				PowerAvailableWatts float32 `json:"PowerAvailableWatts"`
				PowerCapacityWatts float32 `json:"PowerCapacityWatts"`
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
				TotalCores        int     `json:"TotalCores"`
				TotalThreads      int     `json:"TotalThreads"`
				MaxSpeedMHz       int     `json:"MaxSpeedMHz"`
				InstructionSet    string  `json:"InstructionSet"`
				Status            *Status `json:"Status"`
			}
			rawJSON, _ := json.Marshal(procResp)
			if err := json.Unmarshal(rawJSON, &procData); err != nil {
				c.logger.Warn().
					Err(err).
					Msg("Failed to parse processor data")
				continue
			}

			// Add processor core count
			if procData.TotalCores > 0 {
				datapoints = append(datapoints, data_store.DataPoint{
					Name:      "processor.total_cores",
					Timestamp: timestamp,
					Value:     float32(procData.TotalCores),
					Tags:      procTags,
				})
			}

			// Add processor thread count
			if procData.TotalThreads > 0 {
				datapoints = append(datapoints, data_store.DataPoint{
					Name:      "processor.total_threads",
					Timestamp: timestamp,
					Value:     float32(procData.TotalThreads),
					Tags:      procTags,
				})
			}

			// Add processor max speed
			if procData.MaxSpeedMHz > 0 {
				datapoints = append(datapoints, data_store.DataPoint{
					Name:      "processor.max_speed_mhz",
					Timestamp: timestamp,
					Value:     float32(procData.MaxSpeedMHz),
					Tags:      procTags,
				})
			}

			// Add health state if available
			if procData.Status != nil {
				health := mapHealthState(procData.Status.Health)
				datapoints = append(datapoints, data_store.DataPoint{
					Name:      "processor.health",
					Timestamp: timestamp,
					Value:     float32(health),
					Tags:      procTags,
				})
			}
		}
	}

	return datapoints, nil
}

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
				CapacityMiB        int     `json:"CapacityMiB"`
				OperatingSpeedMhz  int     `json:"OperatingSpeedMhz"`
				MemoryType         string  `json:"MemoryDeviceType"`
				DataWidthBits      int     `json:"DataWidthBits"`
				RankCount          int     `json:"RankCount"`
				Status             *Status `json:"Status"`
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

			// Add memory capacity
			if dimData.CapacityMiB > 0 {
				datapoints = append(datapoints, data_store.DataPoint{
					Name:      "memory.capacity_mib",
					Timestamp: timestamp,
					Value:     float32(dimData.CapacityMiB),
					Tags:      dimTags,
				})
			}

			// Add memory speed
			if dimData.OperatingSpeedMhz > 0 {
				datapoints = append(datapoints, data_store.DataPoint{
					Name:      "memory.speed_mhz",
					Timestamp: timestamp,
					Value:     float32(dimData.OperatingSpeedMhz),
					Tags:      dimTags,
				})
			}

			// Add health state if available
			if dimData.Status != nil {
				health := mapHealthState(dimData.Status.Health)
				datapoints = append(datapoints, data_store.DataPoint{
					Name:      "memory.health",
					Timestamp: timestamp,
					Value:     float32(health),
					Tags:      dimTags,
				})
			}
		}
	}

	return datapoints, nil
}

// Helper functions

// collectStorageMetrics gathers storage metrics from a Redfish system
func (c *GenericCollector) collectStorageMetrics(ctx context.Context, timestamp time.Time) ([]data_store.DataPoint, error) {
	var datapoints []data_store.DataPoint

	for _, systemID := range c.systems {
		// Get Storage Collection
		storageCollectionPath := fmt.Sprintf("Systems/%s/Storage", systemID)

		// Fetch storage collection
		storageCollectionResp, err := c.client.Get(ctx, storageCollectionPath)
		if err != nil {
			c.logger.Warn().
				Err(err).
				Str("path", storageCollectionPath).
				Msg("Failed to get storage collection")
			continue
		}

		// Parse the collection
		var storageCollection struct {
			Members []struct {
				OdataID string `json:"@odata.id"`
			} `json:"Members"`
		}

		if err := json.Unmarshal(storageCollectionResp.Raw, &storageCollection); err != nil {
			c.logger.Warn().
				Err(err).
				Str("path", storageCollectionPath).
				Msg("Failed to parse storage collection")
			continue
		}

		// Process each storage controller/subsystem
		for _, member := range storageCollection.Members {
			if member.OdataID == "" {
				continue
			}

			// Fetch storage controller
			storageResp, err := c.client.Get(ctx, member.OdataID)
			if err != nil {
				c.logger.Warn().
					Err(err).
					Str("path", member.OdataID).
					Msg("Failed to get storage details")
				continue
			}

			// Parse details
			var storage struct {
				ID           string  `json:"Id"`
				Name         string  `json:"Name"`
				Description  string  `json:"Description"`
				StorageControllers []struct {
					MemberId    string  `json:"MemberId"`
					Name        string  `json:"Name"`
					Status      *Status `json:"Status"`
					SpeedGbps   float32 `json:"SpeedGbps"`
				} `json:"StorageControllers"`
				Drives []struct {
					OdataID string `json:"@odata.id"`
				} `json:"Drives"`
				Volumes struct {
					OdataID string `json:"@odata.id"`
				} `json:"Volumes"`
				Status *Status `json:"Status"`
			}

			if err := json.Unmarshal(storageResp.Raw, &storage); err != nil {
				c.logger.Warn().
					Err(err).
					Str("path", member.OdataID).
					Msg("Failed to parse storage details")
				continue
			}

			// Storage controller tags - suivant les conventions de REDFISH-TAGS.md
			storageTags := []tags.Tag{
				{Key: "system_id", Value: systemID},
				{Key: "storage_id", Value: storage.ID},
				{Key: "storage_name", Value: storage.Name},
			}

			// Storage health state
			if storage.Status != nil && storage.Status.Health != "" {
				datapoints = append(datapoints, data_store.DataPoint{
					Name:      "hardware.storage.health",
					Value:     float32(mapHealthState(storage.Status.Health)),
					Timestamp: timestamp,
					Tags:      storageTags,
				})
			}

			// Storage controllers
			for _, controller := range storage.StorageControllers {
				// Controller tags
				controllerTags := append([]tags.Tag{}, storageTags...)
				controllerTags = append(controllerTags,
					tags.Tag{Key: "controller_id", Value: controller.MemberId},
					tags.Tag{Key: "controller_name", Value: controller.Name},
				)

				// Controller health
				if controller.Status != nil && controller.Status.Health != "" {
					datapoints = append(datapoints, data_store.DataPoint{
						Name:      "hardware.storage.controller.health",
						Value:     float32(mapHealthState(controller.Status.Health)),
						Timestamp: timestamp,
						Tags:      controllerTags,
					})
				}

				// Controller speed
				if controller.SpeedGbps > 0 {
					datapoints = append(datapoints, data_store.DataPoint{
						Name:      "hardware.storage.controller.speed_gbps",
						Value:     controller.SpeedGbps,
						Timestamp: timestamp,
						Tags:      controllerTags,
					})
				}
			}

			// Process drives if available
			for _, drive := range storage.Drives {
				if drive.OdataID == "" {
					continue
				}

				// Fetch drive details
				driveResp, err := c.client.Get(ctx, drive.OdataID)
				if err != nil {
					c.logger.Warn().
						Err(err).
						Str("path", drive.OdataID).
						Msg("Failed to get drive details")
					continue
				}

				// Parse drive details
				var driveInfo struct {
					ID            string  `json:"Id"`
					Name          string  `json:"Name"`
					Model         string  `json:"Model"`
					SerialNumber  string  `json:"SerialNumber"`
					MediaType     string  `json:"MediaType"`
					Protocol      string  `json:"Protocol"`
					CapacityBytes int64   `json:"CapacityBytes"`
					Status        *Status `json:"Status"`
					FailurePredicted bool  `json:"FailurePredicted"`
				}

				if err := json.Unmarshal(driveResp.Raw, &driveInfo); err != nil {
					c.logger.Warn().
						Err(err).
						Str("path", drive.OdataID).
						Msg("Failed to parse drive details")
					continue
				}

				// Drive tags - suivant les conventions de REDFISH-TAGS.md
				driveTags := append([]tags.Tag{}, storageTags...)
				driveTags = append(driveTags,
					tags.Tag{Key: "drive_id", Value: driveInfo.ID},
					tags.Tag{Key: "drive_name", Value: driveInfo.Name},
				)

				if driveInfo.Model != "" {
					driveTags = append(driveTags, tags.Tag{Key: "model", Value: driveInfo.Model})
				}
				if driveInfo.SerialNumber != "" {
					driveTags = append(driveTags, tags.Tag{Key: "serial_number", Value: driveInfo.SerialNumber})
				}
				if driveInfo.MediaType != "" {
					driveTags = append(driveTags, tags.Tag{Key: "media_type", Value: driveInfo.MediaType})
				}
				if driveInfo.Protocol != "" {
					driveTags = append(driveTags, tags.Tag{Key: "protocol", Value: driveInfo.Protocol})
				}

				// Drive health state
				if driveInfo.Status != nil && driveInfo.Status.Health != "" {
					datapoints = append(datapoints, data_store.DataPoint{
						Name:      "hardware.storage.drive.health",
						Value:     float32(mapHealthState(driveInfo.Status.Health)),
						Timestamp: timestamp,
						Tags:      driveTags,
					})
				}

				// Drive capacity
				if driveInfo.CapacityBytes > 0 {
					datapoints = append(datapoints, data_store.DataPoint{
						Name:      "hardware.storage.drive.capacity_bytes",
						Value:     float32(driveInfo.CapacityBytes),
						Timestamp: timestamp,
						Tags:      driveTags,
					})
				}

				// Drive failure prediction
				failurePredicted := 0.0
				if driveInfo.FailurePredicted {
					failurePredicted = 1.0
				}

				datapoints = append(datapoints, data_store.DataPoint{
					Name:      "hardware.storage.drive.failure_predicted",
					Value:     float32(failurePredicted),
					Timestamp: timestamp,
					Tags:      driveTags,
				})
			}
		}
	}

	return datapoints, nil
}

// collectNetworkMetrics gathers network metrics from a Redfish system
func (c *GenericCollector) collectNetworkMetrics(ctx context.Context, timestamp time.Time) ([]data_store.DataPoint, error) {
	var datapoints []data_store.DataPoint

	for _, systemID := range c.systems {
		// Normalize system ID by removing any redundant "Systems/" prefix
		// This fixes paths like "Systems/Systems/something" that can occur in some implementations
		normalizedSystemID := strings.TrimPrefix(systemID, "Systems/")

		// Get system details for hostname - will be added to network interface tags
		var hostname string
		sysResp, err := c.client.Get(ctx, systemID)
		if err == nil && sysResp.Name != "" {
			hostname = sysResp.Name
		}

		// Network interfaces path
		networkPath := fmt.Sprintf("Systems/%s/NetworkInterfaces", normalizedSystemID)

		// Fetch network collection
		networkResp, err := c.client.Get(ctx, networkPath)
		if err != nil {
			c.logger.Warn().
				Err(err).
				Str("path", networkPath).
				Msg("Failed to get network interfaces collection")

			// Try alternate path - EthernetInterfaces is also common
			alternatePath := fmt.Sprintf("Systems/%s/EthernetInterfaces", normalizedSystemID)
			networkResp, err = c.client.Get(ctx, alternatePath)
			if err != nil {
				c.logger.Warn().
					Err(err).
					Str("path", alternatePath).
					Msg("Failed to get ethernet interfaces collection")

					// Try another common pattern - adapters directly under system
					adaptersPath := fmt.Sprintf("Systems/%s/Adapters", normalizedSystemID)
					networkResp, err = c.client.Get(ctx, adaptersPath)
					if err != nil {
						c.logger.Warn().
							Err(err).
							Str("path", adaptersPath).
							Msg("Failed to get adapters collection")
						continue
					}
			}
		}

		// Parse the collection
		var networkCollection struct {
			Members []struct {
				OdataID string `json:"@odata.id"`
			} `json:"Members"`
		}

		if err := json.Unmarshal(networkResp.Raw, &networkCollection); err != nil {
			c.logger.Warn().
				Err(err).
				Str("path", networkPath).
				Msg("Failed to parse network collection")
			continue
		}

		// Process each network interface
		for _, member := range networkCollection.Members {
			if member.OdataID == "" {
				continue
			}

			// Fetch network interface
			networkDetailResp, err := c.client.Get(ctx, member.OdataID)
			if err != nil {
				c.logger.Warn().
					Err(err).
					Str("path", member.OdataID).
					Msg("Failed to get network interface details")
				continue
			}

			// Parse network interface details
			var network struct {
				ID            string  `json:"Id"`
				Name          string  `json:"Name"`
				Description   string  `json:"Description"`
				Status        *Status `json:"Status"`
				SpeedMbps     int     `json:"SpeedMbps"`
				MACAddress    string  `json:"MACAddress"`
				LinkStatus    string  `json:"LinkStatus"`
				InterfaceType string  `json:"InterfaceType"`
			}

			if err := json.Unmarshal(networkDetailResp.Raw, &network); err != nil {
				c.logger.Warn().
					Err(err).
					Str("path", member.OdataID).
					Msg("Failed to parse network interface details")
				continue
			}

			// Network interface tags - suivant les conventions de REDFISH-TAGS.md
			networkTags := []tags.Tag{
				{Key: "system_id", Value: systemID},
				{Key: "adapter_id", Value: network.ID},
				{Key: "adapter_name", Value: network.Name},
				{Key: "mac_address", Value: network.MACAddress},
				{Key: "interface_type", Value: network.InterfaceType},
			}

			// Add host tag if available
			if hostname != "" {
				networkTags = append(networkTags, tags.Tag{Key: "host", Value: hostname})
			}

			// Network health state
			if network.Status != nil && network.Status.Health != "" {
				datapoints = append(datapoints, data_store.DataPoint{
					Name:      "hardware.network.health",
					Value:     float32(mapHealthState(network.Status.Health)),
					Timestamp: timestamp,
					Tags:      networkTags,
				})
			}

			// Network speed
			if network.SpeedMbps > 0 {
				datapoints = append(datapoints, data_store.DataPoint{
					Name:      "hardware.network.speed_mbps",
					Value:     float32(network.SpeedMbps),
					Timestamp: timestamp,
					Tags:      networkTags,
				})
			}

			// Link status
			linkUp := 0.0
			if strings.ToLower(network.LinkStatus) == "up" || strings.ToLower(network.LinkStatus) == "linked" {
				linkUp = 1.0
			}

			datapoints = append(datapoints, data_store.DataPoint{
				Name:      "hardware.network.link_up",
				Value:     float32(linkUp),
				Timestamp: timestamp,
				Tags:      networkTags,
			})
		}
	}

	return datapoints, nil
}

// containsIgnoreCase checks if a string contains another string, ignoring case
func containsIgnoreCase(s, substr string) bool {
	return strings.Contains(
		strings.ToLower(s),
		strings.ToLower(substr),
	)
}

// mapHealthState converts Redfish health states to numeric values
// Using standard enum pattern for OpenTelemetry:
// 0: OK/Healthy
// 1: Warning/Degraded
// 2: Critical/Error
// 3: Unknown
func mapHealthState(health string) int {
	switch strings.ToLower(health) {
	case "ok":
		return 0 // OK/Healthy
	case "warning":
		return 1 // Warning/Degraded
	case "critical":
		return 2 // Critical/Error
	default:
		return 3 // Unknown
	}
}

// mapPowerState converts Redfish power states to numeric values
// Using standard enum pattern for power states:
// 0: Off
// 1: On
// 2: Powering On (transitional)
// 3: Powering Off (transitional)
// 4: Unknown/Other
func mapPowerState(state string) int {
	switch strings.ToLower(state) {
	case "on":
		return 1 // On
	case "off":
		return 0 // Off
	case "powering on":
		return 2 // Powering On
	case "powering off":
		return 3 // Powering Off
	default:
		return 4 // Unknown
	}
}

// isVersionGreaterOrEqual checks if a version string is >= the minimum required version
// Handles simple version strings like "1.2.3" or "1.2"
func isVersionGreaterOrEqual(version, minVersion string) bool {
	if version == "" {
		return false
	}

	// Remove leading 'v' if present
	if strings.HasPrefix(version, "v") {
		version = version[1:]
	}
	if strings.HasPrefix(minVersion, "v") {
		minVersion = minVersion[1:]
	}

	// Split into components
	vParts := strings.Split(version, ".")
	minParts := strings.Split(minVersion, ".")

	// Compare each component
	for i := 0; i < len(minParts); i++ {
		// If we ran out of components in version, treat as 0
		if i >= len(vParts) {
			return false
		}

		// Parse components as integers and compare
		vComp, err1 := strconv.Atoi(vParts[i])
		minComp, err2 := strconv.Atoi(minParts[i])

		// If we can't parse as integers, fall back to string comparison
		if err1 != nil || err2 != nil {
			if vParts[i] < minParts[i] {
				return false
			} else if vParts[i] > minParts[i] {
				return true
			}
			// If equal, continue to next component
			continue
		}

		// Compare numeric values
		if vComp < minComp {
			return false
		} else if vComp > minComp {
			return true
		}
		// If equal, continue to next component
	}

	// If we've compared all components and they're equal, return true
	return true
}