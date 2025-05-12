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

// StorageCollector is a specialized implementation for storage systems like Dell PowerVault
type StorageCollector struct {
	*GenericCollector
	storageVolumes  []string
	storageControllers []string
}

// NewStorageCollector creates a new collector for storage devices
func NewStorageCollector(endpoint, username, password string, logger *logger.Logger, verifySSL bool) (RedfishCollector, error) {
	// Create generic collector as base
	genericCollector, err := NewGenericCollector(endpoint, username, password, logger, verifySSL)
	if err != nil {
		return nil, fmt.Errorf("failed to create generic collector: %v", err)
	}

	return &StorageCollector{
		GenericCollector: genericCollector.(*GenericCollector),
		storageVolumes:  make([]string, 0),
		storageControllers: make([]string, 0),
	}, nil
}

// Connect establishes connection and discovers storage-specific resources
func (c *StorageCollector) Connect(ctx context.Context) error {
	// Connect using the generic implementation
	if err := c.GenericCollector.Connect(ctx); err != nil {
		return err
	}

	// Check for Storage root collection (used by storage systems like PowerVault)
	storageResp, err := c.client.Get(ctx, "Storage")
	if err == nil {
		// Extract storage controllers from members
		c.storageControllers = make([]string, 0, len(storageResp.Members))
		for _, member := range storageResp.Members {
			if id, ok := member["@odata.id"]; ok {
				normalizedPath := strings.TrimPrefix(id, "/redfish/v1/")
				c.storageControllers = append(c.storageControllers, normalizedPath)
				
				// Attempt to get volumes for each controller
				volumesPath := normalizedPath + "/Volumes"
				volumesResp, err := c.client.Get(ctx, volumesPath)
				if err == nil && len(volumesResp.Members) > 0 {
					// We found volumes, add them to our list
					for _, volume := range volumesResp.Members {
						if volId, ok := volume["@odata.id"]; ok {
							c.storageVolumes = append(c.storageVolumes, strings.TrimPrefix(volId, "/redfish/v1/"))
						}
					}
				}
			}
		}
		
		c.logger.Debug().
			Int("controller_count", len(c.storageControllers)).
			Int("volume_count", len(c.storageVolumes)).
			Msg("Discovered storage resources")
			
		// Set vendor type to storage
		c.vendorType = VendorStorage
	}

	return nil
}

// GetVendorType returns the vendor type for this collector
func (c *StorageCollector) GetVendorType() VendorType {
	return VendorStorage
}

// IsSupported checks if a specific collection type is supported
func (c *StorageCollector) IsSupported(collectionType CollectionType) bool {
	switch collectionType {
	case CollectionSystem, CollectionThermal, CollectionPower, CollectionStorage, CollectionNetworkAdapter:
		return true
	default:
		return false
	}
}

// GetSupportedCollections returns all supported collection types
func (c *StorageCollector) GetSupportedCollections() []CollectionType {
	return []CollectionType{
		CollectionSystem,
		CollectionThermal, 
		CollectionPower,
		CollectionStorage,
		CollectionNetworkAdapter,
	}
}

// CollectMetrics gathers metrics for the specified collection type
func (c *StorageCollector) CollectMetrics(ctx context.Context, collectionType CollectionType, timestamp time.Time) ([]data_store.DataPoint, error) {
	switch collectionType {
	case CollectionSystem, CollectionThermal, CollectionPower:
		// Use generic implementation for these
		return c.GenericCollector.CollectMetrics(ctx, collectionType, timestamp)
	case CollectionStorage:
		return c.collectStorageMetrics(ctx, timestamp)
	case CollectionNetworkAdapter:
		return c.collectNetworkMetrics(ctx, timestamp)
	default:
		return nil, fmt.Errorf("unsupported collection type: %s", collectionType)
	}
}

