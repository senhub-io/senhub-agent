package redfish

import (
	"context"
	"encoding/json"
	"fmt"
	"senhub-agent.go/internal/agent/services/data_store"
	"senhub-agent.go/internal/agent/services/logger"
	"senhub-agent.go/internal/agent/tags"
	"time"
)

// CiscoCollector implements Cisco-specific Redfish collection
type CiscoCollector struct {
	*GenericCollector
	// Cisco specific fields
	cimcVersion string
	deviceType  string // UCS C-Series, S-Series, etc.
	pid         string // Product ID
	vid         string // Vendor ID
}

// NewCiscoCollector creates a new collector for Cisco servers
func NewCiscoCollector(endpoint, username, password string, logger *logger.Logger, verifySSL bool) (RedfishCollector, error) {
	// First create a generic collector as the base
	genericCollector, err := NewGenericCollector(endpoint, username, password, logger, verifySSL)
	if err != nil {
		return nil, err
	}

	// Cast to use the embedded fields and methods
	gc, ok := genericCollector.(*GenericCollector)
	if !ok {
		return nil, fmt.Errorf("unexpected collector type, cannot create Cisco collector")
	}

	// Create the Cisco collector with the generic one embedded
	collector := &CiscoCollector{
		GenericCollector: gc,
	}

	// Override the vendor type
	collector.vendorType = VendorCisco

	return collector, nil
}

// Connect implements Cisco-specific connection logic
func (c *CiscoCollector) Connect(ctx context.Context) error {
	// First use the generic connect method
	if err := c.GenericCollector.Connect(ctx); err != nil {
		return err
	}

	// Get Cisco-specific information
	if err := c.getCiscoInfo(ctx); err != nil {
		c.logger.Warn().Err(err).Msg("Failed to get Cisco-specific info")
		// Continue anyway, as this is not critical
	}

	return nil
}

// getCiscoInfo retrieves Cisco-specific information like CIMC version
func (c *CiscoCollector) getCiscoInfo(ctx context.Context) error {
	// Try to get Cisco-specific manager (CIMC) info
	resp, err := c.client.Get(ctx, "Managers/CIMC")
	if err != nil {
		// Try alternative path
		resp, err = c.client.Get(ctx, "Managers/1")
		if err != nil {
			return fmt.Errorf("failed to get CIMC info: %v", err)
		}
	}

	// Extract CIMC version
	c.cimcVersion = resp.FirmwareVersion

	// Get system information if available
	if len(c.systems) > 0 {
		sysResp, err := c.client.Get(ctx, c.systems[0])
		if err == nil {
			// Try to get Cisco-specific OEM data
			if sysResp.Oem != nil {
				ciscoData, hasCisco := sysResp.Oem["Cisco"]
				if hasCisco {
					ciscoOem, ok := ciscoData.(map[string]interface{})
					if ok {
						// Get PID and VID if available
						if pid, hasPid := ciscoOem["Pid"]; hasPid {
							c.pid = fmt.Sprintf("%v", pid)
						}
						if vid, hasVid := ciscoOem["Vid"]; hasVid {
							c.vid = fmt.Sprintf("%v", vid)
						}
					}
				}
			}

			// Determine device type based on model
			if sysResp.Model != "" {
				if containsIgnoreCase(sysResp.Model, "UCSC") {
					c.deviceType = "UCS C-Series"
				} else if containsIgnoreCase(sysResp.Model, "UCSS") {
					c.deviceType = "UCS S-Series"
				} else if containsIgnoreCase(sysResp.Model, "UCSX") {
					c.deviceType = "UCS X-Series"
				} else if containsIgnoreCase(sysResp.Model, "UCSD") {
					c.deviceType = "UCS Director"
				} else {
					c.deviceType = "Unknown UCS"
				}
			}
		}
	}

	c.logger.Debug().
		Str("cimc_version", c.cimcVersion).
		Str("device_type", c.deviceType).
		Str("pid", c.pid).
		Str("vid", c.vid).
		Msg("Retrieved Cisco-specific information")

	return nil
}

