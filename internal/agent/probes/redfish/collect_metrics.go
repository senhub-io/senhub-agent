// Package redfish provides monitoring capabilities for hardware systems via Redfish API
package redfish

import (
	"context"
	"fmt"
	"senhub-agent.go/internal/agent/services/data_store"
	"time"
)

// CollectMetrics gathers metrics for the specified collection type
func (c *StorageCollector) CollectMetrics(ctx context.Context, collectionType CollectionType, timestamp time.Time) ([]data_store.DataPoint, error) {
	switch collectionType {
	case CollectionSystem, CollectionThermal, CollectionPower:
		// Use generic implementation for these
		return c.GenericCollector.CollectMetrics(ctx, collectionType, timestamp)
	case CollectionStorage:
		// Collect comprehensive storage metrics
		var allPoints []data_store.DataPoint
		
		// Collect pool metrics which contain the resource occupation data
		if len(c.storagePools) > 0 {
			poolPoints, err := c.collectPoolMetrics(ctx, timestamp)
			if err != nil {
				c.logger.Warn().Err(err).Msg("Failed to collect pool metrics")
			} else {
				allPoints = append(allPoints, poolPoints...)
			}
		}
		
		// Collect volume metrics for storage usage
		volumePoints, err := c.collectVolumeConsumptionMetrics(ctx, timestamp)
		if err != nil {
			c.logger.Warn().Err(err).Msg("Failed to collect volume consumption metrics")
		} else {
			allPoints = append(allPoints, volumePoints...)
		}
		
		// If we have controllers, collect health information and drive metrics
		if len(c.storageControllers) > 0 {
			// Collect controller health metrics
			controllerPoints, err := c.collectControllerHealthMetrics(ctx, timestamp)
			if err != nil {
				c.logger.Warn().Err(err).Msg("Failed to collect controller health metrics")
			} else {
				allPoints = append(allPoints, controllerPoints...)
			}
			
			// Collect drive metrics including operations in progress
			drivePoints, err := c.collectDriveMetrics(ctx, timestamp)
			if err != nil {
				c.logger.Warn().Err(err).Msg("Failed to collect drive metrics")
			} else {
				allPoints = append(allPoints, drivePoints...)
			}
		}
		
		return allPoints, nil
	case CollectionNetworkAdapter:
		return c.collectNetworkMetrics(ctx, timestamp)
	default:
		return nil, fmt.Errorf("unsupported collection type: %s", collectionType)
	}
}