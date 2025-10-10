// Package redfish provides monitoring capabilities for hardware systems via Redfish API
package redfish

import (
	"context"
	"encoding/json"
	"fmt"
	"senhub-agent.go/internal/agent/services/data_store"
	"senhub-agent.go/internal/agent/tags"
	"strings"
	"time"
)

// collectDriveMetrics gathers metrics for drives including active operations
func (c *StorageCollector) collectDriveMetrics(ctx context.Context, timestamp time.Time) ([]data_store.DataPoint, error) {
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

	// Process each controller to find drives
	for _, controllerPath := range c.storageControllers {
		// Get controller details
		ctrlResp, err := c.client.Get(ctx, controllerPath)
		if err != nil {
			c.logger.Warn().Err(err).Str("path", controllerPath).Msg("Failed to get controller details")
			continue
		}

		// Extract controller information
		controllerID := ctrlResp.ID
		controllerName := ctrlResp.Name

		// Extract simple controller letter if it looks like "controller_a" -> "A"
		var controllerLetter string
		if strings.Contains(strings.ToLower(controllerID), "_a") {
			controllerLetter = "A"
		} else if strings.Contains(strings.ToLower(controllerID), "_b") {
			controllerLetter = "B"
		}

		// Controller tags for grouping
		controllerTags := []tags.Tag{
			{Key: "controller_id", Value: controllerID},
			{Key: "controller_name", Value: controllerName},
		}
		if controllerLetter != "" {
			controllerTags = append(controllerTags, tags.Tag{Key: "controller", Value: controllerLetter})
		}
		if hostName != "" {
			controllerTags = append(controllerTags, tags.Tag{Key: "host", Value: hostName})
		}

		// Process drives for this controller
		drivesPath := controllerPath + "/Drives"
		drivesResp, err := c.client.Get(ctx, drivesPath)
		if err != nil {
			continue
		}

		// Process each drive
		for _, member := range drivesResp.Members {
			if drivePath, ok := member["@odata.id"]; ok {
				driveResp, err := c.client.Get(ctx, drivePath)
				if err != nil {
					c.logger.Warn().Err(err).Str("path", drivePath).Msg("Failed to get drive details")
					continue
				}

				// Parse the entire response to access all fields
				var driveData map[string]interface{}
				if err := json.Unmarshal(driveResp.Raw, &driveData); err != nil {
					c.logger.Warn().Err(err).Str("path", drivePath).Msg("Failed to parse drive JSON")
					continue
				}

				// Extract basic drive information
				driveID, _ := driveData["Id"].(string)
				driveName, _ := driveData["Name"].(string)
				driveModel, _ := driveData["Model"].(string)
				driveManufacturer, _ := driveData["Manufacturer"].(string)
				driveSerial, _ := driveData["SerialNumber"].(string)
				driveMediaType, _ := driveData["MediaType"].(string)
				driveProtocol, _ := driveData["Protocol"].(string)

				// Drive tags - essential for operations
				driveTags := append([]tags.Tag{}, controllerTags...)
				driveTags = append(driveTags,
					tags.Tag{Key: "drive_id", Value: driveID},
					tags.Tag{Key: "drive_name", Value: driveName},
				)

				if driveModel != "" {
					driveTags = append(driveTags, tags.Tag{Key: "model", Value: driveModel})
				}
				if driveManufacturer != "" {
					driveTags = append(driveTags, tags.Tag{Key: "drive_manufacturer", Value: driveManufacturer})
				}
				if driveSerial != "" {
					driveTags = append(driveTags, tags.Tag{Key: "serial_number", Value: driveSerial})
				}
				if driveMediaType != "" {
					driveTags = append(driveTags, tags.Tag{Key: "media_type", Value: driveMediaType})
				}
				if driveProtocol != "" {
					driveTags = append(driveTags, tags.Tag{Key: "protocol", Value: driveProtocol})
				}

				// Extract physical location information
				if physicalLocation, ok := driveData["PhysicalLocation"].(map[string]interface{}); ok {
					if partLocation, ok := physicalLocation["PartLocation"].(map[string]interface{}); ok {
						if serviceLabel, ok := partLocation["ServiceLabel"].(string); ok && serviceLabel != "" {
							driveTags = append(driveTags, tags.Tag{Key: "service_label", Value: serviceLabel})
						}

						if locationType, ok := partLocation["LocationType"].(string); ok && locationType != "" {
							driveTags = append(driveTags, tags.Tag{Key: "location_type", Value: locationType})
						}

						if locationOrdinalValue, ok := partLocation["LocationOrdinalValue"].(float64); ok {
							driveTags = append(driveTags, tags.Tag{Key: "location_ordinal", Value: fmt.Sprintf("%d", int(locationOrdinalValue))})
						}
					}
				}

				// Health metrics - critical for operations
				if status, ok := driveData["Status"].(map[string]interface{}); ok {
					if health, ok := status["Health"].(string); ok && health != "" {
						datapoints = append(datapoints, data_store.DataPoint{
							Name:      "hardware.storage.drive.health",
							Timestamp: timestamp,
							Value:     float32(mapHealthState(health)),
							Tags:      driveTags,
						})
					}
				}

				// Drive capacity - important for resource planning
				if capacityBytes, ok := driveData["CapacityBytes"].(float64); ok && capacityBytes > 0 {
					datapoints = append(datapoints, data_store.DataPoint{
						Name:      "hardware.storage.drive.capacity.total",
						Timestamp: timestamp,
						Value:     float32(capacityBytes),
						Tags:      driveTags,
					})
				}

				// Failure predicted - critical monitoring metric
				if failurePredicted, ok := driveData["FailurePredicted"].(bool); ok {
					failureValue := 0.0
					if failurePredicted {
						failureValue = 1.0
					}
					datapoints = append(datapoints, data_store.DataPoint{
						Name:      "hardware.storage.drive.failure_predicted",
						Timestamp: timestamp,
						Value:     float32(failureValue),
						Tags:      driveTags,
					})
				}

				// Hotspare type - important for RAID configuration
				if hotspare, ok := driveData["HotspareType"].(string); ok && hotspare != "" {
					driveTags = append(driveTags, tags.Tag{Key: "hotspare_type", Value: hotspare})

					// Add metric for hotspare status (1 if hotspare, 0 if not)
					hotspareValue := 0.0
					if hotspare != "None" {
						hotspareValue = 1.0
					}
					datapoints = append(datapoints, data_store.DataPoint{
						Name:      "hardware.storage.drive.hotspare",
						Timestamp: timestamp,
						Value:     float32(hotspareValue),
						Tags:      driveTags,
					})
				}

				// Operations in progress (rebuild, etc.) - critical for maintenance
				if operations, ok := driveData["Operations"].([]interface{}); ok && len(operations) > 0 {
					// At least one operation is in progress
					datapoints = append(datapoints, data_store.DataPoint{
						Name:      "hardware.storage.drive.has_operations",
						Timestamp: timestamp,
						Value:     1.0, // 1 indicates operations in progress
						Tags:      driveTags,
					})

					// Process each operation
					for i, opRaw := range operations {
						if op, ok := opRaw.(map[string]interface{}); ok {
							// Get operation name
							var opName string
							if opNameValue, ok := op["OperationName"].(string); ok && opNameValue != "" {
								opName = opNameValue
							} else {
								opName = fmt.Sprintf("operation_%d", i)
							}

							// Create operation-specific tags
							opTags := append([]tags.Tag{}, driveTags...)
							opTags = append(opTags, tags.Tag{Key: "operation_name", Value: opName})

							// If there's an associated task, add its link
							if associatedTask, ok := op["AssociatedTask"].(map[string]interface{}); ok {
								if taskID, ok := associatedTask["@odata.id"].(string); ok && taskID != "" {
									opTags = append(opTags, tags.Tag{Key: "associated_task", Value: taskID})
								}
							}

							// Get progress percentage
							if percentComplete, ok := op["PercentageComplete"].(float64); ok {
								datapoints = append(datapoints, data_store.DataPoint{
									Name:      "hardware.storage.drive.operation.progress",
									Timestamp: timestamp,
									Value:     float32(percentComplete),
									Tags:      opTags,
								})
							}
						}
					}
				} else {
					// No operations in progress
					datapoints = append(datapoints, data_store.DataPoint{
						Name:      "hardware.storage.drive.has_operations",
						Timestamp: timestamp,
						Value:     0.0, // 0 indicates no operations
						Tags:      driveTags,
					})
				}
			}
		}
	}

	return datapoints, nil
}
