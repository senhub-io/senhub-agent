// Package redfish provides monitoring capabilities for hardware systems via Redfish API
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

			// Controller tags - suivant les conventions de REDFISH-TAGS.md
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
						
						// Drive tags - suivant les conventions de REDFISH-TAGS.md
						driveTags := append([]tags.Tag{}, controllerTags...)
						driveTags = append(driveTags,
							tags.Tag{Key: "drive_id", Value: driveInfo.ID},
							tags.Tag{Key: "drive_name", Value: driveInfo.Name},
						)

						if driveInfo.Model != "" {
							driveTags = append(driveTags, tags.Tag{Key: "model", Value: driveInfo.Model})
						}
						if driveInfo.Manufacturer != "" {
							driveTags = append(driveTags, tags.Tag{Key: "drive_manufacturer", Value: driveInfo.Manufacturer})
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

						// Extract additional properties from raw data
						var driveRawData map[string]interface{}
						if err := json.Unmarshal(driveResp.Raw, &driveRawData); err == nil {
							// Add hotspare type if available
							if hotspareValue, ok := driveRawData["HotspareType"].(string); ok && hotspareValue != "" {
								driveTags = append(driveTags, tags.Tag{Key: "hotspare_type", Value: hotspareValue})
							}
							
							// Add encryption ability if available
							if encAbilityValue, ok := driveRawData["EncryptionAbility"].(string); ok && encAbilityValue != "" {
								driveTags = append(driveTags, tags.Tag{Key: "encryption_ability", Value: encAbilityValue})
							}
							
							// Add encryption status if available
							if encStatusValue, ok := driveRawData["EncryptionStatus"].(string); ok && encStatusValue != "" {
								driveTags = append(driveTags, tags.Tag{Key: "encryption_status", Value: encStatusValue})
							}
						}

						// Add service label if available - suivant les conventions de REDFISH-TAGS.md
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

						// Drive health state - la valeur est dérivée de Status.Health
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
							ID                string  `json:"Id"`
							Name              string  `json:"Name"`
							CapacityBytes     int64   `json:"CapacityBytes"`
							RAIDType          string  `json:"RAIDType"`
							Status            *Status `json:"Status"`
							VolumeType        string  `json:"VolumeType"`
							Encrypted         bool    `json:"Encrypted"`
							OptimumIOSizeBytes int64   `json:"OptimumIOSizeBytes"`
							EncryptionType    string  `json:"EncryptionType"`
							StripSizeBytes    int64   `json:"StripSizeBytes"`
							// AccessCapabilities peut être une chaîne ou un tableau selon les implémentations
							// Nous allons l'extraire manuellement du JSON brut
						}
						
						if err := json.Unmarshal(volResp.Raw, &volumeInfo); err != nil {
							c.logger.Warn().
								Err(err).
								Str("path", volumePath).
								Msg("Failed to parse volume details")
							continue
						}
						
						// Volume tags - suivant les conventions de REDFISH-TAGS.md
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
						if volumeInfo.EncryptionType != "" {
							volumeTags = append(volumeTags, tags.Tag{Key: "encryption_type", Value: volumeInfo.EncryptionType})
						}
						if volumeInfo.StripSizeBytes > 0 {
							volumeTags = append(volumeTags, tags.Tag{Key: "stripe_size", Value: fmt.Sprintf("%d", volumeInfo.StripSizeBytes)})
						}
						// Extraction d'AccessCapabilities qui peut être une chaîne ou un tableau
						var accessCapabilities string
						var volumeRawData map[string]interface{}
						if err := json.Unmarshal(volResp.Raw, &volumeRawData); err == nil {
							if accessCap, ok := volumeRawData["AccessCapabilities"]; ok {
								// Essayons d'abord de le traiter comme une chaîne
								if strValue, ok := accessCap.(string); ok && strValue != "" {
									accessCapabilities = strValue
								} else if arrValue, ok := accessCap.([]interface{}); ok && len(arrValue) > 0 {
									// Si c'est un tableau, concaténons les valeurs avec des virgules
									var values []string
									for _, val := range arrValue {
										if strVal, ok := val.(string); ok && strVal != "" {
											values = append(values, strVal)
										}
									}
									accessCapabilities = strings.Join(values, ", ")
								}
							}
						}

						if accessCapabilities != "" {
							volumeTags = append(volumeTags, tags.Tag{Key: "access_capabilities", Value: accessCapabilities})
						}

						// Add host tag if available
						if hostName != "" {
							volumeTags = append(volumeTags, tags.Tag{Key: "host", Value: hostName})
						}

						// Add controller letter tag if available
						if controllerLetter != "" {
							volumeTags = append(volumeTags, tags.Tag{Key: "controller", Value: controllerLetter})
						}

						// Volume encryption state - la valeur est dérivée de Encrypted
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

						// Volume health state - la valeur est dérivée de Status.Health
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
							
							// Attempt to get usage information from OEM data if available
							var volumeRawData map[string]interface{}
							if err := json.Unmarshal(volResp.Raw, &volumeRawData); err == nil {
								// Try to extract used and available space from various possible paths
								// First check standard Redfish properties
								if capacitySourcesRaw, ok := volumeRawData["CapacitySources"]; ok {
									if capacitySources, ok := capacitySourcesRaw.([]interface{}); ok && len(capacitySources) > 0 {
										for _, sourceRaw := range capacitySources {
											if source, ok := sourceRaw.(map[string]interface{}); ok {
												// Look for usage data in the CapacitySource
												if providedCapacityRaw, ok := source["ProvidedCapacityBytes"]; ok {
													if providedCapacity, ok := providedCapacityRaw.(float64); ok && providedCapacity > 0 {
														datapoints = append(datapoints, data_store.DataPoint{
															Name:      "hardware.storage.volume.space.used",
															Timestamp: timestamp,
															Value:     float32(providedCapacity),
															Tags:      volumeTags,
														})
														
														// Calculate used space percentage
														usedSpacePercent := (float32(providedCapacity) / float32(volumeInfo.CapacityBytes)) * 100
														datapoints = append(datapoints, data_store.DataPoint{
															Name:      "hardware.storage.volume.space.utilization",
															Timestamp: timestamp,
															Value:     usedSpacePercent,
															Tags:      volumeTags,
														})
														
														// Calculate free space
														freeSpace := float32(volumeInfo.CapacityBytes) - float32(providedCapacity)
														datapoints = append(datapoints, data_store.DataPoint{
															Name:      "hardware.storage.volume.space.free",
															Timestamp: timestamp,
															Value:     freeSpace,
															Tags:      volumeTags,
														})
													}
												}
											}
										}
									}
								}
								
								// If not found, try OEM-specific paths
								if oemData, ok := volumeRawData["Oem"]; ok {
									if oemMap, ok := oemData.(map[string]interface{}); ok {
										// Check for Dell-specific paths
										if dellData, ok := oemMap["Dell"]; ok {
											if dellMap, ok := dellData.(map[string]interface{}); ok {
												// Common Dell storage metrics names
												if usedBytesRaw, ok := dellMap["UsedBytes"]; ok {
													if usedBytes, ok := usedBytesRaw.(float64); ok && usedBytes > 0 {
														datapoints = append(datapoints, data_store.DataPoint{
															Name:      "hardware.storage.volume.space.used",
															Timestamp: timestamp,
															Value:     float32(usedBytes),
															Tags:      volumeTags,
														})
														
														// Add used GB
														usedGB := float32(usedBytes) / (1024 * 1024 * 1024)
														volumeTagsWithUnit := append(volumeTags, tags.Tag{Key: "unit", Value: "GB"})
														datapoints = append(datapoints, data_store.DataPoint{
															Name:      "hardware.storage.volume.space.used",
															Timestamp: timestamp,
															Value:     usedGB,
															Tags:      volumeTagsWithUnit,
														})
														
														// Calculate free space
														freeSpace := float32(volumeInfo.CapacityBytes) - float32(usedBytes)
														datapoints = append(datapoints, data_store.DataPoint{
															Name:      "hardware.storage.volume.space.free",
															Timestamp: timestamp,
															Value:     freeSpace,
															Tags:      volumeTags,
														})
														
														// Add free GB 
														freeGB := float32(freeSpace) / (1024 * 1024 * 1024)
														datapoints = append(datapoints, data_store.DataPoint{
															Name:      "hardware.storage.volume.space.free",
															Timestamp: timestamp,
															Value:     freeGB,
															Tags:      volumeTagsWithUnit,
														})
														
														// Calculate used space percentage
														usedSpacePercent := (float32(usedBytes) / float32(volumeInfo.CapacityBytes)) * 100
														volumeTagsWithPercentUnit := append(volumeTags, tags.Tag{Key: "unit", Value: "percent"})
														datapoints = append(datapoints, data_store.DataPoint{
															Name:      "hardware.storage.volume.space.utilization",
															Timestamp: timestamp,
															Value:     usedSpacePercent,
															Tags:      volumeTagsWithPercentUnit,
														})
													}
												}
												
												// Try common RemainingCapacity percent field
												if remainingCapacityRaw, ok := dellMap["RemainingCapacityPercent"]; ok {
													if remainingPercent, ok := remainingCapacityRaw.(float64); ok {
														usedPercent := 100.0 - float32(remainingPercent)
														volumeTagsWithPercentUnit := append(volumeTags, tags.Tag{Key: "unit", Value: "percent"})
														datapoints = append(datapoints, data_store.DataPoint{
															Name:      "hardware.storage.volume.space.utilization",
															Timestamp: timestamp,
															Value:     usedPercent,
															Tags:      volumeTagsWithPercentUnit,
														})
														
														// Calculate and add used and free space based on percentage
														usedBytes := float32(volumeInfo.CapacityBytes) * (usedPercent / 100.0)
														freeBytes := float32(volumeInfo.CapacityBytes) - usedBytes
														
														datapoints = append(datapoints, data_store.DataPoint{
															Name:      "hardware.storage.volume.space.used",
															Timestamp: timestamp,
															Value:     usedBytes,
															Tags:      volumeTags,
														})
														
														datapoints = append(datapoints, data_store.DataPoint{
															Name:      "hardware.storage.volume.space.free",
															Timestamp: timestamp,
															Value:     freeBytes,
															Tags:      volumeTags,
														})
														
														// Add GB versions
														usedGB := usedBytes / (1024 * 1024 * 1024)
														freeGB := freeBytes / (1024 * 1024 * 1024)
														volumeTagsWithUnit := append(volumeTags, tags.Tag{Key: "unit", Value: "GB"})
														
														datapoints = append(datapoints, data_store.DataPoint{
															Name:      "hardware.storage.volume.space.used",
															Timestamp: timestamp,
															Value:     usedGB,
															Tags:      volumeTagsWithUnit,
														})
														
														datapoints = append(datapoints, data_store.DataPoint{
															Name:      "hardware.storage.volume.space.free",
															Timestamp: timestamp,
															Value:     freeGB,
															Tags:      volumeTagsWithUnit,
														})
													}
												}
											}
										}
										
										// Check for HPE-specific paths
										if hpeData, ok := oemMap["Hpe"]; ok {
											if hpeMap, ok := hpeData.(map[string]interface{}); ok {
												// Common HPE storage metrics names
												if spaceInfoRaw, ok := hpeMap["VolumeSpaceInfo"]; ok {
													if spaceInfo, ok := spaceInfoRaw.(map[string]interface{}); ok {
														// Check for used capacity fields
														if usedRaw, ok := spaceInfo["UsedSpace"]; ok {
															if usedBytes, ok := usedRaw.(float64); ok && usedBytes > 0 {
																datapoints = append(datapoints, data_store.DataPoint{
																	Name:      "hardware.storage.volume.space.used",
																	Timestamp: timestamp,
																	Value:     float32(usedBytes),
																	Tags:      volumeTags,
																})
																
																// Calculate free space and percentage
																freeSpace := float32(volumeInfo.CapacityBytes) - float32(usedBytes)
																usedPercent := (float32(usedBytes) / float32(volumeInfo.CapacityBytes)) * 100
																
																datapoints = append(datapoints, data_store.DataPoint{
																	Name:      "hardware.storage.volume.space.free",
																	Timestamp: timestamp,
																	Value:     freeSpace,
																	Tags:      volumeTags,
																})
																
																volumeTagsWithPercentUnit := append(volumeTags, tags.Tag{Key: "unit", Value: "percent"})
																datapoints = append(datapoints, data_store.DataPoint{
																	Name:      "hardware.storage.volume.space.utilization",
																	Timestamp: timestamp,
																	Value:     usedPercent,
																	Tags:      volumeTagsWithPercentUnit,
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
						}

						// Volume optimal IO size - la valeur est dérivée de OptimumIOSizeBytes
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