package redfish

import (
	"context"
	"encoding/json"
	"fmt"
	"senhub-agent.go/internal/agent/services/data_store"
	"senhub-agent.go/internal/agent/tags"
	"time"
)

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