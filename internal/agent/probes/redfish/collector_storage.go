// Package redfish provides monitoring capabilities for hardware systems via Redfish API
package redfish

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"senhub-agent.go/internal/agent/services/data_store"
	"senhub-agent.go/internal/agent/services/logger"
	"senhub-agent.go/internal/agent/tags"
	"strconv"
	"strings"
	"time"
)

// StorageCollector is a specialized implementation for storage systems like Dell PowerVault
type StorageCollector struct {
	*GenericCollector
	storageVolumes     []string
	storageControllers []string
	storagePools       []string
	visitedLinks       map[string]bool // To avoid cycles when traversing links
	maxDepth           int             // Maximum depth for traversal
}

// NewStorageCollector creates a new collector for storage devices
func NewStorageCollector(endpoint, username, password string, logger *logger.Logger, verifySSL bool) (RedfishCollector, error) {
	// Create generic collector as base
	genericCollector, err := NewGenericCollector(endpoint, username, password, logger, verifySSL)
	if err != nil {
		return nil, fmt.Errorf("failed to create generic collector: %v", err)
	}

	return &StorageCollector{
		GenericCollector:   genericCollector.(*GenericCollector),
		storageVolumes:     make([]string, 0),
		storageControllers: make([]string, 0),
		storagePools:       make([]string, 0),
		visitedLinks:       make(map[string]bool),
		maxDepth:           5, // Default depth for traversal
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
				
				// Attempt to get storage pools for each controller
				poolsPath := normalizedPath + "/StoragePools"
				poolsResp, err := c.client.Get(ctx, poolsPath)
				if err == nil && len(poolsResp.Members) > 0 {
					// We found storage pools, add them to our list
					for _, pool := range poolsResp.Members {
						if poolId, ok := pool["@odata.id"]; ok {
							c.storagePools = append(c.storagePools, strings.TrimPrefix(poolId, "/redfish/v1/"))
						}
					}
				}
			}
		}
		
		c.logger.Debug().
			Int("controller_count", len(c.storageControllers)).
			Int("volume_count", len(c.storageVolumes)).
			Int("pool_count", len(c.storagePools)).
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

