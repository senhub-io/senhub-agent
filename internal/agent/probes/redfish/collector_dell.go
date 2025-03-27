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

// DellCollector implements Dell-specific Redfish collection
type DellCollector struct {
	*GenericCollector
	// Dell specific fields
	idracVersion string
	lifecycleVersion string
}

// NewDellCollector creates a new collector for Dell servers
func NewDellCollector(endpoint, username, password string, logger *logger.Logger) (RedfishCollector, error) {
	// First create a generic collector as the base
	genericCollector, err := NewGenericCollector(endpoint, username, password, logger)
	if err != nil {
		return nil, err
	}

	// Cast to use the embedded fields and methods
	gc, ok := genericCollector.(*GenericCollector)
	if !ok {
		return nil, fmt.Errorf("unexpected collector type, cannot create Dell collector")
	}

	// Create the Dell collector with the generic one embedded
	collector := &DellCollector{
		GenericCollector: gc,
	}

	// Override the vendor type
	collector.vendorType = VendorDell

	return collector, nil
}

// Connect implements Dell-specific connection logic
func (c *DellCollector) Connect(ctx context.Context) error {
	// First use the generic connect method
	if err := c.GenericCollector.Connect(ctx); err != nil {
		return err
	}

	// Get Dell-specific information
	if err := c.getDellInfo(ctx); err != nil {
		c.logger.Warn().Err(err).Msg("Failed to get Dell-specific info")
		// Continue anyway, as this is not critical
	}

	return nil
}

// getDellInfo retrieves Dell-specific information like iDRAC version
func (c *DellCollector) getDellInfo(ctx context.Context) error {
	// Try to get Dell-specific manager (iDRAC) info
	resp, err := c.client.Get(ctx, "Managers/iDRAC.Embedded.1")
	if err != nil {
		return fmt.Errorf("failed to get iDRAC info: %v", err)
	}

	// Extract iDRAC version
	c.idracVersion = resp.FirmwareVersion

	// Try to get Lifecycle Controller version
	lcFound := false
	if resp.Oem != nil {
		// Dell puts Lifecycle Controller info in OEM section
		dellData, hasDell := resp.Oem["Dell"]
		if hasDell {
			dellOem, ok := dellData.(map[string]interface{})
			if ok {
				if lcv, has := dellOem["LifecycleControllerVersion"]; has {
					c.lifecycleVersion = fmt.Sprintf("%v", lcv)
					lcFound = true
				}
			}
		}
	}

	c.logger.Debug().
		Str("idrac_version", c.idracVersion).
		Bool("lifecycle_controller_found", lcFound).
		Str("lifecycle_version", c.lifecycleVersion).
		Msg("Retrieved Dell-specific information")

	return nil
}