// GetSupportedCollections returns Cisco-specific collection capabilities
func (c *CiscoCollector) GetSupportedCollections() []CollectionType {
	// Cisco supports all generic collections plus some extras
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
func (c *CiscoCollector) IsSupported(collectionType CollectionType) bool {
	switch collectionType {
	case CollectionSystem, CollectionThermal, CollectionPower,
		CollectionProcessor, CollectionMemory, CollectionStorage,
		CollectionDrives, CollectionNetworkAdapter:
		return true
	default:
		return false
	}
}

// CollectMetrics gathers Cisco-specific metrics
func (c *CiscoCollector) CollectMetrics(ctx context.Context, collectionType CollectionType, timestamp time.Time) ([]data_store.DataPoint, error) {
	// For most collection types, use the generic collector
	switch collectionType {
	case CollectionSystem:
		return c.collectSystemMetrics(ctx, timestamp)
	case CollectionNetworkAdapter:
		return c.collectNetworkMetrics(ctx, timestamp)
	case CollectionStorage, CollectionDrives:
		return c.collectStorageMetrics(ctx, timestamp)
	default:
		// For other types, delegate to the generic collector
		return c.GenericCollector.CollectMetrics(ctx, collectionType, timestamp)
	}
}

// collectSystemMetrics gathers Cisco-specific system metrics
func (c *CiscoCollector) collectSystemMetrics(ctx context.Context, timestamp time.Time) ([]data_store.DataPoint, error) {
	// Get the generic system metrics first
	datapoints, err := c.GenericCollector.collectSystemMetrics(ctx, timestamp)
	if err != nil {
		return nil, err
	}

	// Add Cisco-specific system datapoints
	for _, systemPath := range c.systems {
		// Get Cisco OEM data for this system
		resp, err := c.client.Get(ctx, systemPath)
		if err != nil {
			c.logger.Warn().
				Err(err).
				Str("path", systemPath).
				Msg("Failed to get Cisco system details")
			continue
		}

		// Extract system tags
		systemTags := []tags.Tag{
			{Key: "system_id", Value: resp.ID},
			{Key: "system_name", Value: resp.Name},
			{Key: "vendor", Value: string(c.vendorType)},
			{Key: "cimc_version", Value: c.cimcVersion},
		}

		if c.deviceType != "" {
			systemTags = append(systemTags, tags.Tag{Key: "device_type", Value: c.deviceType})
		}
		if c.pid != "" {
			systemTags = append(systemTags, tags.Tag{Key: "pid", Value: c.pid})
		}
		if c.vid != "" {
			systemTags = append(systemTags, tags.Tag{Key: "vid", Value: c.vid})
		}

		// Add Cisco-specific OEM data if available
		if resp.Oem != nil {
			ciscoData, hasCisco := resp.Oem["Cisco"]
			if hasCisco {
				ciscoOem, ok := ciscoData.(map[string]interface{})
				if ok {
					// Add BIOS version if available
					if biosVersion, hasBios := ciscoOem["BiosVersion"]; hasBios {
						biosVersionStr := fmt.Sprintf("%v", biosVersion)
						systemTags = append(systemTags, tags.Tag{Key: "bios_version", Value: biosVersionStr})
					}

					// Add CIMC mode if available
					if cimcMode, hasMode := ciscoOem["CimcMode"]; hasMode {
						cimcModeStr := fmt.Sprintf("%v", cimcMode)
						systemTags = append(systemTags, tags.Tag{Key: "cimc_mode", Value: cimcModeStr})
					}
				}
			}
		}

		// Add CIMC version datapoint if available
		if c.cimcVersion != "" {
			// We don't have a numeric value for version, so we use 1.0 as a sentinel value
			// The actual version is in the tags
			datapoints = append(datapoints, data_store.DataPoint{
				Name:      "system.cimc_firmware",
				Timestamp: timestamp,
				Value:     1.0, // Sentinel value
				Tags:      systemTags,
			})
		}
	}

	return datapoints, nil
}

// collectNetworkMetrics gathers Cisco-specific network metrics
func (c *CiscoCollector) collectNetworkMetrics(ctx context.Context, timestamp time.Time) ([]data_store.DataPoint, error) {
	// For Cisco UCS, there are two paths to check for network adapters
	// 1. Standard Redfish path
	// 2. Cisco OEM-specific path

	// First get the standard metrics
	datapoints, err := c.GenericCollector.collectNetworkMetrics(ctx, timestamp)
	if err != nil {
		// If we get an error, still try the Cisco-specific paths
		c.logger.Warn().
			Err(err).
			Msg("Failed to get standard network metrics, continuing with Cisco-specific")
		datapoints = []data_store.DataPoint{}
	}

	// Then check for Cisco-specific network data
	if len(c.systems) > 0 {
		// Try Cisco VIC adapters path
		vicPath := c.systems[0] + "/Oem/Cisco/VIC"
		vicResp, err := c.client.GetRaw(ctx, vicPath)
		if err == nil {
			// Parse the raw response
			var vicData map[string]interface{}
			if err := json.Unmarshal(vicResp, &vicData); err == nil {
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

				// Process VIC adapters
				if adapters, hasAdapters := vicData["Members"]; hasAdapters {
					adaptersList, ok := adapters.([]interface{})
					if ok {
						for _, adapter := range adaptersList {
							adapterData, ok := adapter.(map[string]interface{})
							if !ok {
								continue
							}

							// Extract adapter URL to get full details
							if adapterId, hasId := adapterData["@odata.id"]; hasId {
								adapterPath := fmt.Sprintf("%v", adapterId)
								adapterResp, err := c.client.GetRaw(ctx, adapterPath)
								if err != nil {
									c.logger.Warn().
										Err(err).
										Str("path", adapterPath).
										Msg("Failed to get adapter details")
									continue
								}

								var adapterDetails map[string]interface{}
								if err := json.Unmarshal(adapterResp, &adapterDetails); err != nil {
									continue
								}

								// Get adapter name and model
								var adapterName, adapterModel string
								if name, hasName := adapterDetails["Name"]; hasName {
									adapterName = fmt.Sprintf("%v", name)
								} else {
									adapterName = "VIC Adapter"
								}

								if model, hasModel := adapterDetails["Model"]; hasModel {
									adapterModel = fmt.Sprintf("%v", model)
								}

								// Create adapter-specific tags
								adapterTags := append([]tags.Tag{}, systemTags...)
								adapterTags = append(adapterTags, tags.Tag{Key: "adapter_name", Value: adapterName})
								if adapterModel != "" {
									adapterTags = append(adapterTags, tags.Tag{Key: "model", Value: adapterModel})
								}

								// Get adapter health status
								var healthState int
								if status, hasStatus := adapterDetails["Status"]; hasStatus {
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
									Name:      "network.adapter.health",
									Timestamp: timestamp,
									Value:     float32(healthState),
									Tags:      adapterTags,
								})

								// Process adapter ports if available
								if ports, hasPorts := adapterDetails["Ports"]; hasPorts {
									// Get URL to ports collection
									portsData, ok := ports.(map[string]interface{})
									if !ok {
										continue
									}

									if portsPath, hasPath := portsData["@odata.id"]; hasPath {
										portsURL := fmt.Sprintf("%v", portsPath)
										portsResp, err := c.client.GetRaw(ctx, portsURL)
										if err != nil {
											continue
										}

										var portsCollection map[string]interface{}
										if err := json.Unmarshal(portsResp, &portsCollection); err != nil {
											continue
										}

										// Process each port
										if portMembers, hasMembers := portsCollection["Members"]; hasMembers {
											portsList, ok := portMembers.([]interface{})
											if !ok {
												continue
											}

											for _, port := range portsList {
												portData, ok := port.(map[string]interface{})
												if !ok {
													continue
												}

												if portPath, hasPath := portData["@odata.id"]; hasPath {
													portURL := fmt.Sprintf("%v", portPath)
													portResp, err := c.client.GetRaw(ctx, portURL)
													if err != nil {
														continue
													}

													var portDetails map[string]interface{}
													if err := json.Unmarshal(portResp, &portDetails); err != nil {
														continue
													}

													// Get port name
													var portName string
													if name, hasName := portDetails["Name"]; hasName {
														portName = fmt.Sprintf("%v", name)
													} else if id, hasId := portDetails["Id"]; hasId {
														portName = fmt.Sprintf("Port %v", id)
													} else {
														portName = "VIC Port"
													}

													// Create port-specific tags
													portTags := append([]tags.Tag{}, adapterTags...)
													portTags = append(portTags, tags.Tag{Key: "port_name", Value: portName})

													// Get port speed
													if linkSpeed, hasSpeed := portDetails["CurrentLinkSpeed"]; hasSpeed {
														speedStr := fmt.Sprintf("%v", linkSpeed)
														portTags = append(portTags, tags.Tag{Key: "link_speed", Value: speedStr})
													}

													// Get port status
													var linkUp float32 = 0.0
													if linkStatus, hasStatus := portDetails["LinkStatus"]; hasStatus {
														statusStr := fmt.Sprintf("%v", linkStatus)
														if containsIgnoreCase(statusStr, "up") {
															linkUp = 1.0
														}
														portTags = append(portTags, tags.Tag{Key: "link_status", Value: statusStr})
													}

													// Add link status datapoint
													datapoints = append(datapoints, data_store.DataPoint{
														Name:      "network.port.link_up",
														Timestamp: timestamp,
														Value:     linkUp,
														Tags:      portTags,
													})

													// Get port health
													var healthState int
													if status, hasStatus := portDetails["Status"]; hasStatus {
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
														Name:      "network.port.health",
														Timestamp: timestamp,
														Value:     float32(healthState),
														Tags:      portTags,
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

	return datapoints, nil
}

// collectStorageMetrics gathers Cisco-specific storage metrics
func (c *CiscoCollector) collectStorageMetrics(ctx context.Context, timestamp time.Time) ([]data_store.DataPoint, error) {
	// For Cisco UCS, there are two paths to check for storage
	// 1. Standard Redfish path
	// 2. Cisco OEM-specific path for StorageController

	// First get the standard metrics
	datapoints, err := c.GenericCollector.collectStorageMetrics(ctx, timestamp)
	if err != nil {
		// If we get an error, still try the Cisco-specific paths
		c.logger.Warn().
			Err(err).
			Msg("Failed to get standard storage metrics, continuing with Cisco-specific")
		datapoints = []data_store.DataPoint{}
	}

	// Then check for Cisco-specific storage controllers
	if len(c.systems) > 0 {
		// Try Cisco StorageController path
		storageCtrlPath := c.systems[0] + "/Oem/Cisco/StorageController"
		storageResp, err := c.client.GetRaw(ctx, storageCtrlPath)
		if err == nil {
			// Parse the raw response
			var storageData map[string]interface{}
			if err := json.Unmarshal(storageResp, &storageData); err == nil {
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

				// Process storage controllers
				if controllers, hasControllers := storageData["Members"]; hasControllers {
					controllersList, ok := controllers.([]interface{})
					if ok {
						for _, controller := range controllersList {
							controllerData, ok := controller.(map[string]interface{})
							if !ok {
								continue
							}

							// Extract controller URL to get full details
							if controllerId, hasId := controllerData["@odata.id"]; hasId {
								controllerPath := fmt.Sprintf("%v", controllerId)
								controllerResp, err := c.client.GetRaw(ctx, controllerPath)
								if err != nil {
									c.logger.Warn().
										Err(err).
										Str("path", controllerPath).
										Msg("Failed to get controller details")
									continue
								}

								var controllerDetails map[string]interface{}
								if err := json.Unmarshal(controllerResp, &controllerDetails); err != nil {
									continue
								}

								// Get controller name and model
								var controllerName, controllerModel string
								if name, hasName := controllerDetails["Name"]; hasName {
									controllerName = fmt.Sprintf("%v", name)
								} else if id, hasId := controllerDetails["Id"]; hasId {
									controllerName = fmt.Sprintf("Controller %v", id)
								} else {
									controllerName = "Storage Controller"
								}

								if model, hasModel := controllerDetails["Model"]; hasModel {
									controllerModel = fmt.Sprintf("%v", model)
								}

								// Create controller-specific tags
								controllerTags := append([]tags.Tag{}, systemTags...)
								controllerTags = append(controllerTags, tags.Tag{Key: "controller_name", Value: controllerName})
								if controllerModel != "" {
									controllerTags = append(controllerTags, tags.Tag{Key: "model", Value: controllerModel})
								}

								// Get controller health status
								var healthState int
								if status, hasStatus := controllerDetails["Status"]; hasStatus {
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

								// Get physical disks if available
								if physDisks, hasDisks := controllerDetails["PhysicalDrive"]; hasDisks {
									disksObj, ok := physDisks.(map[string]interface{})
									if !ok {
										continue
									}

									if disksPath, hasPath := disksObj["@odata.id"]; hasPath {
										disksURL := fmt.Sprintf("%v", disksPath)
										disksResp, err := c.client.GetRaw(ctx, disksURL)
										if err != nil {
											continue
										}

										var disksCollection map[string]interface{}
										if err := json.Unmarshal(disksResp, &disksCollection); err != nil {
											continue
										}

										// Process each physical disk
										if diskMembers, hasMembers := disksCollection["Members"]; hasMembers {
											disksList, ok := diskMembers.([]interface{})
											if !ok {
												continue
											}

											for _, disk := range disksList {
												diskData, ok := disk.(map[string]interface{})
												if !ok {
													continue
												}

												if diskPath, hasPath := diskData["@odata.id"]; hasPath {
													diskURL := fmt.Sprintf("%v", diskPath)
													diskResp, err := c.client.GetRaw(ctx, diskURL)
													if err != nil {
														continue
													}

													var diskDetails map[string]interface{}
													if err := json.Unmarshal(diskResp, &diskDetails); err != nil {
														continue
													}

													// Get disk name and details
													var diskName, diskModel, diskSerial string
													if name, hasName := diskDetails["Name"]; hasName {
														diskName = fmt.Sprintf("%v", name)
													} else if id, hasId := diskDetails["Id"]; hasId {
														diskName = fmt.Sprintf("Disk %v", id)
													} else {
														diskName = "Physical Disk"
													}

													if model, hasModel := diskDetails["Model"]; hasModel {
														diskModel = fmt.Sprintf("%v", model)
													}

													if serial, hasSerial := diskDetails["SerialNumber"]; hasSerial {
														diskSerial = fmt.Sprintf("%v", serial)
													}

													// Create disk-specific tags
													diskTags := append([]tags.Tag{}, controllerTags...)
													diskTags = append(diskTags, tags.Tag{Key: "drive_name", Value: diskName})
													if diskModel != "" {
														diskTags = append(diskTags, tags.Tag{Key: "model", Value: diskModel})
													}
													if diskSerial != "" {
														diskTags = append(diskTags, tags.Tag{Key: "serial_number", Value: diskSerial})
													}

													// Get disk type
													if mediaType, hasType := diskDetails["MediaType"]; hasType {
														typeStr := fmt.Sprintf("%v", mediaType)
														diskTags = append(diskTags, tags.Tag{Key: "media_type", Value: typeStr})
													}

													// Get disk capacity
													if capacity, hasCapacity := diskDetails["CapacityBytes"]; hasCapacity {
														capBytes, ok := capacity.(float64)
														if ok {
															capacityGB := float32(capBytes) / (1024 * 1024 * 1024)
															datapoints = append(datapoints, data_store.DataPoint{
																Name:      "storage.drive.capacity_gb",
																Timestamp: timestamp,
																Value:     capacityGB,
																Tags:      diskTags,
															})
														}
													}

													// Get disk health
													var healthState int
													if status, hasStatus := diskDetails["Status"]; hasStatus {
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
														Name:      "storage.drive.health",
														Timestamp: timestamp,
														Value:     float32(healthState),
														Tags:      diskTags,
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

	return datapoints, nil
}