// CollectMetrics implementation is in collect_metrics.go

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

						// Drive health state - value is derived from Status.Health
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
								Name:      "hardware.storage.drive.capacity.total",
								Timestamp: timestamp,
								Value:     float32(driveInfo.CapacityBytes),
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
							// AccessCapabilities can be either a string or an array depending on implementations
							// We will extract it manually from the raw JSON
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
						// Extraction of AccessCapabilities which can be either a string or an array
						var accessCapabilities string
						var volumeRawData map[string]interface{}
						if err := json.Unmarshal(volResp.Raw, &volumeRawData); err == nil {
							if accessCap, ok := volumeRawData["AccessCapabilities"]; ok {
								// First try to treat it as a string
								if strValue, ok := accessCap.(string); ok && strValue != "" {
									accessCapabilities = strValue
								} else if arrValue, ok := accessCap.([]interface{}); ok && len(arrValue) > 0 {
									// If it's an array, concatenate values with commas
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

						// Volume encryption state - value is derived from Encrypted
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

						// Volume health state - value is derived from Status.Health
						if volumeInfo.Status != nil && volumeInfo.Status.Health != "" {
							datapoints = append(datapoints, data_store.DataPoint{
								Name:      "hardware.storage.volume.health",
								Timestamp: timestamp,
								Value:     float32(mapHealthState(volumeInfo.Status.Health)),
								Tags:      volumeTags,
							})
						}

						// For volumes, use CapacityBytes directly
						effectiveCapacity := volumeInfo.CapacityBytes
						
						// Volume capacity
						if effectiveCapacity > 0 {
							datapoints = append(datapoints, data_store.DataPoint{
								Name:      "hardware.storage.volume.capacity.total",
								Timestamp: timestamp,
								Value:     float32(effectiveCapacity),
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
															Name:      "hardware.storage.volume.capacity.used",
															Timestamp: timestamp,
															Value:     float32(providedCapacity),
															Tags:      volumeTags,
														})
														
														
														// Calculate free space
														freeSpace := roundToTwoDecimals(float32(effectiveCapacity) - float32(providedCapacity))
														datapoints = append(datapoints, data_store.DataPoint{
															Name:      "hardware.storage.volume.capacity.free",
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
															Name:      "hardware.storage.volume.capacity.used",
															Timestamp: timestamp,
															Value:     float32(usedBytes),
															Tags:      volumeTags,
														})
														
														
														// Calculate free space
														freeSpace := roundToTwoDecimals(float32(effectiveCapacity) - float32(usedBytes))
														datapoints = append(datapoints, data_store.DataPoint{
															Name:      "hardware.storage.volume.capacity.free",
															Timestamp: timestamp,
															Value:     freeSpace,
															Tags:      volumeTags,
														})
														
													}
												}
												
												// Try common RemainingCapacity percent field
												if remainingCapacityRaw, ok := dellMap["RemainingCapacityPercent"]; ok {
													if remainingPercent, ok := remainingCapacityRaw.(float64); ok {
														usedPercent := 100.0 - float32(remainingPercent)
														
														// Calculate and add used and free space based on percentage
														usedBytes := float32(effectiveCapacity) * (usedPercent / 100.0)
														freeBytes := float32(effectiveCapacity) - usedBytes
														
														datapoints = append(datapoints, data_store.DataPoint{
															Name:      "hardware.storage.volume.capacity.used",
															Timestamp: timestamp,
															Value:     usedBytes,
															Tags:      volumeTags,
														})
														
														datapoints = append(datapoints, data_store.DataPoint{
															Name:      "hardware.storage.volume.capacity.free",
															Timestamp: timestamp,
															Value:     freeBytes,
															Tags:      volumeTags,
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
																	Name:      "hardware.storage.volume.capacity.used",
																	Timestamp: timestamp,
																	Value:     float32(usedBytes),
																	Tags:      volumeTags,
																})
																
																// Calculate free space
																freeSpace := roundToTwoDecimals(float32(effectiveCapacity) - float32(usedBytes))
																
																datapoints = append(datapoints, data_store.DataPoint{
																	Name:      "hardware.storage.volume.capacity.free",
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
									}
								}
							}
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

// collectVolumeConsumptionMetrics collects storage consumption and I/O metrics for volumes
func (c *StorageCollector) collectVolumeConsumptionMetrics(ctx context.Context, timestamp time.Time) ([]data_store.DataPoint, error) {
	var datapoints []data_store.DataPoint

	// Get system name to use as the host tag
	var hostName string
	if len(c.systems) > 0 {
		sysResp, err := c.client.Get(ctx, c.systems[0])
		if err == nil && sysResp.Name != "" {
			hostName = sysResp.Name
		}
	}
	if hostName == "" {
		rootResp, err := c.client.Get(ctx, "")
		if err == nil && rootResp.UUID != "" {
			hostName = rootResp.UUID
		}
	}

	// Process volumes for each controller
	for _, controllerPath := range c.storageControllers {
		// Get controller details to extract letter
		ctrlResp, err := c.client.Get(ctx, controllerPath)
		if err != nil {
			c.logger.Warn().Err(err).Str("path", controllerPath).Msg("Failed to get controller details")
			continue
		}

		// Extract controller letter (A/B) for tagging
		controllerID := ctrlResp.ID
		var controllerLetter string
		if strings.Contains(strings.ToLower(controllerID), "_a") {
			controllerLetter = "A"
		} else if strings.Contains(strings.ToLower(controllerID), "_b") {
			controllerLetter = "B"
		}

		// Process each volume
		volumesPath := controllerPath + "/Volumes"
		volumesResp, err := c.client.Get(ctx, volumesPath)
		if err != nil {
			continue
		}

		for _, member := range volumesResp.Members {
			if volumePath, ok := member["@odata.id"]; ok {
				volResp, err := c.client.Get(ctx, volumePath)
				if err != nil {
					continue
				}

				// Extract detailed volume information
				var volumeInfo struct {
					ID                    string  `json:"Id"`
					Name                  string  `json:"Name"`
					CapacityBytes         int64   `json:"CapacityBytes"`
					BlockSizeBytes        int     `json:"BlockSizeBytes"`
					Status                *Status `json:"Status"`
					Encrypted             bool    `json:"Encrypted"`
					RemainingCapacityPercent int  `json:"RemainingCapacityPercent"`
					WriteCachePolicy      string  `json:"WriteCachePolicy"`
					AccessCapabilities    []string `json:"AccessCapabilities"`
					EncryptionTypes       []string `json:"EncryptionTypes"`
					RAIDType              string  `json:"RAIDType"`
					Capacity struct {
						Data struct {
							AllocatedBytes int64 `json:"AllocatedBytes"`
							ConsumedBytes  int64 `json:"ConsumedBytes"`
						} `json:"Data"`
					} `json:"Capacity"`
					// IO Statistics for performance metrics
					IOStatistics struct {
						ReadHitIORequests  int64 `json:"ReadHitIORequests"`
						ReadIOKiBytes      int64 `json:"ReadIOKiBytes"`
						WriteHitIORequests int64 `json:"WriteHitIORequests"`
						WriteIOKiBytes     int64 `json:"WriteIOKiBytes"`
						ReadIORequestTime  string `json:"ReadIORequestTime"`
						WriteIORequestTime string `json:"WriteIORequestTime"`
					} `json:"IOStatistics"`
					// Capacity Sources to track pool association
					CapacitySources []struct {
						Id             string `json:"Id"`
						Name           string `json:"Name"`
						ProvidingPools struct {
							Members []struct {
								OdataID string `json:"@odata.id"`
							}
						} `json:"ProvidingPools"`
					} `json:"CapacitySources"`
				}
				
				if err := json.Unmarshal(volResp.Raw, &volumeInfo); err != nil {
					c.logger.Warn().Err(err).Str("path", volumePath).Msg("Failed to unmarshal volume data")
					continue
				}

				// Extract volume pool from path
				var poolID string
				if len(volumeInfo.CapacitySources) > 0 && len(volumeInfo.CapacitySources[0].ProvidingPools.Members) > 0 {
					// Extract pool ID from @odata.id path which is like "/redfish/v1/Storage/controller_a/StoragePools/A"
					poolPath := volumeInfo.CapacitySources[0].ProvidingPools.Members[0].OdataID
					pathParts := strings.Split(poolPath, "/")
					if len(pathParts) > 0 {
						poolID = pathParts[len(pathParts)-1]
					}
				}

				// Volume tags - comprehensive tags for operational context
				volumeTags := []tags.Tag{
					{Key: "volume_id", Value: volumeInfo.ID},
					{Key: "volume_name", Value: volumeInfo.Name},
					{Key: "controller_id", Value: controllerID},
				}
				
				// Add controller letter tag - critical for operations
				if controllerLetter != "" {
					volumeTags = append(volumeTags, tags.Tag{Key: "controller", Value: controllerLetter})
				}
				
				// Add host tag for instance identification
				if hostName != "" {
					volumeTags = append(volumeTags, tags.Tag{Key: "host", Value: hostName})
				}
				
				// Add pool ID for mapping volumes to pools
				if poolID != "" {
					volumeTags = append(volumeTags, tags.Tag{Key: "pool_id", Value: poolID})
				}
				
				// Add RAID type as tag - critical for understanding volume configuration
				if volumeInfo.RAIDType != "" {
					volumeTags = append(volumeTags, tags.Tag{Key: "raid_type", Value: volumeInfo.RAIDType})
				}
				
				// Add cache policy as tag - impacts performance
				if volumeInfo.WriteCachePolicy != "" {
					volumeTags = append(volumeTags, tags.Tag{Key: "write_cache_policy", Value: volumeInfo.WriteCachePolicy})
				}
				
				// Add block size as tag, not metric
				if volumeInfo.BlockSizeBytes > 0 {
					volumeTags = append(volumeTags, tags.Tag{Key: "block_size_bytes", Value: fmt.Sprintf("%d", volumeInfo.BlockSizeBytes)})
				}
				
				// Add access capabilities as tag
				if len(volumeInfo.AccessCapabilities) > 0 {
					accessCapabilities := strings.Join(volumeInfo.AccessCapabilities, ",")
					volumeTags = append(volumeTags, tags.Tag{Key: "access_capabilities", Value: accessCapabilities})
				}
				
				// Add encryption types as tag
				if len(volumeInfo.EncryptionTypes) > 0 {
					encryptionType := strings.Join(volumeInfo.EncryptionTypes, ",")
					volumeTags = append(volumeTags, tags.Tag{Key: "encryption_type", Value: encryptionType})
				}

				// Volume health - essential operational metric
				if volumeInfo.Status != nil && volumeInfo.Status.Health != "" {
					datapoints = append(datapoints, data_store.DataPoint{
						Name:      "hardware.storage.volume.health",
						Timestamp: timestamp,
						Value:     float32(mapHealthState(volumeInfo.Status.Health)),
						Tags:      volumeTags,
					})
				}
				
				// Volume encryption state - security metric
				datapoints = append(datapoints, data_store.DataPoint{
					Name:      "hardware.storage.volume.encrypted",
					Timestamp: timestamp,
					Value:     float32(boolToFloat(volumeInfo.Encrypted)),
					Tags:      volumeTags,
				})

				// Debug logging for volume capacity data
				c.logger.Debug().
					Str("volume_id", volumeInfo.ID).
					Int64("capacity_bytes", volumeInfo.CapacityBytes).
					Int64("allocated_bytes", int64(volumeInfo.Capacity.Data.AllocatedBytes)).
					Int64("consumed_bytes", int64(volumeInfo.Capacity.Data.ConsumedBytes)).
					Int("remaining_capacity_percent", volumeInfo.RemainingCapacityPercent).
					Msg("Volume capacity data before calculation")
				
				// For Dell ME storage, use AllocatedBytes if CapacityBytes is 0
				effectiveVolumeCapacity := volumeInfo.CapacityBytes
				if effectiveVolumeCapacity == 0 && volumeInfo.Capacity.Data.AllocatedBytes > 0 {
					effectiveVolumeCapacity = volumeInfo.Capacity.Data.AllocatedBytes
				}
				
				// Volume capacity metrics - core metrics for capacity planning
				if effectiveVolumeCapacity > 0 {
					// Total capacity
					datapoints = append(datapoints, data_store.DataPoint{
						Name:      "hardware.storage.volume.capacity.total",
						Timestamp: timestamp,
						Value:     float32(effectiveVolumeCapacity),
						Tags:      volumeTags,
					})
					
					// Allocated capacity - storage reserved for the volume
					if volumeInfo.Capacity.Data.AllocatedBytes > 0 {
						allocatedBytes := float32(volumeInfo.Capacity.Data.AllocatedBytes)
						
						datapoints = append(datapoints, data_store.DataPoint{
							Name:      "hardware.storage.volume.capacity.allocated",
							Timestamp: timestamp,
							Value:     allocatedBytes,
							Tags:      volumeTags,
						})
						
						// Calculate allocated percentage
						allocatedPercent := roundToTwoDecimals((allocatedBytes / float32(effectiveVolumeCapacity)) * 100.0)
						datapoints = append(datapoints, data_store.DataPoint{
							Name:      "hardware.storage.volume.capacity.allocated_percent",
							Timestamp: timestamp,
							Value:     allocatedPercent,
							Tags:      volumeTags,
						})
					}
					
					// Consumed capacity - actual space used
					if volumeInfo.Capacity.Data.ConsumedBytes > 0 {
						consumedBytes := float32(volumeInfo.Capacity.Data.ConsumedBytes)
						
						datapoints = append(datapoints, data_store.DataPoint{
							Name:      "hardware.storage.volume.capacity.used",
							Timestamp: timestamp,
							Value:     consumedBytes,
							Tags:      volumeTags,
						})
						
						// Calculate used percentage
						usedPercent := roundToTwoDecimals((consumedBytes / float32(effectiveVolumeCapacity)) * 100.0)
						datapoints = append(datapoints, data_store.DataPoint{
							Name:      "hardware.storage.volume.capacity.used_percent",
							Timestamp: timestamp,
							Value:     usedPercent,
							Tags:      volumeTags,
						})
					}
					
					// Remaining capacity percentage - direct from API for accuracy
					if volumeInfo.RemainingCapacityPercent > 0 {
						datapoints = append(datapoints, data_store.DataPoint{
							Name:      "hardware.storage.volume.capacity.free_percent",
							Timestamp: timestamp,
							Value:     roundToTwoDecimals(float32(volumeInfo.RemainingCapacityPercent)),
							Tags:      volumeTags,
						})
						
						// Calculate free bytes based on percentage
						freeBytes := roundToTwoDecimals(float32(effectiveVolumeCapacity) * (float32(volumeInfo.RemainingCapacityPercent) / 100.0))
						datapoints = append(datapoints, data_store.DataPoint{
							Name:      "hardware.storage.volume.capacity.free",
							Timestamp: timestamp,
							Value:     freeBytes,
							Tags:      volumeTags,
						})
					}
				}
				
				// IO Statistics for performance metrics - essential I/O metrics
				if volumeInfo.IOStatistics.ReadHitIORequests > 0 || volumeInfo.IOStatistics.WriteHitIORequests > 0 {
					// Total I/O operations - combined metric for overall activity
					totalIO := float32(volumeInfo.IOStatistics.ReadHitIORequests + volumeInfo.IOStatistics.WriteHitIORequests)
					datapoints = append(datapoints, data_store.DataPoint{
						Name:      "hardware.storage.volume.io.total_ops",
						Timestamp: timestamp,
						Value:     totalIO,
						Tags:      volumeTags,
					})
					
					// Read operations - separate to understand read workload
					datapoints = append(datapoints, data_store.DataPoint{
						Name:      "hardware.storage.volume.io.reads",
						Timestamp: timestamp,
						Value:     float32(volumeInfo.IOStatistics.ReadHitIORequests),
						Tags:      volumeTags,
					})
					
					// Write operations - separate to understand write workload
					datapoints = append(datapoints, data_store.DataPoint{
						Name:      "hardware.storage.volume.io.writes",
						Timestamp: timestamp,
						Value:     float32(volumeInfo.IOStatistics.WriteHitIORequests),
						Tags:      volumeTags,
					})
					
					// Total bytes - combined data volume metric
					if volumeInfo.IOStatistics.ReadIOKiBytes > 0 || volumeInfo.IOStatistics.WriteIOKiBytes > 0 {
						totalBytes := float32((volumeInfo.IOStatistics.ReadIOKiBytes + volumeInfo.IOStatistics.WriteIOKiBytes) * 1024)
						datapoints = append(datapoints, data_store.DataPoint{
							Name:      "hardware.storage.volume.io.total_bytes",
							Timestamp: timestamp,
							Value:     totalBytes,
							Tags:      volumeTags,
						})
					}
					
					// Read bytes - data volume for reads (convert KiB to bytes)
					if volumeInfo.IOStatistics.ReadIOKiBytes > 0 {
						datapoints = append(datapoints, data_store.DataPoint{
							Name:      "hardware.storage.volume.io.read.bytes",
							Timestamp: timestamp,
							Value:     float32(volumeInfo.IOStatistics.ReadIOKiBytes * 1024),
							Tags:      volumeTags,
						})
					}
					
					// Write bytes - data volume for writes (convert KiB to bytes)
					if volumeInfo.IOStatistics.WriteIOKiBytes > 0 {
						datapoints = append(datapoints, data_store.DataPoint{
							Name:      "hardware.storage.volume.io.write.bytes",
							Timestamp: timestamp,
							Value:     float32(volumeInfo.IOStatistics.WriteIOKiBytes * 1024),
							Tags:      volumeTags,
						})
					}
					
					// Read request time if available - performance metric
					if volumeInfo.IOStatistics.ReadIORequestTime != "" {
						if readTime, err := strconv.ParseFloat(volumeInfo.IOStatistics.ReadIORequestTime, 32); err == nil {
							datapoints = append(datapoints, data_store.DataPoint{
								Name:      "hardware.storage.volume.io.read.latency",
								Timestamp: timestamp,
								Value:     float32(readTime),
								Tags:      volumeTags,
							})
						}
					}
					
					// Write request time if available - performance metric
					if volumeInfo.IOStatistics.WriteIORequestTime != "" {
						if writeTime, err := strconv.ParseFloat(volumeInfo.IOStatistics.WriteIORequestTime, 32); err == nil {
							datapoints = append(datapoints, data_store.DataPoint{
								Name:      "hardware.storage.volume.io.write.latency",
								Timestamp: timestamp,
								Value:     float32(writeTime),
								Tags:      volumeTags,
							})
						}
					}
				}
				
				// Try to extract additional OEM-specific data if standard fields weren't sufficient
				var volumeRawData map[string]interface{}
				if err := json.Unmarshal(volResp.Raw, &volumeRawData); err == nil {
					if oemData, ok := volumeRawData["Oem"]; ok {
						if oemMap, ok := oemData.(map[string]interface{}); ok {
							// Process Dell-specific OEM data
							if dellData, ok := oemMap["Dell"]; ok {
								c.processVolumeOemDellData(dellData, effectiveVolumeCapacity, volumeTags, timestamp, &datapoints)
							}
							
							// Process HPE-specific OEM data
							if hpeData, ok := oemMap["Hpe"]; ok {
								c.processVolumeOemHpeData(hpeData, effectiveVolumeCapacity, volumeTags, timestamp, &datapoints)
							}
						}
					}
				}
			}
		}
	}
	
	return datapoints, nil
}

// Helper function to process Dell OEM volume data
func (c *StorageCollector) processVolumeOemDellData(dellData interface{}, capacityBytes int64, volumeTags []tags.Tag, timestamp time.Time, datapoints *[]data_store.DataPoint) {
	if dellMap, ok := dellData.(map[string]interface{}); ok {
		// Process UsedBytes if available
		if usedBytesRaw, ok := dellMap["UsedBytes"]; ok {
			if usedBytes, ok := usedBytesRaw.(float64); ok && usedBytes > 0 {
				*datapoints = append(*datapoints, data_store.DataPoint{
					Name:      "hardware.storage.volume.capacity.used",
					Timestamp: timestamp,
					Value:     float32(usedBytes),
					Tags:      volumeTags,
				})
				
				// Calculate and add used percentage if total capacity is available
				if capacityBytes > 0 {
					usedPercent := roundToTwoDecimals((float32(usedBytes) / float32(capacityBytes)) * 100.0)
					*datapoints = append(*datapoints, data_store.DataPoint{
						Name:      "hardware.storage.volume.capacity.used_percent",
						Timestamp: timestamp,
						Value:     usedPercent,
						Tags:      volumeTags,
					})
				}
			}
		}
		
		// Process IO Stats if available
		if ioStatsRaw, ok := dellMap["IOStats"]; ok {
			if ioStats, ok := ioStatsRaw.(map[string]interface{}); ok {
				// Read operations
				if readOpsRaw, ok := ioStats["ReadOps"]; ok {
					if readOps, ok := readOpsRaw.(float64); ok {
						*datapoints = append(*datapoints, data_store.DataPoint{
							Name:      "hardware.storage.volume.io.reads",
							Timestamp: timestamp,
							Value:     float32(readOps),
							Tags:      volumeTags,
						})
					}
				}
				
				// Read bytes
				if readBytesRaw, ok := ioStats["ReadBytes"]; ok {
					if readBytes, ok := readBytesRaw.(float64); ok {
						*datapoints = append(*datapoints, data_store.DataPoint{
							Name:      "hardware.storage.volume.io.read.bytes",
							Timestamp: timestamp,
							Value:     float32(readBytes),
							Tags:      volumeTags,
						})
					}
				}
				
				// Write operations
				if writeOpsRaw, ok := ioStats["WriteOps"]; ok {
					if writeOps, ok := writeOpsRaw.(float64); ok {
						*datapoints = append(*datapoints, data_store.DataPoint{
							Name:      "hardware.storage.volume.io.writes",
							Timestamp: timestamp,
							Value:     float32(writeOps),
							Tags:      volumeTags,
						})
					}
				}
				
				// Write bytes
				if writeBytesRaw, ok := ioStats["WriteBytes"]; ok {
					if writeBytes, ok := writeBytesRaw.(float64); ok {
						*datapoints = append(*datapoints, data_store.DataPoint{
							Name:      "hardware.storage.volume.io.write.bytes",
							Timestamp: timestamp,
							Value:     float32(writeBytes),
							Tags:      volumeTags,
						})
					}
				}
				
				// Calculate total I/O operations
				var totalOps float32 = 0
				if readOpsRaw, ok := ioStats["ReadOps"]; ok {
					if readOps, ok := readOpsRaw.(float64); ok {
						totalOps += float32(readOps)
					}
				}
				if writeOpsRaw, ok := ioStats["WriteOps"]; ok {
					if writeOps, ok := writeOpsRaw.(float64); ok {
						totalOps += float32(writeOps)
					}
				}
				
				if totalOps > 0 {
					*datapoints = append(*datapoints, data_store.DataPoint{
						Name:      "hardware.storage.volume.io.total_ops",
						Timestamp: timestamp,
						Value:     totalOps,
						Tags:      volumeTags,
					})
				}
				
				// Calculate total I/O bytes
				var totalBytes float32 = 0
				if readBytesRaw, ok := ioStats["ReadBytes"]; ok {
					if readBytes, ok := readBytesRaw.(float64); ok {
						totalBytes += float32(readBytes)
					}
				}
				if writeBytesRaw, ok := ioStats["WriteBytes"]; ok {
					if writeBytes, ok := writeBytesRaw.(float64); ok {
						totalBytes += float32(writeBytes)
					}
				}
				
				if totalBytes > 0 {
					*datapoints = append(*datapoints, data_store.DataPoint{
						Name:      "hardware.storage.volume.io.total_bytes",
						Timestamp: timestamp,
						Value:     totalBytes,
						Tags:      volumeTags,
					})
				}
				
				// Check for latency information
				if readLatencyRaw, ok := ioStats["ReadLatency"]; ok {
					if readLatency, ok := readLatencyRaw.(float64); ok && readLatency > 0 {
						*datapoints = append(*datapoints, data_store.DataPoint{
							Name:      "hardware.storage.volume.io.read.latency",
							Timestamp: timestamp,
							Value:     float32(readLatency),
							Tags:      volumeTags,
						})
					}
				}
				
				if writeLatencyRaw, ok := ioStats["WriteLatency"]; ok {
					if writeLatency, ok := writeLatencyRaw.(float64); ok && writeLatency > 0 {
						*datapoints = append(*datapoints, data_store.DataPoint{
							Name:      "hardware.storage.volume.io.write.latency",
							Timestamp: timestamp,
							Value:     float32(writeLatency),
							Tags:      volumeTags,
						})
					}
				}
			}
		}
	}
}

// Helper function to process HPE OEM volume data
func (c *StorageCollector) processVolumeOemHpeData(hpeData interface{}, capacityBytes int64, volumeTags []tags.Tag, timestamp time.Time, datapoints *[]data_store.DataPoint) {
	if hpeMap, ok := hpeData.(map[string]interface{}); ok {
		if spaceInfoRaw, ok := hpeMap["VolumeSpaceInfo"]; ok {
			if spaceInfo, ok := spaceInfoRaw.(map[string]interface{}); ok {
				// Process UsedSpace if available
				if usedRaw, ok := spaceInfo["UsedSpace"]; ok {
					if usedBytes, ok := usedRaw.(float64); ok && usedBytes > 0 {
						*datapoints = append(*datapoints, data_store.DataPoint{
							Name:      "hardware.storage.volume.capacity.used",
							Timestamp: timestamp,
							Value:     float32(usedBytes),
							Tags:      volumeTags,
						})
					}
				}
				
				// Process allocated and reserved space if available
				if allocatedRaw, ok := spaceInfo["AllocatedSpace"]; ok {
					if allocatedBytes, ok := allocatedRaw.(float64); ok && allocatedBytes > 0 {
						*datapoints = append(*datapoints, data_store.DataPoint{
							Name:      "hardware.storage.volume.capacity.allocated",
							Timestamp: timestamp,
							Value:     float32(allocatedBytes),
							Tags:      volumeTags,
						})
					}
				}
				
				if reservedRaw, ok := spaceInfo["ReservedSpace"]; ok {
					if reservedBytes, ok := reservedRaw.(float64); ok && reservedBytes > 0 {
						*datapoints = append(*datapoints, data_store.DataPoint{
							Name:      "hardware.storage.volume.capacity.reserved",
							Timestamp: timestamp,
							Value:     float32(reservedBytes),
							Tags:      volumeTags,
						})
					}
				}
			}
		}
		
		// Process IO Stats if available
		if statsRaw, ok := hpeMap["IOStatistics"]; ok {
			if stats, ok := statsRaw.(map[string]interface{}); ok {
				// Read operations
				if readOpsRaw, ok := stats["ReadIOCount"]; ok {
					if readOps, ok := readOpsRaw.(float64); ok {
						*datapoints = append(*datapoints, data_store.DataPoint{
							Name:      "hardware.storage.volume.io.reads",
							Timestamp: timestamp,
							Value:     float32(readOps),
							Tags:      volumeTags,
						})
					}
				}
				
				// Read bytes
				if readBytesRaw, ok := stats["ReadIOBytes"]; ok {
					if readBytes, ok := readBytesRaw.(float64); ok {
						*datapoints = append(*datapoints, data_store.DataPoint{
							Name:      "hardware.storage.volume.io.read.bytes",
							Timestamp: timestamp,
							Value:     float32(readBytes),
							Tags:      volumeTags,
						})
					}
				}
				
				// Write operations
				if writeOpsRaw, ok := stats["WriteIOCount"]; ok {
					if writeOps, ok := writeOpsRaw.(float64); ok {
						*datapoints = append(*datapoints, data_store.DataPoint{
							Name:      "hardware.storage.volume.io.writes",
							Timestamp: timestamp,
							Value:     float32(writeOps),
							Tags:      volumeTags,
						})
					}
				}
				
				// Write bytes
				if writeBytesRaw, ok := stats["WriteIOBytes"]; ok {
					if writeBytes, ok := writeBytesRaw.(float64); ok {
						*datapoints = append(*datapoints, data_store.DataPoint{
							Name:      "hardware.storage.volume.io.write.bytes",
							Timestamp: timestamp,
							Value:     float32(writeBytes),
							Tags:      volumeTags,
						})
					}
				}
			}
		}
	}
}

// roundToTwoDecimals rounds a float32 to 2 decimal places
func roundToTwoDecimals(value float32) float32 {
	return float32(math.Round(float64(value)*100) / 100)
}

// Helper function to convert bool to float
func boolToFloat(b bool) float64 {
	if b {
		return 1.0
	}
	return 0.0
}

// collectControllerHealthMetrics collects essential health metrics for storage controllers
// including redundancy status and controller information
func (c *StorageCollector) collectControllerHealthMetrics(ctx context.Context, timestamp time.Time) ([]data_store.DataPoint, error) {
	var datapoints []data_store.DataPoint

	// Get system name to use as the host tag
	var hostName string
	if len(c.systems) > 0 {
		sysResp, err := c.client.Get(ctx, c.systems[0])
		if err == nil && sysResp.Name != "" {
			hostName = sysResp.Name
		}
	}
	if hostName == "" {
		rootResp, err := c.client.Get(ctx, "")
		if err == nil && rootResp.UUID != "" {
			hostName = rootResp.UUID
		}
	}

	// Process each controller
	for _, controllerPath := range c.storageControllers {
		ctrlResp, err := c.client.Get(ctx, controllerPath)
		if err != nil {
			continue
		}

		// Extract controller information
		controllerID := ctrlResp.ID
		controllerName := ctrlResp.Name

		// Extract model, manufacturer and serial information when available
		var manufacturer, model, serialNumber string
		
		// Try to get manufacturer from storage controllers
		if len(ctrlResp.StorageControllers) > 0 {
			for _, sc := range ctrlResp.StorageControllers {
				if mfr, ok := sc["Manufacturer"].(string); ok && mfr != "" {
					manufacturer = mfr
				}
				if mod, ok := sc["Model"].(string); ok && mod != "" {
					model = mod
				}
				if sn, ok := sc["SerialNumber"].(string); ok && sn != "" {
					serialNumber = sn
				}
			}
		}

		// Extract controller letter (A/B)
		var controllerLetter string
		if strings.Contains(strings.ToLower(controllerID), "_a") {
			controllerLetter = "A"
		} else if strings.Contains(strings.ToLower(controllerID), "_b") {
			controllerLetter = "B"
		}

		// Controller tags - identification tags with operational context
		controllerTags := []tags.Tag{
			{Key: "controller_id", Value: controllerID},
			{Key: "controller_name", Value: controllerName},
			{Key: "controller_type", Value: "storage"},
		}
		
		// Add controller letter tag if available - important for operations
		if controllerLetter != "" {
			controllerTags = append(controllerTags, tags.Tag{Key: "controller", Value: controllerLetter})
		}
		
		// Add host tag if available
		if hostName != "" {
			controllerTags = append(controllerTags, tags.Tag{Key: "host", Value: hostName})
		}
		
		// Add manufacturer tag if available
		if manufacturer != "" {
			controllerTags = append(controllerTags, tags.Tag{Key: "manufacturer", Value: manufacturer})
		}
		
		// Add model tag if available
		if model != "" {
			controllerTags = append(controllerTags, tags.Tag{Key: "model", Value: model})
		}
		
		// Add serial number tag if available
		if serialNumber != "" {
			controllerTags = append(controllerTags, tags.Tag{Key: "serial_number", Value: serialNumber})
		}

		// Controller health - essential operational metric
		if ctrlResp.Status != nil && ctrlResp.Status.Health != "" {
			datapoints = append(datapoints, data_store.DataPoint{
				Name:      "hardware.storage.controller.health",
				Timestamp: timestamp,
				Value:     float32(mapHealthState(ctrlResp.Status.Health)),
				Tags:      controllerTags,
			})
		}
		
		// Check for redundancy information - critical for high availability
		var storageData struct {
			Redundancy []struct {
				MaxNumSupported int    `json:"MaxNumSupported"`
				MemberId        string `json:"MemberId"`
				MinNumNeeded    int    `json:"MinNumNeeded"`
				Mode            string `json:"Mode"`
				Name            string `json:"Name"`
				RedundancySet   []struct {
					OdataID string `json:"@odata.id"`
				} `json:"RedundancySet"`
				Status struct {
					Health string `json:"Health"`
					State  string `json:"State"`
				} `json:"Status"`
			} `json:"Redundancy"`
		}
		
		if err := json.Unmarshal(ctrlResp.Raw, &storageData); err == nil {
			// Process redundancy information
			for _, redundancy := range storageData.Redundancy {
				if redundancy.Status.Health != "" {
					// Create tags specific to redundancy group
					redundancyTags := append([]tags.Tag{}, controllerTags...)
					redundancyTags = append(redundancyTags, 
						tags.Tag{Key: "redundancy_group", Value: redundancy.Name},
						tags.Tag{Key: "redundancy_mode", Value: redundancy.Mode},
					)
					
					// Add minimum and maximum redundancy information if available
					if redundancy.MinNumNeeded > 0 {
						redundancyTags = append(redundancyTags, 
							tags.Tag{Key: "min_controllers_needed", Value: fmt.Sprintf("%d", redundancy.MinNumNeeded)})
					}
					
					// Add redundancy health metric
					datapoints = append(datapoints, data_store.DataPoint{
						Name:      "hardware.storage.redundancy.health",
						Timestamp: timestamp,
						Value:     float32(mapHealthState(redundancy.Status.Health)),
						Tags:      redundancyTags,
					})
					
					// Add redundancy count metrics
					if redundancy.MaxNumSupported > 0 {
						datapoints = append(datapoints, data_store.DataPoint{
							Name:      "hardware.storage.redundancy.controllers_max",
							Timestamp: timestamp,
							Value:     float32(redundancy.MaxNumSupported),
							Tags:      redundancyTags,
						})
					}
					
					if redundancy.MinNumNeeded > 0 {
						datapoints = append(datapoints, data_store.DataPoint{
							Name:      "hardware.storage.redundancy.controllers_min",
							Timestamp: timestamp,
							Value:     float32(redundancy.MinNumNeeded),
							Tags:      redundancyTags,
						})
					}
					
					// Add count of currently active controllers
					if len(redundancy.RedundancySet) > 0 {
						datapoints = append(datapoints, data_store.DataPoint{
							Name:      "hardware.storage.redundancy.controllers_active",
							Timestamp: timestamp,
							Value:     float32(len(redundancy.RedundancySet)),
							Tags:      redundancyTags,
						})
					}
				}
			}
		}
	}
	
	return datapoints, nil
}

// followODataLink recursively traverses an OData link to retrieve data
func (c *StorageCollector) followODataLink(ctx context.Context, path string, depth int) (map[string]interface{}, error) {
	// Check for cycles to avoid circular references
	if c.visitedLinks[path] {
		return nil, fmt.Errorf("cycle detected in link traversal: %s", path)
	}
	
	// Check maximum depth to avoid infinite recursion
	if depth > c.maxDepth {
		return nil, fmt.Errorf("max traversal depth reached at: %s", path)
	}
	
	// Mark as visited
	c.visitedLinks[path] = true
	
	// Obtenir la ressource
	normalizedPath := strings.TrimPrefix(path, "/redfish/v1/")
	resp, err := c.client.Get(ctx, normalizedPath)
	if err != nil {
		return nil, fmt.Errorf("failed to get resource at %s: %v", path, err)
	}
	
	// Parse the JSON response
	var data map[string]interface{}
	if err := json.Unmarshal(resp.Raw, &data); err != nil {
		return nil, fmt.Errorf("failed to parse response for %s: %v", path, err)
	}
	
	// Process each field recursively if it's an @odata.id
	for key, value := range data {
		// Ignore the @odata.id field itself
		if key == "@odata.id" {
			continue
		}
		
		// If it's a map/object, check if it has @odata.id
		if subObj, ok := value.(map[string]interface{}); ok {
			if linkID, ok := subObj["@odata.id"].(string); ok {
				// Follow this link recursively
				subData, err := c.followODataLink(ctx, linkID, depth+1)
				if err == nil {
					// Replace the link with actual data
					data[key+"Data"] = subData
				}
			}
		}
		
		// If it's an array, check each item
		if arr, ok := value.([]interface{}); ok {
			for i, item := range arr {
				if itemObj, ok := item.(map[string]interface{}); ok {
					if linkID, ok := itemObj["@odata.id"].(string); ok {
						// Follow this link recursively
						subData, err := c.followODataLink(ctx, linkID, depth+1)
						if err == nil {
							// Since we can't easily modify the array in place,
							// we add a new field with the link data
							itemKey := fmt.Sprintf("%sItem%d", key, i)
							data[itemKey] = subData
						}
					}
				}
			}
		}
	}
	
	return data, nil
}

// collectPoolMetrics collects resource consumption metrics for storage pools
func (c *StorageCollector) collectPoolMetrics(ctx context.Context, timestamp time.Time) ([]data_store.DataPoint, error) {
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

	// For each storage pool
	for _, poolPath := range c.storagePools {
		poolResp, err := c.client.Get(ctx, poolPath)
		if err != nil {
			c.logger.Warn().
				Err(err).
				Str("path", poolPath).
				Msg("Failed to get storage pool details")
			continue
		}
		
		// Extract pool information
		var poolInfo struct {
			ID                   string  `json:"Id"`
			Name                 string  `json:"Name"`
			Status               *Status `json:"Status"`
			Description          string  `json:"Description"`
			MaxBlockSizeBytes    int64   `json:"MaxBlockSizeBytes"`
			CapacityBytes        int64   `json:"CapacityBytes"`
			RemainingCapacityBytes int64 `json:"RemainingCapacityBytes"`
			AllocatedBytes       int64   `json:"AllocatedBytes"`
			RemainingCapacityPercent int `json:"RemainingCapacityPercent"`
			SupportedRAIDTypes   []string `json:"SupportedRAIDTypes"`
			Capacity struct {
				Data struct {
					AllocatedBytes int64 `json:"AllocatedBytes"`
					ConsumedBytes  int64 `json:"ConsumedBytes"`
					VolumesAllocatedBytes int64 `json:"VolumesAllocatedBytes"`
					SnapshotsAllocatedBytes int64 `json:"SnapshotsAllocatedBytes"`
					UnusedBytes int64 `json:"UnusedBytes"`
					TotalCommittedBytes int64 `json:"TotalCommittedBytes"`
				} `json:"Data"`
				IsThinProvisioned bool `json:"IsThinProvisioned"`
			} `json:"Capacity"`
			// IOStatistics for performance metrics
			IOStatistics struct {
				ReadHitIORequests  int64 `json:"ReadHitIORequests"`
				ReadIOKiBytes      int64 `json:"ReadIOKiBytes"`
				ReadIORequestTime  string `json:"ReadIORequestTime"`
				WriteHitIORequests int64 `json:"WriteHitIORequests"`
				WriteIOKiBytes     int64 `json:"WriteIOKiBytes"`
				WriteIORequestTime string `json:"WriteIORequestTime"`
			} `json:"IOStatistics"`
			// OEM-specific fields
			Oem struct {
				Dell struct {
					VolumesBytes int64 `json:"VolumesBytes"`
					SnapshotsBytes int64 `json:"SnapshotsBytes"`
					FreeBytes int64 `json:"FreeBytes"`
					OverCommitBytes int64 `json:"OverCommitBytes"`
					AllocatedSpaceRemaining int64 `json:"AllocatedSpaceRemaining"`
				} `json:"Dell"`
			} `json:"Oem"`
		}
		
		if err := json.Unmarshal(poolResp.Raw, &poolInfo); err != nil {
			c.logger.Warn().
				Err(err).
				Str("path", poolPath).
				Msg("Failed to parse pool details")
			continue
		}
		
		// Extract controller ID from path (e.g., "Storage/controller_a/StoragePools/A" -> "controller_a")
		pathParts := strings.Split(poolPath, "/")
		var controllerID, controllerLetter string
		if len(pathParts) >= 2 {
			controllerID = pathParts[1]
			
			// Extract simple controller letter if it looks like "controller_a" -> "A"
			if strings.Contains(strings.ToLower(controllerID), "_a") {
				controllerLetter = "A"
			} else if strings.Contains(strings.ToLower(controllerID), "_b") {
				controllerLetter = "B"
			}
		}
		
		// Pool tags - essential tags for operational clarity
		poolTags := []tags.Tag{
			{Key: "pool_id", Value: poolInfo.ID},
			{Key: "pool_name", Value: poolInfo.Name},
		}
		
		// Add controller tags if available
		if controllerID != "" {
			poolTags = append(poolTags, tags.Tag{Key: "controller_id", Value: controllerID})
		}
		if controllerLetter != "" {
			poolTags = append(poolTags, tags.Tag{Key: "controller", Value: controllerLetter})
		}
		
		// Add host tag if available
		if hostName != "" {
			poolTags = append(poolTags, tags.Tag{Key: "host", Value: hostName})
		}
		
		// Add description if available
		if poolInfo.Description != "" {
			poolTags = append(poolTags, tags.Tag{Key: "description", Value: poolInfo.Description})
		}
		
		// Add RAID types if available - important operational information
		if len(poolInfo.SupportedRAIDTypes) > 0 {
			raidTypes := strings.Join(poolInfo.SupportedRAIDTypes, ",")
			poolTags = append(poolTags, tags.Tag{Key: "supported_raid_types", Value: raidTypes})
		}
		
		// Add block size if available - as tag, not metric
		if poolInfo.MaxBlockSizeBytes > 0 {
			poolTags = append(poolTags, tags.Tag{Key: "max_block_size_bytes", 
				Value: fmt.Sprintf("%d", poolInfo.MaxBlockSizeBytes)})
		}
		
		// Add thin provisioning information as tag
		if poolInfo.Capacity.IsThinProvisioned {
			poolTags = append(poolTags, tags.Tag{Key: "thin_provisioned", Value: "true"})
		} else {
			poolTags = append(poolTags, tags.Tag{Key: "thin_provisioned", Value: "false"})
		}
		
		// Pool health state - critical operational metric
		if poolInfo.Status != nil && poolInfo.Status.Health != "" {
			datapoints = append(datapoints, data_store.DataPoint{
				Name:      "hardware.storage.pool.health",
				Timestamp: timestamp,
				Value:     float32(mapHealthState(poolInfo.Status.Health)),
				Tags:      poolTags,
			})
		}
		
		// Total capacity - raw capacity of the pool
		if poolInfo.CapacityBytes > 0 {
			datapoints = append(datapoints, data_store.DataPoint{
				Name:      "hardware.storage.pool.capacity.total",
				Timestamp: timestamp,
				Value:     float32(poolInfo.CapacityBytes),
				Tags:      poolTags,
			})
		}
		
		// Pool allocated capacity - showing resource allocation
		allocatedBytes := float32(poolInfo.AllocatedBytes)
		if allocatedBytes == 0 && poolInfo.Capacity.Data.AllocatedBytes > 0 {
			allocatedBytes = float32(poolInfo.Capacity.Data.AllocatedBytes)
		}
		
		if allocatedBytes > 0 {
			datapoints = append(datapoints, data_store.DataPoint{
				Name:      "hardware.storage.pool.capacity.allocated",
				Timestamp: timestamp,
				Value:     allocatedBytes,
				Tags:      poolTags,
			})
			
			// Calculate and add allocated percentage if total capacity is available
			if poolInfo.CapacityBytes > 0 {
				allocatedPercent := roundToTwoDecimals((allocatedBytes / float32(poolInfo.CapacityBytes)) * 100.0)
				datapoints = append(datapoints, data_store.DataPoint{
					Name:      "hardware.storage.pool.capacity.allocated_percent",
					Timestamp: timestamp,
					Value:     allocatedPercent,
					Tags:      poolTags,
				})
			}
		}
		
		// Pool consumed capacity calculation
		// For Dell ME storage arrays, we need to directly apply the RemainingCapacityPercent 
		// to the total capacity to get accurate used capacity values
		// Note: Dell ME interface displays values in decimal TB (10^12 bytes) rather than binary TiB (2^40 bytes)
		
		// Debug logging for pool capacity data
		c.logger.Debug().
			Str("pool_id", poolInfo.ID).
			Int64("capacity_bytes", int64(poolInfo.CapacityBytes)).
			Int("remaining_capacity_percent", poolInfo.RemainingCapacityPercent).
			Int64("allocated_bytes", int64(poolInfo.Capacity.Data.AllocatedBytes)).
			Int64("consumed_bytes", int64(poolInfo.Capacity.Data.ConsumedBytes)).
			Msg("Pool capacity data before calculation")
		
		// For Dell ME storage, always use AllocatedBytes as the effective capacity
		// CapacityBytes is often 0 or incorrect, AllocatedBytes is the real pool capacity
		effectiveCapacity := uint64(poolInfo.Capacity.Data.AllocatedBytes)
		
		if effectiveCapacity > 0 && poolInfo.RemainingCapacityPercent >= 0 {
			
			// For Dell ME, the formula that correctly calculates the used capacity is:
			// UsedBytes = EffectiveCapacity * (100 - RemainingCapacityPercent) / 100
			// This applies the remaining percentage to the total capacity directly
			
			// Calculate used capacity percentage based on RemainingCapacityPercent
			usedPercent := roundToTwoDecimals(100.0 - float32(poolInfo.RemainingCapacityPercent))
			
			// Calculate used bytes by applying the used percentage to the effective capacity
			// Dell ME interface uses decimal TB (10^12 bytes) rather than binary TiB (2^40 bytes)
			// This is the formula that matches the values displayed in the Dell ME interface
			usedBytes := roundToTwoDecimals(float32(effectiveCapacity) * (usedPercent / 100.0))
			
			// Debug logging for calculation result
			c.logger.Debug().
				Str("pool_id", poolInfo.ID).
				Float32("used_percent", usedPercent).
				Float32("used_bytes", usedBytes).
				Msg("Pool capacity calculation result")
			
			// Add the used capacity metric
			datapoints = append(datapoints, data_store.DataPoint{
				Name:      "hardware.storage.pool.capacity.used",
				Timestamp: timestamp,
				Value:     usedBytes,
				Tags:      poolTags,
			})
			
			// Add the used percentage metric
			datapoints = append(datapoints, data_store.DataPoint{
				Name:      "hardware.storage.pool.capacity.used_percent",
				Timestamp: timestamp,
				Value:     usedPercent,
				Tags:      poolTags,
			})
		} else {
			// Debug logging when condition is not met
			c.logger.Debug().
				Str("pool_id", poolInfo.ID).
				Int64("effective_capacity", int64(effectiveCapacity)).
				Int("remaining_capacity_percent", poolInfo.RemainingCapacityPercent).
				Msg("Pool capacity condition not met - skipping used capacity calculation")
		}
		
		// Remaining capacity as a percentage - direct from API for accuracy
		if poolInfo.RemainingCapacityPercent >= 0 {
			datapoints = append(datapoints, data_store.DataPoint{
				Name:      "hardware.storage.pool.capacity.free_percent",
				Timestamp: timestamp,
				Value:     roundToTwoDecimals(float32(poolInfo.RemainingCapacityPercent)),
				Tags:      poolTags,
			})
		}
		
		// Remaining capacity in bytes - direct from API when available
		if poolInfo.RemainingCapacityBytes > 0 {
			datapoints = append(datapoints, data_store.DataPoint{
				Name:      "hardware.storage.pool.capacity.free",
				Timestamp: timestamp,
				Value:     float32(poolInfo.RemainingCapacityBytes),
				Tags:      poolTags,
			})
		} else if poolInfo.Capacity.Data.UnusedBytes > 0 {
			// Try alternate field if RemainingCapacityBytes not available
			datapoints = append(datapoints, data_store.DataPoint{
				Name:      "hardware.storage.pool.capacity.free",
				Timestamp: timestamp,
				Value:     float32(poolInfo.Capacity.Data.UnusedBytes),
				Tags:      poolTags,
			})
		} else if poolInfo.Oem.Dell.FreeBytes > 0 {
			// Try Dell OEM data as last resort
			datapoints = append(datapoints, data_store.DataPoint{
				Name:      "hardware.storage.pool.capacity.free",
				Timestamp: timestamp,
				Value:     float32(poolInfo.Oem.Dell.FreeBytes),
				Tags:      poolTags,
			})
		}
		
		// Pool volumes allocated capacity (if available) - important for understanding pool usage
		if poolInfo.Capacity.Data.VolumesAllocatedBytes > 0 {
			datapoints = append(datapoints, data_store.DataPoint{
				Name:      "hardware.storage.pool.capacity.volumes",
				Timestamp: timestamp,
				Value:     float32(poolInfo.Capacity.Data.VolumesAllocatedBytes),
				Tags:      poolTags,
			})
		} else if poolInfo.Oem.Dell.VolumesBytes > 0 {
			// Try Dell OEM data if standard field not available
			datapoints = append(datapoints, data_store.DataPoint{
				Name:      "hardware.storage.pool.capacity.volumes",
				Timestamp: timestamp,
				Value:     float32(poolInfo.Oem.Dell.VolumesBytes),
				Tags:      poolTags,
			})
		}
		
		// Pool snapshots allocated capacity (if available) - important for understanding pool usage
		if poolInfo.Capacity.Data.SnapshotsAllocatedBytes > 0 {
			datapoints = append(datapoints, data_store.DataPoint{
				Name:      "hardware.storage.pool.capacity.snapshots",
				Timestamp: timestamp,
				Value:     float32(poolInfo.Capacity.Data.SnapshotsAllocatedBytes),
				Tags:      poolTags,
			})
		} else if poolInfo.Oem.Dell.SnapshotsBytes > 0 {
			// Try Dell OEM data if standard field not available
			datapoints = append(datapoints, data_store.DataPoint{
				Name:      "hardware.storage.pool.capacity.snapshots",
				Timestamp: timestamp,
				Value:     float32(poolInfo.Oem.Dell.SnapshotsBytes),
				Tags:      poolTags,
			})
		}
		
		// Pool committed capacity (if available) - important for thin provisioning
		if poolInfo.Capacity.Data.TotalCommittedBytes > 0 {
			datapoints = append(datapoints, data_store.DataPoint{
				Name:      "hardware.storage.pool.capacity.committed",
				Timestamp: timestamp,
				Value:     float32(poolInfo.Capacity.Data.TotalCommittedBytes),
				Tags:      poolTags,
			})
		}
		
		// Track pool overcommit (if available) - critical for thin provisioning
		if poolInfo.Oem.Dell.OverCommitBytes > 0 {
			datapoints = append(datapoints, data_store.DataPoint{
				Name:      "hardware.storage.pool.capacity.overcommit",
				Timestamp: timestamp,
				Value:     float32(poolInfo.Oem.Dell.OverCommitBytes),
				Tags:      poolTags,
			})
		}
		
		// Process IO statistics if available
		if poolInfo.IOStatistics.ReadHitIORequests > 0 || poolInfo.IOStatistics.WriteHitIORequests > 0 {
			// Read operations
			datapoints = append(datapoints, data_store.DataPoint{
				Name:      "hardware.storage.pool.io.reads",
				Timestamp: timestamp,
				Value:     float32(poolInfo.IOStatistics.ReadHitIORequests),
				Tags:      poolTags,
			})
			
			// Read bytes (convert KiBytes to bytes)
			if poolInfo.IOStatistics.ReadIOKiBytes > 0 {
				datapoints = append(datapoints, data_store.DataPoint{
					Name:      "hardware.storage.pool.io.read.bytes",
					Timestamp: timestamp,
					Value:     float32(poolInfo.IOStatistics.ReadIOKiBytes * 1024),
					Tags:      poolTags,
				})
			}
			
			// Write operations
			datapoints = append(datapoints, data_store.DataPoint{
				Name:      "hardware.storage.pool.io.writes",
				Timestamp: timestamp,
				Value:     float32(poolInfo.IOStatistics.WriteHitIORequests),
				Tags:      poolTags,
			})
			
			// Write bytes (convert KiBytes to bytes)
			if poolInfo.IOStatistics.WriteIOKiBytes > 0 {
				datapoints = append(datapoints, data_store.DataPoint{
					Name:      "hardware.storage.pool.io.write.bytes",
					Timestamp: timestamp,
					Value:     float32(poolInfo.IOStatistics.WriteIOKiBytes * 1024),
					Tags:      poolTags,
				})
			}
		}
	}
	
	return datapoints, nil
}
