// Package redfish provides storage pool metrics collection
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

// collectPoolMetrics collects resource consumption metrics for storage pools
func (c *StorageCollector) collectPoolMetrics(ctx context.Context, timestamp time.Time) ([]data_store.DataPoint, error) {
	var datapoints []data_store.DataPoint

	// Get system name for tagging
	hostName := c.getSystemHostName(ctx)

	// Process each storage pool
	for _, poolPath := range c.storagePools {
		poolMetrics, err := c.collectSinglePoolMetrics(ctx, poolPath, hostName, timestamp)
		if err != nil {
			c.logger.Warn().Err(err).Str("pool", poolPath).Msg("Failed to collect pool metrics")
			continue
		}
		datapoints = append(datapoints, poolMetrics...)
	}

	return datapoints, nil
}

// collectSinglePoolMetrics collects metrics for a single storage pool
func (c *StorageCollector) collectSinglePoolMetrics(ctx context.Context, poolPath, hostName string, timestamp time.Time) ([]data_store.DataPoint, error) {
	var datapoints []data_store.DataPoint

	poolResp, err := c.client.Get(ctx, poolPath)
	if err != nil {
		return nil, fmt.Errorf("failed to get pool details: %w", err)
	}

	// Parse pool information
	var poolInfo PoolInfo
	if err := json.Unmarshal(poolResp.Raw, &poolInfo); err != nil {
		return nil, fmt.Errorf("failed to parse pool details: %w", err)
	}

	// Skip DiskGroups (redundant with pools)
	if strings.ToLower(poolInfo.Description) == "diskgroup" {
		c.logger.Debug().
			Str("pool_name", poolInfo.Name).
			Str("description", poolInfo.Description).
			Msg("Skipping DiskGroup metrics (redundant with pool metrics)")
		return datapoints, nil
	}

	// Extract controller information from path
	controllerID, controllerLetter := c.extractControllerFromPath(poolPath)

	// Build pool tags
	poolTags := c.buildPoolTags(ctx, poolInfo, poolPath, controllerID, controllerLetter, hostName)

	// Collect health metrics
	datapoints = append(datapoints, c.collectPoolHealthMetrics(poolInfo, poolTags, timestamp)...)

	// Collect capacity metrics
	capacityMetrics := c.collectPoolCapacityMetrics(poolInfo, poolTags, timestamp)
	datapoints = append(datapoints, capacityMetrics...)

	// Collect I/O metrics
	ioMetrics := c.collectPoolIOMetrics(poolInfo, poolTags, timestamp)
	datapoints = append(datapoints, ioMetrics...)

	return datapoints, nil
}

// PoolInfo contains storage pool information structure
type PoolInfo struct {
	ID                       string   `json:"Id"`
	Name                     string   `json:"Name"`
	Status                   *Status  `json:"Status"`
	Description              string   `json:"Description"`
	MaxBlockSizeBytes        int64    `json:"MaxBlockSizeBytes"`
	CapacityBytes            int64    `json:"CapacityBytes"`
	RemainingCapacityBytes   int64    `json:"RemainingCapacityBytes"`
	AllocatedBytes           int64    `json:"AllocatedBytes"`
	RemainingCapacityPercent int      `json:"RemainingCapacityPercent"`
	SupportedRAIDTypes       []string `json:"SupportedRAIDTypes"`
	Capacity                 struct {
		Data struct {
			AllocatedBytes          int64 `json:"AllocatedBytes"`
			ConsumedBytes           int64 `json:"ConsumedBytes"`
			VolumesAllocatedBytes   int64 `json:"VolumesAllocatedBytes"`
			SnapshotsAllocatedBytes int64 `json:"SnapshotsAllocatedBytes"`
			UnusedBytes             int64 `json:"UnusedBytes"`
			TotalCommittedBytes     int64 `json:"TotalCommittedBytes"`
		} `json:"Data"`
		IsThinProvisioned bool `json:"IsThinProvisioned"`
	} `json:"Capacity"`
	IOStatistics struct {
		ReadHitIORequests  int64  `json:"ReadHitIORequests"`
		ReadIOKiBytes      int64  `json:"ReadIOKiBytes"`
		ReadIORequestTime  string `json:"ReadIORequestTime"`
		WriteHitIORequests int64  `json:"WriteHitIORequests"`
		WriteIOKiBytes     int64  `json:"WriteIOKiBytes"`
		WriteIORequestTime string `json:"WriteIORequestTime"`
	} `json:"IOStatistics"`
	Oem struct {
		Dell struct {
			VolumesBytes            int64 `json:"VolumesBytes"`
			SnapshotsBytes          int64 `json:"SnapshotsBytes"`
			FreeBytes               int64 `json:"FreeBytes"`
			OverCommitBytes         int64 `json:"OverCommitBytes"`
			AllocatedSpaceRemaining int64 `json:"AllocatedSpaceRemaining"`
		} `json:"Dell"`
	} `json:"Oem"`
}

