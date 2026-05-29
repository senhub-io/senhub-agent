// Package redfish provides storage controller health metrics collection
package redfish

import (
	"context"
	"encoding/json"
	"fmt"
	"senhub-agent.go/probesdk/datastore"
	"senhub-agent.go/probesdk/tags"
	"time"
)

// collectControllerHealthMetrics collects essential health metrics for storage controllers
// including redundancy status and controller information
func (c *StorageCollector) collectControllerHealthMetrics(ctx context.Context, timestamp time.Time) ([]data_store.DataPoint, error) {
	var datapoints []data_store.DataPoint

	// Get system name for tagging
	hostName := c.getSystemHostName(ctx)

	// Process each controller
	for _, controllerPath := range c.storageControllers {
		controllerMetrics, err := c.collectSingleControllerMetrics(ctx, controllerPath, hostName, timestamp)
		if err != nil {
			c.logger.Warn().Err(err).Str("controller", controllerPath).Msg("Failed to collect controller metrics")
			continue
		}
		datapoints = append(datapoints, controllerMetrics...)
	}

	return datapoints, nil
}

// collectSingleControllerMetrics collects metrics for a single controller
func (c *StorageCollector) collectSingleControllerMetrics(ctx context.Context, controllerPath, hostName string, timestamp time.Time) ([]data_store.DataPoint, error) {
	var datapoints []data_store.DataPoint

	ctrlResp, err := c.client.Get(ctx, controllerPath)
	if err != nil {
		return nil, err
	}

	// Extract controller information
	controllerID := ctrlResp.ID
	controllerName := ctrlResp.Name
	controllerLetter := extractControllerLetter(controllerID)

	// Extract manufacturer, model, and serial
	manufacturer, model, serialNumber := c.extractControllerDetails(ctrlResp)

	// Build controller tags
	controllerTags := c.buildControllerTags(controllerID, controllerName, controllerLetter, hostName, manufacturer, model, serialNumber)

	// Controller health metric
	if ctrlResp.Status != nil && ctrlResp.Status.Health != "" {
		datapoints = append(datapoints, data_store.DataPoint{
			Name:      "hardware.storage.controller.health",
			Timestamp: timestamp,
			Value:     float32(mapHealthState(ctrlResp.Status.Health)),
			Tags:      controllerTags,
		})
	}

	// Process redundancy information
	redundancyMetrics := c.collectRedundancyMetrics(ctrlResp, controllerTags, timestamp)
	datapoints = append(datapoints, redundancyMetrics...)

	return datapoints, nil
}

// extractControllerDetails extracts manufacturer, model, and serial from controller
func (c *StorageCollector) extractControllerDetails(ctrlResp *RedfishResponse) (string, string, string) {
	var manufacturer, model, serialNumber string

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

	return manufacturer, model, serialNumber
}

// buildControllerTags constructs comprehensive tags for controller metrics
func (c *StorageCollector) buildControllerTags(controllerID, controllerName, controllerLetter, hostName, manufacturer, model, serialNumber string) []tags.Tag {
	controllerTags := []tags.Tag{
		{Key: "controller_id", Value: controllerID},
		{Key: "controller_name", Value: controllerName},
		{Key: "controller_type", Value: "storage"},
	}

	if controllerLetter != "" {
		controllerTags = append(controllerTags, tags.Tag{Key: "controller", Value: controllerLetter})
	}

	if hostName != "" {
		controllerTags = append(controllerTags, tags.Tag{Key: "host", Value: hostName})
	}

	if manufacturer != "" {
		controllerTags = append(controllerTags, tags.Tag{Key: "manufacturer", Value: manufacturer})
	}

	if model != "" {
		controllerTags = append(controllerTags, tags.Tag{Key: "model", Value: model})
	}

	if serialNumber != "" {
		controllerTags = append(controllerTags, tags.Tag{Key: "serial_number", Value: serialNumber})
	}

	return controllerTags
}

// collectRedundancyMetrics processes redundancy information for high availability
func (c *StorageCollector) collectRedundancyMetrics(ctrlResp *RedfishResponse, controllerTags []tags.Tag, timestamp time.Time) []data_store.DataPoint {
	var datapoints []data_store.DataPoint

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

	if err := json.Unmarshal(ctrlResp.Raw, &storageData); err != nil {
		return datapoints
	}

	// Process each redundancy group
	for _, redundancy := range storageData.Redundancy {
		if redundancy.Status.Health == "" {
			continue
		}

		// Create tags specific to redundancy group
		redundancyTags := append([]tags.Tag{}, controllerTags...)
		redundancyTags = append(redundancyTags,
			tags.Tag{Key: "redundancy_group", Value: redundancy.Name},
			tags.Tag{Key: "redundancy_mode", Value: redundancy.Mode},
		)

		if redundancy.MinNumNeeded > 0 {
			redundancyTags = append(redundancyTags,
				tags.Tag{Key: "min_controllers_needed", Value: fmt.Sprintf("%d", redundancy.MinNumNeeded)})
		}

		// Redundancy health metric
		datapoints = append(datapoints, data_store.DataPoint{
			Name:      "hardware.storage.redundancy.health",
			Timestamp: timestamp,
			Value:     float32(mapHealthState(redundancy.Status.Health)),
			Tags:      redundancyTags,
		})

		// Maximum controllers supported
		if redundancy.MaxNumSupported > 0 {
			datapoints = append(datapoints, data_store.DataPoint{
				Name:      "hardware.storage.redundancy.controllers_max",
				Timestamp: timestamp,
				Value:     float32(redundancy.MaxNumSupported),
				Tags:      redundancyTags,
			})
		}

		// Minimum controllers needed
		if redundancy.MinNumNeeded > 0 {
			datapoints = append(datapoints, data_store.DataPoint{
				Name:      "hardware.storage.redundancy.controllers_min",
				Timestamp: timestamp,
				Value:     float32(redundancy.MinNumNeeded),
				Tags:      redundancyTags,
			})
		}

		// Active controllers count
		if len(redundancy.RedundancySet) > 0 {
			datapoints = append(datapoints, data_store.DataPoint{
				Name:      "hardware.storage.redundancy.controllers_active",
				Timestamp: timestamp,
				Value:     float32(len(redundancy.RedundancySet)),
				Tags:      redundancyTags,
			})
		}
	}

	return datapoints
}