// GetSupportedCollections returns Dell-specific collection capabilities
func (c *DellCollector) GetSupportedCollections() []CollectionType {
	// Dell supports all generic collections plus some extras
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
func (c *DellCollector) IsSupported(collectionType CollectionType) bool {
	switch collectionType {
	case CollectionSystem, CollectionThermal, CollectionPower,
		CollectionProcessor, CollectionMemory, CollectionStorage,
		CollectionDrives, CollectionNetworkAdapter:
		return true
	default:
		return false
	}
}

// CollectMetrics gathers Dell-specific metrics
func (c *DellCollector) CollectMetrics(ctx context.Context, collectionType CollectionType, timestamp time.Time) ([]data_store.DataPoint, error) {
	// For most collection types, use the generic collector
	switch collectionType {
	case CollectionSystem:
		return c.collectSystemMetrics(ctx, timestamp)
	case CollectionStorage, CollectionDrives:
		return c.collectStorageMetrics(ctx, timestamp)
	case CollectionNetworkAdapter:
		return c.collectNetworkMetrics(ctx, timestamp)
	default:
		// For other types, delegate to the generic collector
		return c.GenericCollector.CollectMetrics(ctx, collectionType, timestamp)
	}
}

// collectSystemMetrics gathers Dell-specific system metrics
func (c *DellCollector) collectSystemMetrics(ctx context.Context, timestamp time.Time) ([]data_store.DataPoint, error) {
	// Get the generic system metrics first
	datapoints, err := c.GenericCollector.collectSystemMetrics(ctx, timestamp)
	if err != nil {
		return nil, err
	}

	// Add Dell-specific system datapoints
	for _, systemPath := range c.systems {
		// Get Dell OEM data for this system
		resp, err := c.client.Get(ctx, systemPath)
		if err != nil {
			c.logger.Warn().
				Err(err).
				Str("path", systemPath).
				Msg("Failed to get Dell system details")
			continue
		}

		// Extract system tags
		systemTags := []tags.Tag{
			{Key: "system_id", Value: resp.ID},
			{Key: "system_name", Value: resp.Name},
			{Key: "vendor", Value: string(c.vendorType)},
			{Key: "idrac_version", Value: c.idracVersion},
		}

		// Add Dell-specific OEM data if available
		if resp.Oem != nil {
			dellData, hasDell := resp.Oem["Dell"]
			if hasDell {
				dellOem, ok := dellData.(map[string]interface{})
				if ok {
					// Add Dell service tag if available
					if serviceTag, has := dellOem["ServiceTag"]; has {
						serviceTagStr := fmt.Sprintf("%v", serviceTag)
						// Add service tag as a tag
						systemTags = append(systemTags, tags.Tag{Key: "service_tag", Value: serviceTagStr})
					}
				}
			}
		}

		// Add iDRAC version datapoint if available
		if c.idracVersion != "" {
			// We don't have a numeric value for version, so we use 1.0 as a sentinel value
			// The actual version is in the tags
			datapoints = append(datapoints, data_store.DataPoint{
				Name:      "system.idrac_firmware",
				Timestamp: timestamp,
				Value:     1.0, // Sentinel value
				Tags:      systemTags,
			})
		}

		// Add Lifecycle Controller version datapoint if available
		if c.lifecycleVersion != "" {
			systemTags = append(systemTags, tags.Tag{Key: "lifecycle_version", Value: c.lifecycleVersion})
			datapoints = append(datapoints, data_store.DataPoint{
				Name:      "system.lifecycle_controller",
				Timestamp: timestamp,
				Value:     1.0, // Sentinel value
				Tags:      systemTags,
			})
		}
	}

	return datapoints, nil
}

// collectStorageMetrics gathers Dell-specific storage metrics
func (c *DellCollector) collectStorageMetrics(ctx context.Context, timestamp time.Time) ([]data_store.DataPoint, error) {
	if len(c.systems) == 0 {
		return nil, fmt.Errorf("no systems found")
	}

	var datapoints []data_store.DataPoint

	// Collect metrics for each system
	for _, systemPath := range c.systems {
		// Get storage collection
		storagePath := systemPath + "/Storage"
		storageResp, err := c.client.Get(ctx, storagePath)
		if err != nil {
			c.logger.Warn().
				Err(err).
				Str("path", storagePath).
				Msg("Failed to get storage collection, skipping")
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

		// Process each storage controller
		for _, member := range storageResp.Members {
			controllerPath, ok := member["@odata.id"]
			if !ok {
				continue
			}

			// Get controller details
			controllerResp, err := c.client.Get(ctx, controllerPath)
			if err != nil {
				c.logger.Warn().
					Err(err).
					Str("path", controllerPath).
					Msg("Failed to get storage controller details, skipping")
				continue
			}

			// Create controller-specific tags
			controllerTags := append([]tags.Tag{}, systemTags...)
			controllerTags = append(controllerTags, tags.Tag{Key: "controller_id", Value: controllerResp.ID})
			controllerTags = append(controllerTags, tags.Tag{Key: "controller_name", Value: controllerResp.Name})

			if controllerResp.Model != "" {
				controllerTags = append(controllerTags, tags.Tag{Key: "model", Value: controllerResp.Model})
			}
			if controllerResp.Manufacturer != "" {
				controllerTags = append(controllerTags, tags.Tag{Key: "manufacturer", Value: controllerResp.Manufacturer})
			}

			// Add controller health state if available
			if controllerResp.Status != nil {
				health := mapHealthState(controllerResp.Status.Health)
				datapoints = append(datapoints, data_store.DataPoint{
					Name:      "storage.controller.health",
					Timestamp: timestamp,
					Value:     float32(health),
					Tags:      controllerTags,
				})
			}

			// Process drives
			drivesPath := controllerPath + "/Drives"
			drivesResp, err := c.client.Get(ctx, drivesPath)
			if err != nil {
				c.logger.Warn().
					Err(err).
					Str("path", drivesPath).
					Msg("Failed to get drives collection, skipping")
				continue
			}

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

				if driveResp.Model != "" {
					driveTags = append(driveTags, tags.Tag{Key: "model", Value: driveResp.Model})
				}
				if driveResp.Manufacturer != "" {
					driveTags = append(driveTags, tags.Tag{Key: "manufacturer", Value: driveResp.Manufacturer})
				}
				if driveResp.SerialNumber != "" {
					driveTags = append(driveTags, tags.Tag{Key: "serial_number", Value: driveResp.SerialNumber})
				}

				// Extract drive metrics
				var driveData struct {
					CapacityBytes    int64    `json:"CapacityBytes"`
					MediaType        string   `json:"MediaType"`
					Protocol         string   `json:"Protocol"`
					RotationSpeedRPM int      `json:"RotationSpeedRPM"`
					PredictedMediaLifeLeftPercent float32 `json:"PredictedMediaLifeLeftPercent"`
					Status           *Status  `json:"Status"`
				}
				rawJSON, _ := json.Marshal(driveResp)
				if err := json.Unmarshal(rawJSON, &driveData); err != nil {
					c.logger.Warn().
						Err(err).
						Msg("Failed to parse drive data")
					continue
				}

				// Add media type and protocol to tags
				if driveData.MediaType != "" {
					driveTags = append(driveTags, tags.Tag{Key: "media_type", Value: driveData.MediaType})
				}
				if driveData.Protocol != "" {
					driveTags = append(driveTags, tags.Tag{Key: "protocol", Value: driveData.Protocol})
				}

				// Add drive capacity
				if driveData.CapacityBytes > 0 {
					capacityGB := float32(driveData.CapacityBytes) / (1024 * 1024 * 1024)
					datapoints = append(datapoints, data_store.DataPoint{
						Name:      "storage.drive.capacity_gb",
						Timestamp: timestamp,
						Value:     capacityGB,
						Tags:      driveTags,
					})
				}

				// Add rotation speed for HDDs
				if driveData.RotationSpeedRPM > 0 {
					datapoints = append(datapoints, data_store.DataPoint{
						Name:      "storage.drive.rotation_rpm",
						Timestamp: timestamp,
						Value:     float32(driveData.RotationSpeedRPM),
						Tags:      driveTags,
					})
				}

				// Add media life left for SSDs
				if driveData.PredictedMediaLifeLeftPercent > 0 {
					datapoints = append(datapoints, data_store.DataPoint{
						Name:      "storage.drive.media_life_percent",
						Timestamp: timestamp,
						Value:     driveData.PredictedMediaLifeLeftPercent,
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

			// Get volumes (logical drives)
			volumesPath := controllerPath + "/Volumes"
			volumesResp, err := c.client.Get(ctx, volumesPath)
			if err != nil {
				c.logger.Warn().
					Err(err).
					Str("path", volumesPath).
					Msg("Failed to get volumes collection, skipping")
				continue
			}

			for _, volumeMember := range volumesResp.Members {
				volumePath, ok := volumeMember["@odata.id"]
				if !ok {
					continue
				}

				// Get volume details
				volumeResp, err := c.client.Get(ctx, volumePath)
				if err != nil {
					c.logger.Warn().
						Err(err).
						Str("path", volumePath).
						Msg("Failed to get volume details, skipping")
					continue
				}

				// Create volume-specific tags
				volumeTags := append([]tags.Tag{}, controllerTags...)
				volumeTags = append(volumeTags, tags.Tag{Key: "volume_id", Value: volumeResp.ID})
				volumeTags = append(volumeTags, tags.Tag{Key: "volume_name", Value: volumeResp.Name})

				// Extract volume metrics
				var volumeData struct {
					CapacityBytes int64  `json:"CapacityBytes"`
					RAIDType      string `json:"RAIDType"`
					Status        *Status `json:"Status"`
				}
				rawJSON, _ := json.Marshal(volumeResp)
				if err := json.Unmarshal(rawJSON, &volumeData); err != nil {
					c.logger.Warn().
						Err(err).
						Msg("Failed to parse volume data")
					continue
				}

				// Add RAID type to tags
				if volumeData.RAIDType != "" {
					volumeTags = append(volumeTags, tags.Tag{Key: "raid_type", Value: volumeData.RAIDType})
				}

				// Add volume capacity
				if volumeData.CapacityBytes > 0 {
					capacityGB := float32(volumeData.CapacityBytes) / (1024 * 1024 * 1024)
					datapoints = append(datapoints, data_store.DataPoint{
						Name:      "storage.volume.capacity_gb",
						Timestamp: timestamp,
						Value:     capacityGB,
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

// collectNetworkMetrics gathers Dell-specific network metrics
func (c *DellCollector) collectNetworkMetrics(ctx context.Context, timestamp time.Time) ([]data_store.DataPoint, error) {
	if len(c.systems) == 0 {
		return nil, fmt.Errorf("no systems found")
	}

	var datapoints []data_store.DataPoint

	// Collect metrics for each system
	for _, systemPath := range c.systems {
		// Get network adapters collection
		netPath := systemPath + "/NetworkAdapters"
		netResp, err := c.client.Get(ctx, netPath)
		if err != nil {
			c.logger.Warn().
				Err(err).
				Str("path", netPath).
				Msg("Failed to get network adapters collection, skipping")
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

		// Process each network adapter
		for _, member := range netResp.Members {
			adapterPath, ok := member["@odata.id"]
			if !ok {
				continue
			}

			// Get adapter details
			adapterResp, err := c.client.Get(ctx, adapterPath)
			if err != nil {
				c.logger.Warn().
					Err(err).
					Str("path", adapterPath).
					Msg("Failed to get network adapter details, skipping")
				continue
			}

			// Create adapter-specific tags
			adapterTags := append([]tags.Tag{}, systemTags...)
			adapterTags = append(adapterTags, tags.Tag{Key: "adapter_id", Value: adapterResp.ID})
			adapterTags = append(adapterTags, tags.Tag{Key: "adapter_name", Value: adapterResp.Name})

			if adapterResp.Model != "" {
				adapterTags = append(adapterTags, tags.Tag{Key: "model", Value: adapterResp.Model})
			}
			if adapterResp.Manufacturer != "" {
				adapterTags = append(adapterTags, tags.Tag{Key: "manufacturer", Value: adapterResp.Manufacturer})
			}

			// Add adapter health state if available
			if adapterResp.Status != nil {
				health := mapHealthState(adapterResp.Status.Health)
				datapoints = append(datapoints, data_store.DataPoint{
					Name:      "network.adapter.health",
					Timestamp: timestamp,
					Value:     float32(health),
					Tags:      adapterTags,
				})
			}

			// Get network ports
			portsPath := adapterPath + "/NetworkPorts"
			portsResp, err := c.client.Get(ctx, portsPath)
			if err != nil {
				c.logger.Warn().
					Err(err).
					Str("path", portsPath).
					Msg("Failed to get network ports collection, skipping")
				continue
			}

			for _, portMember := range portsResp.Members {
				portPath, ok := portMember["@odata.id"]
				if !ok {
					continue
				}

				// Get port details
				portResp, err := c.client.Get(ctx, portPath)
				if err != nil {
					c.logger.Warn().
						Err(err).
						Str("path", portPath).
						Msg("Failed to get network port details, skipping")
					continue
				}

				// Create port-specific tags
				portTags := append([]tags.Tag{}, adapterTags...)
				portTags = append(portTags, tags.Tag{Key: "port_id", Value: portResp.ID})
				portTags = append(portTags, tags.Tag{Key: "port_name", Value: portResp.Name})

				// Extract port metrics
				var portData struct {
					LinkStatus       string   `json:"LinkStatus"`
					CurrentLinkSpeed string   `json:"CurrentLinkSpeed"`
					Status           *Status  `json:"Status"`
				}
				rawJSON, _ := json.Marshal(portResp)
				if err := json.Unmarshal(rawJSON, &portData); err != nil {
					c.logger.Warn().
						Err(err).
						Msg("Failed to parse network port data")
					continue
				}

				// Add link status to tags
				if portData.LinkStatus != "" {
					portTags = append(portTags, tags.Tag{Key: "link_status", Value: portData.LinkStatus})
				}

				// Add link speed numeric value
				if portData.CurrentLinkSpeed != "" {
					// Parse the link speed (example: "10 Gbps" -> 10)
					var speedValue float32
					var speedUnit string
					if _, err := fmt.Sscanf(portData.CurrentLinkSpeed, "%f %s", &speedValue, &speedUnit); err == nil {
						// Convert to Gbps if needed
						if strings.Contains(strings.ToLower(speedUnit), "mb") {
							speedValue /= 1000.0
						}

						datapoints = append(datapoints, data_store.DataPoint{
							Name:      "network.port.speed_gbps",
							Timestamp: timestamp,
							Value:     speedValue,
							Tags:      portTags,
						})
					}
				}

				// Add link status numeric value
				linkUp := 0.0
				if strings.ToLower(portData.LinkStatus) == "up" {
					linkUp = 1.0
				}
				datapoints = append(datapoints, data_store.DataPoint{
					Name:      "network.port.link_up",
					Timestamp: timestamp,
					Value:     float32(linkUp),
					Tags:      portTags,
				})

				// Add health state
				if portData.Status != nil {
					health := mapHealthState(portData.Status.Health)
					datapoints = append(datapoints, data_store.DataPoint{
						Name:      "network.port.health",
						Timestamp: timestamp,
						Value:     float32(health),
						Tags:      portTags,
					})
				}
			}
		}
	}

	return datapoints, nil
}