// extractControllerFromPath extracts controller ID and letter from pool path
func (c *StorageCollector) extractControllerFromPath(poolPath string) (string, string) {
	pathParts := strings.Split(poolPath, "/")
	if len(pathParts) < 2 {
		return "", ""
	}

	controllerID := pathParts[1]
	controllerLetter := extractControllerLetter(controllerID)

	return controllerID, controllerLetter
}

// buildPoolTags constructs comprehensive tags for pool metrics
func (c *StorageCollector) buildPoolTags(ctx context.Context, poolInfo PoolInfo, poolPath, controllerID, controllerLetter, hostName string) []tags.Tag {
	poolTags := []tags.Tag{
		{Key: "pool_name", Value: poolInfo.Name},
	}

	if controllerID != "" {
		poolTags = append(poolTags, tags.Tag{Key: "controller_id", Value: controllerID})
	}

	if controllerLetter != "" {
		poolTags = append(poolTags, tags.Tag{Key: "controller", Value: controllerLetter})
	}

	if hostName != "" {
		poolTags = append(poolTags, tags.Tag{Key: "host", Value: hostName})
	}

	// Add manufacturer/model from controller
	if controllerID != "" {
		poolTags = c.addControllerInfo(ctx, controllerID, poolTags)
	}

	if poolInfo.Description != "" {
		poolTags = append(poolTags, tags.Tag{Key: "description", Value: poolInfo.Description})
	}

	if len(poolInfo.SupportedRAIDTypes) > 0 {
		poolTags = append(poolTags, tags.Tag{Key: "supported_raid_types", Value: strings.Join(poolInfo.SupportedRAIDTypes, ",")})
	}

	if poolInfo.MaxBlockSizeBytes > 0 {
		poolTags = append(poolTags, tags.Tag{Key: "max_block_size_bytes", Value: fmt.Sprintf("%d", poolInfo.MaxBlockSizeBytes)})
	}

	thinProvisionedValue := "false"
	if poolInfo.Capacity.IsThinProvisioned {
		thinProvisionedValue = "true"
	}
	poolTags = append(poolTags, tags.Tag{Key: "thin_provisioned", Value: thinProvisionedValue})

	return poolTags
}

// collectPoolHealthMetrics collects health metrics for pool
func (c *StorageCollector) collectPoolHealthMetrics(poolInfo PoolInfo, poolTags []tags.Tag, timestamp time.Time) []data_store.DataPoint {
	var datapoints []data_store.DataPoint

	if poolInfo.Status != nil && poolInfo.Status.Health != "" {
		datapoints = append(datapoints, data_store.DataPoint{
			Name:      "hardware.storage.pool.health",
			Timestamp: timestamp,
			Value:     float32(mapHealthState(poolInfo.Status.Health)),
			Tags:      poolTags,
		})
	}

	return datapoints
}

