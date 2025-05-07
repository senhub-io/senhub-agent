package redfish

import (
	"context"
	"encoding/json"
	"fmt"
	"senhub-agent.go/internal/agent/services/data_store"
	"senhub-agent.go/internal/agent/services/logger"
	"senhub-agent.go/internal/agent/tags"
	"strings"
	"time"
)

// HPECollector implements HPE-specific Redfish collection
type HPECollector struct {
	*GenericCollector
	// HPE specific fields
	iloVersion string
	smartStorage bool
	isGen10     bool
}

// NewHPECollector creates a new collector for HPE servers
func NewHPECollector(endpoint, username, password string, logger *logger.Logger, verifySSL bool) (RedfishCollector, error) {
	// First create a generic collector as the base
	genericCollector, err := NewGenericCollector(endpoint, username, password, logger, verifySSL)
	if err != nil {
		return nil, err
	}

	// Cast to use the embedded fields and methods
	gc, ok := genericCollector.(*GenericCollector)
	if !ok {
		return nil, fmt.Errorf("unexpected collector type, cannot create HPE collector")
	}

	// Create the HPE collector with the generic one embedded
	collector := &HPECollector{
		GenericCollector: gc,
	}

	// Override the vendor type
	collector.vendorType = VendorHPE

	return collector, nil
}

// Connect implements HPE-specific connection logic
func (c *HPECollector) Connect(ctx context.Context) error {
	// First use the generic connect method
	if err := c.GenericCollector.Connect(ctx); err != nil {
		return err
	}

	// Get HPE-specific information
	if err := c.getHPEInfo(ctx); err != nil {
		c.logger.Warn().Err(err).Msg("Failed to get HPE-specific info")
		// Continue anyway, as this is not critical
	}

	return nil
}

// getHPEInfo retrieves HPE-specific information like iLO version
func (c *HPECollector) getHPEInfo(ctx context.Context) error {
	// Try to get HPE-specific manager (iLO) info
	resp, err := c.client.Get(ctx, "Managers/1")
	if err != nil {
		// Try alternative path
		resp, err = c.client.Get(ctx, "Managers/iLO")
		if err != nil {
			return fmt.Errorf("failed to get iLO info: %v", err)
		}
	}

	// Extract iLO version
	c.iloVersion = resp.FirmwareVersion

	// Determine if this is Gen10 or newer based on iLO version
	// iLO 5.x and higher correspond to Gen10 and newer
	if strings.HasPrefix(c.iloVersion, "5.") || strings.HasPrefix(c.iloVersion, "6.") {
		c.isGen10 = true
	}

	// Check for SmartStorage
	if c.systems != nil && len(c.systems) > 0 {
		// Try to access SmartStorage endpoint
		smartStoragePath := c.systems[0] + "/SmartStorage"
		smartResp, err := c.client.Get(ctx, smartStoragePath)
		if err == nil && smartResp != nil {
			c.smartStorage = true
		}
	}

	c.logger.Debug().
		Str("ilo_version", c.iloVersion).
		Bool("is_gen10", c.isGen10).
		Bool("smart_storage", c.smartStorage).
		Msg("Retrieved HPE-specific information")

	return nil
}