// collectVolumeMetrics collects metrics for storage volumes
func (c *StorageCollector) collectStorageMetrics(ctx context.Context, timestamp time.Time) ([]data_store.DataPoint, error) {
	var datapoints []data_store.DataPoint

	// Get system name to use as the host tag
	var hostName string

	// Try to get host info
	if len(c.systems) > 0 {
		sysResp, err := c.client.Get(ctx, c.systems[0])
		if err == nil && sysResp.Name != "" {
			hostName = sysResp.Name
		}
	}

	// If hostname is empty, try to get from the service root
	if hostName == "" {
		rootResp, err := c.client.Get(ctx, "")
		if err == nil && rootResp.UUID != "" {
			hostName = rootResp.UUID
		}
	}

	// If we have a storage-specific root collection
	if len(c.storageControllers) > 0 {
		for _, controllerPath := range c.storageControllers {
			// Get controller details
			ctrlResp, err := c.client.Get(ctx, controllerPath)
			if err != nil {
				c.logger.Warn().
					Err(err).
					Str("path", controllerPath).
					Msg("Failed to get storage controller details")
				continue
			}

			// Get controller ID and map it to A/B if it contains those letters
			controllerID := ctrlResp.ID
			controllerName := ctrlResp.Name

			// Extract simple controller letter if it looks like "controller_a" -> "A"
			var controllerLetter string
			if strings.Contains(strings.ToLower(controllerID), "_a") {
				controllerLetter = "A"
			} else if strings.Contains(strings.ToLower(controllerID), "_b") {
				controllerLetter = "B"
			}

			// Controller tags
			controllerTags := []tags.Tag{
				{Key: "controller_id", Value: controllerID},
				{Key: "controller_name", Value: controllerName},
				{Key: "controller", Value: controllerLetter},
				{Key: "controller_type", Value: "storage"},
			}

			// Add host tag if available
			if hostName != "" {
				controllerTags = append(controllerTags, tags.Tag{Key: "host", Value: hostName})
			}

			// Controller health
			if ctrlResp.Status != nil && ctrlResp.Status.Health != "" {
				datapoints = append(datapoints, data_store.DataPoint{
					Name:      "hardware.storage.controller.health",
					Timestamp: timestamp,
					Value:     float32(mapHealthState(ctrlResp.Status.Health)),
					Tags:      controllerTags,
				})
			}
			
			// Process drives for this controller
			drivesPath := controllerPath + "/Drives"
			drivesResp, err := c.client.Get(ctx, drivesPath)
			if err == nil {
				// Process each drive
				for _, member := range drivesResp.Members {
					if drivePath, ok := member["@odata.id"]; ok {
						driveResp, err := c.client.Get(ctx, drivePath)
						if err != nil {
							c.logger.Warn().
								Err(err).
								Str("path", drivePath).
								Msg("Failed to get drive details")
							continue
						}
						
						// Extract drive information
						var driveInfo struct {
							ID            string  `json:"Id"`
							Name          string  `json:"Name"`
							Model         string  `json:"Model"`
							Manufacturer  string  `json:"Manufacturer"`
							SerialNumber  string  `json:"SerialNumber"`
							MediaType     string  `json:"MediaType"`
							CapacityBytes int64   `json:"CapacityBytes"`
							Protocol      string  `json:"Protocol"`
							Status        *Status `json:"Status"`
							BlockSizeBytes int64  `json:"BlockSizeBytes"`
							RotationSpeedRPM int  `json:"RotationSpeedRPM"`
							FailurePredicted bool  `json:"FailurePredicted"`
							PhysicalLocation struct {
								PartLocation struct {
									LocationType string `json:"LocationType"`
									ServiceLabel string `json:"ServiceLabel"`
									LocationOrdinalValue int `json:"LocationOrdinalValue"`
								} `json:"PartLocation"`
							} `json:"PhysicalLocation"`
						}
						
						if err := json.Unmarshal(driveResp.Raw, &driveInfo); err != nil {
							c.logger.Warn().
								Err(err).
								Str("path", drivePath).
								Msg("Failed to parse drive details")
							continue
						}
						
						// Drive tags
						driveTags := append([]tags.Tag{}, controllerTags...)
						driveTags = append(driveTags,
							tags.Tag{Key: "drive_id", Value: driveInfo.ID},
							tags.Tag{Key: "drive_name", Value: driveInfo.Name},
						)
						
						if driveInfo.Model != "" {
							driveTags = append(driveTags, tags.Tag{Key: "model", Value: driveInfo.Model})
						}
						if driveInfo.Manufacturer != "" {
							driveTags = append(driveTags, tags.Tag{Key: "manufacturer", Value: driveInfo.Manufacturer})
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
						
						// Add physical location if available
						if driveInfo.PhysicalLocation.PartLocation.ServiceLabel != "" {
							driveTags = append(driveTags, tags.Tag{Key: "service_label", Value: driveInfo.PhysicalLocation.PartLocation.ServiceLabel})
						}
						
						// Add host tag if available
						if hostName != "" {
							driveTags = append(driveTags, tags.Tag{Key: "host", Value: hostName})
						}

						// Add controller letter tag if available
						if controllerLetter != "" {
							driveTags = append(driveTags, tags.Tag{Key: "controller", Value: controllerLetter})
						}

						// Drive health state
						if driveInfo.Status != nil && driveInfo.Status.Health != "" {
							datapoints = append(datapoints, data_store.DataPoint{
								Name:      "hardware.storage.drive.health",
								Timestamp: timestamp,
								Value:     float32(mapHealthState(driveInfo.Status.Health)),
								Tags:      driveTags,
							})
						}

						// Drive capacity
						if driveInfo.CapacityBytes > 0 {
							datapoints = append(datapoints, data_store.DataPoint{
								Name:      "hardware.storage.drive.size",
								Timestamp: timestamp,
								Value:     float32(driveInfo.CapacityBytes),
								Tags:      driveTags,
							})

							// Also add capacity in GB for easier consumption
							gbValue := float32(driveInfo.CapacityBytes) / (1024 * 1024 * 1024)
							datapoints = append(datapoints, data_store.DataPoint{
								Name:      "hardware.storage.drive.size_gb",
								Timestamp: timestamp,
								Value:     gbValue,
								Tags:      driveTags,
							})
						}

						// Drive block size
						if driveInfo.BlockSizeBytes > 0 {
							datapoints = append(datapoints, data_store.DataPoint{
								Name:      "hardware.storage.drive.block_size",
								Timestamp: timestamp,
								Value:     float32(driveInfo.BlockSizeBytes),
								Tags:      driveTags,
							})
						}

						// Drive rotation speed
						if driveInfo.RotationSpeedRPM > 0 {
							datapoints = append(datapoints, data_store.DataPoint{
								Name:      "hardware.storage.drive.rotation_speed",
								Timestamp: timestamp,
								Value:     float32(driveInfo.RotationSpeedRPM),
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
							Timestamp: timestamp,
							Value:     float32(failurePredicted),
							Tags:      driveTags,
						})
					}
				}
			}
			
			// Process volumes for this controller
			volumesPath := controllerPath + "/Volumes"
			volumesResp, err := c.client.Get(ctx, volumesPath)
			if err == nil {
				// Process each volume
				for _, member := range volumesResp.Members {
					if volumePath, ok := member["@odata.id"]; ok {
						volResp, err := c.client.Get(ctx, volumePath)
						if err != nil {
							c.logger.Warn().
								Err(err).
								Str("path", volumePath).
								Msg("Failed to get volume details")
							continue
						}
						
						// Extract volume information
						var volumeInfo struct {
							ID            string  `json:"Id"`
							Name          string  `json:"Name"`
							CapacityBytes int64   `json:"CapacityBytes"`
							RAIDType      string  `json:"RAIDType"`
							Status        *Status `json:"Status"`
							VolumeType    string  `json:"VolumeType"`
							Encrypted     bool    `json:"Encrypted"`
							OptimumIOSizeBytes int64 `json:"OptimumIOSizeBytes"`
						}
						
						if err := json.Unmarshal(volResp.Raw, &volumeInfo); err != nil {
							c.logger.Warn().
								Err(err).
								Str("path", volumePath).
								Msg("Failed to parse volume details")
							continue
						}
						
						// Volume tags
						volumeTags := append([]tags.Tag{}, controllerTags...)
						volumeTags = append(volumeTags,
							tags.Tag{Key: "volume_id", Value: volumeInfo.ID},
							tags.Tag{Key: "volume_name", Value: volumeInfo.Name},
						)

						if volumeInfo.RAIDType != "" {
							volumeTags = append(volumeTags, tags.Tag{Key: "raid_type", Value: volumeInfo.RAIDType})
						}
						if volumeInfo.VolumeType != "" {
							volumeTags = append(volumeTags, tags.Tag{Key: "volume_type", Value: volumeInfo.VolumeType})
						}

						// Add host tag if available
						if hostName != "" {
							volumeTags = append(volumeTags, tags.Tag{Key: "host", Value: hostName})
						}

						// Add controller letter tag if available
						if controllerLetter != "" {
							volumeTags = append(volumeTags, tags.Tag{Key: "controller", Value: controllerLetter})
						}

						// Volume encryption state
						encrypted := 0.0
						if volumeInfo.Encrypted {
							encrypted = 1.0
						}

						datapoints = append(datapoints, data_store.DataPoint{
							Name:      "hardware.storage.volume.encrypted",
							Timestamp: timestamp,
							Value:     float32(encrypted),
							Tags:      volumeTags,
						})

						// Volume health state
						if volumeInfo.Status != nil && volumeInfo.Status.Health != "" {
							datapoints = append(datapoints, data_store.DataPoint{
								Name:      "hardware.storage.volume.health",
								Timestamp: timestamp,
								Value:     float32(mapHealthState(volumeInfo.Status.Health)),
								Tags:      volumeTags,
							})
						}

						// Volume capacity
						if volumeInfo.CapacityBytes > 0 {
							datapoints = append(datapoints, data_store.DataPoint{
								Name:      "hardware.storage.volume.size",
								Timestamp: timestamp,
								Value:     float32(volumeInfo.CapacityBytes),
								Tags:      volumeTags,
							})

							// Also add capacity in GB for easier consumption
							gbValue := float32(volumeInfo.CapacityBytes) / (1024 * 1024 * 1024)
							datapoints = append(datapoints, data_store.DataPoint{
								Name:      "hardware.storage.volume.size_gb",
								Timestamp: timestamp,
								Value:     gbValue,
								Tags:      volumeTags,
							})
						}

						// Volume optimal IO size
						if volumeInfo.OptimumIOSizeBytes > 0 {
							datapoints = append(datapoints, data_store.DataPoint{
								Name:      "hardware.storage.volume.optimum_io_size",
								Timestamp: timestamp,
								Value:     float32(volumeInfo.OptimumIOSizeBytes),
								Tags:      volumeTags,
							})
						}
					}
				}
			}
		}
	} else {
		// Fall back to standard server storage collection if available
		return c.GenericCollector.collectStorageMetrics(ctx, timestamp)
	}
	
	return datapoints, nil
}