// collectPoolCapacityMetrics collects capacity-related metrics for pool
func (c *StorageCollector) collectPoolCapacityMetrics(poolInfo PoolInfo, poolTags []tags.Tag, timestamp time.Time) []data_store.DataPoint {
	var datapoints []data_store.DataPoint

	c.logger.Debug().
		Str("pool_id", poolInfo.ID).
		Int64("capacity_bytes", poolInfo.CapacityBytes).
		Int("remaining_capacity_percent", poolInfo.RemainingCapacityPercent).
		Int64("allocated_bytes", poolInfo.Capacity.Data.AllocatedBytes).
		Int64("consumed_bytes", poolInfo.Capacity.Data.ConsumedBytes).
		Msg("Pool capacity data before calculation")

	// Total capacity
	if poolInfo.CapacityBytes > 0 {
		datapoints = append(datapoints, data_store.DataPoint{
			Name:      "hardware.storage.pool.capacity.total",
			Timestamp: timestamp,
			Value:     float32(poolInfo.CapacityBytes),
			Tags:      poolTags,
		})
	}

	// Allocated capacity
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

	// Used capacity calculation for Dell ME storage
	var effectiveCapacity uint64
	if poolInfo.Capacity.Data.AllocatedBytes < 0 {
		effectiveCapacity = 0
	} else {
		effectiveCapacity = uint64(poolInfo.Capacity.Data.AllocatedBytes) // #nosec G115 - negative values handled above
	}

	if effectiveCapacity > 0 && poolInfo.RemainingCapacityPercent >= 0 {
		usedPercent := roundToTwoDecimals(100.0 - float32(poolInfo.RemainingCapacityPercent))
		usedBytes := roundToTwoDecimals(float32(effectiveCapacity) * (usedPercent / 100.0))

		c.logger.Debug().
			Str("pool_id", poolInfo.ID).
			Float32("used_percent", usedPercent).
			Float32("used_bytes", usedBytes).
			Msg("Pool capacity calculation result")

		datapoints = append(datapoints, data_store.DataPoint{
			Name:      "hardware.storage.pool.capacity.used",
			Timestamp: timestamp,
			Value:     usedBytes,
			Tags:      poolTags,
		})

		datapoints = append(datapoints, data_store.DataPoint{
			Name:      "hardware.storage.pool.capacity.used_percent",
			Timestamp: timestamp,
			Value:     usedPercent,
			Tags:      poolTags,
		})
	} else {
		c.logger.Debug().
			Str("pool_id", poolInfo.ID).
			Int64("effective_capacity", int64(effectiveCapacity)).
			Int("remaining_capacity_percent", poolInfo.RemainingCapacityPercent).
			Msg("Pool capacity condition not met - skipping used capacity calculation")
	}

	// Free capacity
	if poolInfo.RemainingCapacityPercent >= 0 {
		datapoints = append(datapoints, data_store.DataPoint{
			Name:      "hardware.storage.pool.capacity.free_percent",
			Timestamp: timestamp,
			Value:     roundToTwoDecimals(float32(poolInfo.RemainingCapacityPercent)),
			Tags:      poolTags,
		})
	}

	if poolInfo.RemainingCapacityBytes > 0 {
		datapoints = append(datapoints, data_store.DataPoint{
			Name:      "hardware.storage.pool.capacity.free",
			Timestamp: timestamp,
			Value:     float32(poolInfo.RemainingCapacityBytes),
			Tags:      poolTags,
		})
	} else if poolInfo.Capacity.Data.UnusedBytes > 0 {
		datapoints = append(datapoints, data_store.DataPoint{
			Name:      "hardware.storage.pool.capacity.free",
			Timestamp: timestamp,
			Value:     float32(poolInfo.Capacity.Data.UnusedBytes),
			Tags:      poolTags,
		})
	} else if poolInfo.Oem.Dell.FreeBytes > 0 {
		datapoints = append(datapoints, data_store.DataPoint{
			Name:      "hardware.storage.pool.capacity.free",
			Timestamp: timestamp,
			Value:     float32(poolInfo.Oem.Dell.FreeBytes),
			Tags:      poolTags,
		})
	}

	// Volumes capacity
	if poolInfo.Capacity.Data.VolumesAllocatedBytes > 0 {
		datapoints = append(datapoints, data_store.DataPoint{
			Name:      "hardware.storage.pool.capacity.volumes",
			Timestamp: timestamp,
			Value:     float32(poolInfo.Capacity.Data.VolumesAllocatedBytes),
			Tags:      poolTags,
		})
	} else if poolInfo.Oem.Dell.VolumesBytes > 0 {
		datapoints = append(datapoints, data_store.DataPoint{
			Name:      "hardware.storage.pool.capacity.volumes",
			Timestamp: timestamp,
			Value:     float32(poolInfo.Oem.Dell.VolumesBytes),
			Tags:      poolTags,
		})
	}

	// Snapshots capacity
	if poolInfo.Capacity.Data.SnapshotsAllocatedBytes > 0 {
		datapoints = append(datapoints, data_store.DataPoint{
			Name:      "hardware.storage.pool.capacity.snapshots",
			Timestamp: timestamp,
			Value:     float32(poolInfo.Capacity.Data.SnapshotsAllocatedBytes),
			Tags:      poolTags,
		})
	} else if poolInfo.Oem.Dell.SnapshotsBytes > 0 {
		datapoints = append(datapoints, data_store.DataPoint{
			Name:      "hardware.storage.pool.capacity.snapshots",
			Timestamp: timestamp,
			Value:     float32(poolInfo.Oem.Dell.SnapshotsBytes),
			Tags:      poolTags,
		})
	}

	// Committed capacity
	if poolInfo.Capacity.Data.TotalCommittedBytes > 0 {
		datapoints = append(datapoints, data_store.DataPoint{
			Name:      "hardware.storage.pool.capacity.committed",
			Timestamp: timestamp,
			Value:     float32(poolInfo.Capacity.Data.TotalCommittedBytes),
			Tags:      poolTags,
		})
	}

	// Overcommit tracking
	if poolInfo.Oem.Dell.OverCommitBytes > 0 {
		datapoints = append(datapoints, data_store.DataPoint{
			Name:      "hardware.storage.pool.capacity.overcommit",
			Timestamp: timestamp,
			Value:     float32(poolInfo.Oem.Dell.OverCommitBytes),
			Tags:      poolTags,
		})
	}

	return datapoints
}

// collectPoolIOMetrics collects I/O performance metrics for pool
func (c *StorageCollector) collectPoolIOMetrics(poolInfo PoolInfo, poolTags []tags.Tag, timestamp time.Time) []data_store.DataPoint {
	var datapoints []data_store.DataPoint

	if poolInfo.IOStatistics.ReadHitIORequests == 0 && poolInfo.IOStatistics.WriteHitIORequests == 0 {
		return datapoints
	}

	// Read operations
	datapoints = append(datapoints, data_store.DataPoint{
		Name:      "hardware.storage.pool.io.reads",
		Timestamp: timestamp,
		Value:     float32(poolInfo.IOStatistics.ReadHitIORequests),
		Tags:      poolTags,
	})

	// Read bytes
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

	// Write bytes
	if poolInfo.IOStatistics.WriteIOKiBytes > 0 {
		datapoints = append(datapoints, data_store.DataPoint{
			Name:      "hardware.storage.pool.io.write.bytes",
			Timestamp: timestamp,
			Value:     float32(poolInfo.IOStatistics.WriteIOKiBytes * 1024),
			Tags:      poolTags,
		})
	}

	return datapoints
}