// GetSupportedCollections returns HPE-specific collection capabilities
func (c *HPECollector) GetSupportedCollections() []CollectionType {
	// HPE supports all generic collections plus some extras
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
func (c *HPECollector) IsSupported(collectionType CollectionType) bool {
	switch collectionType {
	case CollectionSystem, CollectionThermal, CollectionPower,
		CollectionProcessor, CollectionMemory, CollectionStorage,
		CollectionDrives, CollectionNetworkAdapter:
		return true
	default:
		return false
	}
}

// CollectMetrics gathers HPE-specific metrics
func (c *HPECollector) CollectMetrics(ctx context.Context, collectionType CollectionType, timestamp time.Time) ([]data_store.DataPoint, error) {
	// For most collection types, use the generic collector
	switch collectionType {
	case CollectionSystem:
		return c.collectSystemMetrics(ctx, timestamp)
	case CollectionStorage, CollectionDrives:
		if c.smartStorage {
			return c.collectSmartStorageMetrics(ctx, timestamp)
		}
		return c.collectStorageMetrics(ctx, timestamp)
	case CollectionNetworkAdapter:
		return c.collectNetworkMetrics(ctx, timestamp)
	default:
		// For other types, delegate to the generic collector
		return c.GenericCollector.CollectMetrics(ctx, collectionType, timestamp)
	}
}

// collectSystemMetrics gathers HPE-specific system metrics
func (c *HPECollector) collectSystemMetrics(ctx context.Context, timestamp time.Time) ([]data_store.DataPoint, error) {
	// Get the generic system metrics first
	datapoints, err := c.GenericCollector.collectSystemMetrics(ctx, timestamp)
	if err != nil {
		return nil, err
	}

	// Add HPE-specific system datapoints
	for _, systemPath := range c.systems {
		// Get HPE OEM data for this system
		resp, err := c.client.Get(ctx, systemPath)
		if err != nil {
			c.logger.Warn().
				Err(err).
				Str("path", systemPath).
				Msg("Failed to get HPE system details")
			continue
		}

		// Extract system tags
		systemTags := []tags.Tag{
			{Key: "system_id", Value: resp.ID},
			{Key: "system_name", Value: resp.Name},
			{Key: "vendor", Value: string(c.vendorType)},
			{Key: "ilo_version", Value: c.iloVersion},
		}

		// Add HPE-specific OEM data if available
		if resp.Oem != nil {
			hpeData, hasHPE := resp.Oem["Hpe"]
			if !hasHPE {
				// Try legacy "Hp" OEM key
				hpeData, hasHPE = resp.Oem["Hp"]
			}
			
			if hasHPE {
				hpeOem, ok := hpeData.(map[string]interface{})
				if ok {
					// Add serial number if available
					if serialNumber, has := hpeOem["SerialNumber"]; has {
						serialStr := fmt.Sprintf("%v", serialNumber)
						// Add serial as a tag
						systemTags = append(systemTags, tags.Tag{Key: "serial_number", Value: serialStr})
					}
					
					// Add product ID if available
					if productID, has := hpeOem["ProductID"]; has {
						productIDStr := fmt.Sprintf("%v", productID)
						systemTags = append(systemTags, tags.Tag{Key: "product_id", Value: productIDStr})
					}
				}
			}
		}

		// Add iLO version datapoint if available
		if c.iloVersion != "" {
			// We don't have a numeric value for version, so we use 1.0 as a sentinel value
			// The actual version is in the tags
			datapoints = append(datapoints, data_store.DataPoint{
				Name:      "system.ilo_firmware",
				Timestamp: timestamp,
				Value:     1.0, // Sentinel value
				Tags:      systemTags,
			})
		}
	}

	return datapoints, nil
}

// collectSmartStorageMetrics gathers HPE SmartStorage metrics
func (c *HPECollector) collectSmartStorageMetrics(ctx context.Context, timestamp time.Time) ([]data_store.DataPoint, error) {
	if len(c.systems) == 0 {
		return nil, fmt.Errorf("no systems found")
	}

	var datapoints []data_store.DataPoint

	// Collect metrics for each system
	for _, systemPath := range c.systems {
		// Get SmartStorage collection
		smartPath := systemPath + "/SmartStorage"
		_, err := c.client.Get(ctx, smartPath)
		if err != nil {
			c.logger.Warn().
				Err(err).
				Str("path", smartPath).
				Msg("Failed to get SmartStorage collection, skipping")
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
			{Key: "vendor", Value: string(c.vendorType)},
		}

		// Get array controllers
		arraysPath := smartPath + "/ArrayControllers"
		arraysResp, err := c.client.Get(ctx, arraysPath)
		if err != nil {
			c.logger.Warn().
				Err(err).
				Str("path", arraysPath).
				Msg("Failed to get ArrayControllers collection, skipping")
			continue
		}

		// Process each array controller
		for _, arrayMember := range arraysResp.Members {
			arrayPath, ok := arrayMember["@odata.id"]
			if !ok {
				continue
			}

			// Get controller details
			arrayResp, err := c.client.Get(ctx, arrayPath)
			if err != nil {
				c.logger.Warn().
					Err(err).
					Str("path", arrayPath).
					Msg("Failed to get array controller details, skipping")
				continue
			}

			// Create controller-specific tags
			controllerTags := append([]tags.Tag{}, systemTags...)
			controllerTags = append(controllerTags, tags.Tag{Key: "controller_id", Value: arrayResp.ID})
			controllerTags = append(controllerTags, tags.Tag{Key: "controller_name", Value: arrayResp.Name})

			// Extract controller metrics
			var arrayData struct {
				Model            string   `json:"Model"`
				SerialNumber     string   `json:"SerialNumber"`
				FirmwareVersion  string   `json:"FirmwareVersion"`
				Status           *Status  `json:"Status"`
			}
			rawJSON, _ := json.Marshal(arrayResp)
			if err := json.Unmarshal(rawJSON, &arrayData); err != nil {
				c.logger.Warn().
					Err(err).
					Msg("Failed to parse array controller data")
				continue
			}

			// Add model and firmware info as tags
			if arrayData.Model != "" {
				controllerTags = append(controllerTags, tags.Tag{Key: "model", Value: arrayData.Model})
			}
			if arrayData.SerialNumber != "" {
				controllerTags = append(controllerTags, tags.Tag{Key: "serial_number", Value: arrayData.SerialNumber})
			}
			if arrayData.FirmwareVersion != "" {
				controllerTags = append(controllerTags, tags.Tag{Key: "firmware_version", Value: arrayData.FirmwareVersion})
			}

			// Add controller health state if available
			if arrayData.Status != nil {
				health := mapHealthState(arrayData.Status.Health)
				datapoints = append(datapoints, data_store.DataPoint{
					Name:      "storage.controller.health",
					Timestamp: timestamp,
					Value:     float32(health),
					Tags:      controllerTags,
				})
			}

			// Get physical drives
			drivesPath := arrayPath + "/DiskDrives"
			drivesResp, err := c.client.Get(ctx, drivesPath)
			if err != nil {
				c.logger.Warn().
					Err(err).
					Str("path", drivesPath).
					Msg("Failed to get disk drives collection, skipping")
				continue
			}

			// Process physical drives
			for _, driveMember := range drivesResp.Members {
				drivePath, ok := driveMember["@odata.id"]
				if !ok {
					continue
				}

				// Get drive details
				driveResp, err := c.client.Get(ctx, drivePath)
				if err != nil {
					c.logger.Warn().
						Err(err).
						Str("path", drivePath).
						Msg("Failed to get drive details, skipping")
					continue
				}

				// Create drive-specific tags
				driveTags := append([]tags.Tag{}, controllerTags...)
				driveTags = append(driveTags, tags.Tag{Key: "drive_id", Value: driveResp.ID})
				driveTags = append(driveTags, tags.Tag{Key: "drive_name", Value: driveResp.Name})

				// Extract drive metrics
				var driveData struct {
					CapacityGB       float32 `json:"CapacityGB"`
					MediaType        string  `json:"MediaType"`
					InterfaceType    string  `json:"InterfaceType"`
					SerialNumber     string  `json:"SerialNumber"`
					Model            string  `json:"Model"`
					RotationalSpeedRpm int   `json:"RotationalSpeedRpm"`
					Status           *Status `json:"Status"`
				}
				rawJSON, _ := json.Marshal(driveResp)
				if err := json.Unmarshal(rawJSON, &driveData); err != nil {
					c.logger.Warn().
						Err(err).
						Msg("Failed to parse drive data")
					continue
				}

				// Add model and serial as tags
				if driveData.Model != "" {
					driveTags = append(driveTags, tags.Tag{Key: "model", Value: driveData.Model})
				}
				if driveData.SerialNumber != "" {
					driveTags = append(driveTags, tags.Tag{Key: "serial_number", Value: driveData.SerialNumber})
				}
				
				// Add media type and interface as tags
				if driveData.MediaType != "" {
					driveTags = append(driveTags, tags.Tag{Key: "media_type", Value: driveData.MediaType})
				}
				if driveData.InterfaceType != "" {
					driveTags = append(driveTags, tags.Tag{Key: "interface_type", Value: driveData.InterfaceType})
				}

				// Add drive capacity
				if driveData.CapacityGB > 0 {
					datapoints = append(datapoints, data_store.DataPoint{
						Name:      "storage.drive.capacity_gb",
						Timestamp: timestamp,
						Value:     driveData.CapacityGB,
						Tags:      driveTags,
					})
				}

				// Add rotation speed for HDDs
				if driveData.RotationalSpeedRpm > 0 {
					datapoints = append(datapoints, data_store.DataPoint{
						Name:      "storage.drive.rotation_rpm",
						Timestamp: timestamp,
						Value:     float32(driveData.RotationalSpeedRpm),
						Tags:      driveTags,
					})
				}

				// Add health state
				if driveData.Status != nil {
					health := mapHealthState(driveData.Status.Health)
					datapoints = append(datapoints, data_store.DataPoint{
						Name:      "storage.drive.health",
						Timestamp: timestamp,
						Value:     float32(health),
						Tags:      driveTags,
					})
				}
			}

			// Get logical drives
			logicalPath := arrayPath + "/LogicalDrives"
			logicalResp, err := c.client.Get(ctx, logicalPath)
			if err != nil {
				c.logger.Warn().
					Err(err).
					Str("path", logicalPath).
					Msg("Failed to get logical drives collection, skipping")
				continue
			}

			// Process logical drives
			for _, logicalMember := range logicalResp.Members {
				volumePath, ok := logicalMember["@odata.id"]
				if !ok {
					continue
				}

				// Get logical drive details
				volumeResp, err := c.client.Get(ctx, volumePath)
				if err != nil {
					c.logger.Warn().
						Err(err).
						Str("path", volumePath).
						Msg("Failed to get logical drive details, skipping")
					continue
				}

				// Create logical drive-specific tags
				volumeTags := append([]tags.Tag{}, controllerTags...)
				volumeTags = append(volumeTags, tags.Tag{Key: "volume_id", Value: volumeResp.ID})
				volumeTags = append(volumeTags, tags.Tag{Key: "volume_name", Value: volumeResp.Name})

				// Extract logical drive metrics
				var volumeData struct {
					CapacityGB      float32  `json:"CapacityGB"`
					Raid            string   `json:"Raid"`
					LogicalDriveType string  `json:"LogicalDriveType"`
					Status          *Status  `json:"Status"`
				}
				rawJSON, _ := json.Marshal(volumeResp)
				if err := json.Unmarshal(rawJSON, &volumeData); err != nil {
					c.logger.Warn().
						Err(err).
						Msg("Failed to parse logical drive data")
					continue
				}

				// Add RAID level and type as tags
				if volumeData.Raid != "" {
					volumeTags = append(volumeTags, tags.Tag{Key: "raid_level", Value: volumeData.Raid})
				}
				if volumeData.LogicalDriveType != "" {
					volumeTags = append(volumeTags, tags.Tag{Key: "drive_type", Value: volumeData.LogicalDriveType})
				}

				// Add logical drive capacity
				if volumeData.CapacityGB > 0 {
					datapoints = append(datapoints, data_store.DataPoint{
						Name:      "storage.volume.capacity_gb",
						Timestamp: timestamp,
						Value:     volumeData.CapacityGB,
						Tags:      volumeTags,
					})
				}

				// Add health state
				if volumeData.Status != nil {
					health := mapHealthState(volumeData.Status.Health)
					datapoints = append(datapoints, data_store.DataPoint{
						Name:      "storage.volume.health",
						Timestamp: timestamp,
						Value:     float32(health),
						Tags:      volumeTags,
					})
				}
			}
		}
	}

	return datapoints, nil
}

// collectStorageMetrics gathers standard Redfish storage metrics for HPE servers without SmartStorage
func (c *HPECollector) collectStorageMetrics(ctx context.Context, timestamp time.Time) ([]data_store.DataPoint, error) {
	// Use the generic storage collection method
	return c.GenericCollector.collectStorageMetrics(ctx, timestamp)
}

// collectNetworkMetrics gathers HPE-specific network metrics
func (c *HPECollector) collectNetworkMetrics(ctx context.Context, timestamp time.Time) ([]data_store.DataPoint, error) {
	// Use the generic network collection method
	return c.GenericCollector.collectNetworkMetrics(ctx, timestamp)
}