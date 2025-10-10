// Package redfish provides storage volume metrics collection
package redfish

import (
	"context"
	"encoding/json"
	"fmt"
	"senhub-agent.go/internal/agent/services/data_store"
	"senhub-agent.go/internal/agent/tags"
	"strconv"
	"strings"
	"time"
)

// collectVolumeConsumptionMetrics collects storage consumption and I/O metrics for volumes
func (c *StorageCollector) collectVolumeConsumptionMetrics(ctx context.Context, timestamp time.Time) ([]data_store.DataPoint, error) {
	var datapoints []data_store.DataPoint

	// Get system name to use as the host tag
	hostName := c.getSystemHostName(ctx)

	// Process volumes for each controller
	for _, controllerPath := range c.storageControllers {
		volumeDatapoints, err := c.collectControllerVolumes(ctx, controllerPath, hostName, timestamp)
		if err != nil {
			c.logger.Warn().Err(err).Str("controller", controllerPath).Msg("Failed to collect volumes")
			continue
		}
		datapoints = append(datapoints, volumeDatapoints...)
	}

	return datapoints, nil
}

// getSystemHostName retrieves the system hostname for tagging
func (c *StorageCollector) getSystemHostName(ctx context.Context) string {
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
	return hostName
}

// collectControllerVolumes collects volume metrics for a specific controller
func (c *StorageCollector) collectControllerVolumes(ctx context.Context, controllerPath, hostName string, timestamp time.Time) ([]data_store.DataPoint, error) {
	var datapoints []data_store.DataPoint

	// Get controller details
	ctrlResp, err := c.client.Get(ctx, controllerPath)
	if err != nil {
		return nil, fmt.Errorf("failed to get controller details: %w", err)
	}

	controllerID := ctrlResp.ID
	controllerLetter := extractControllerLetter(controllerID)

	// Process each volume
	volumesPath := controllerPath + "/Volumes"
	volumesResp, err := c.client.Get(ctx, volumesPath)
	if err != nil {
		return datapoints, nil
	}

	for _, member := range volumesResp.Members {
		if volumePath, ok := member["@odata.id"]; ok {
			volumeMetrics, err := c.collectVolumeMetrics(ctx, volumePath, controllerID, controllerLetter, hostName, timestamp)
			if err != nil {
				c.logger.Warn().Err(err).Str("volume", volumePath).Msg("Failed to collect volume metrics")
				continue
			}
			datapoints = append(datapoints, volumeMetrics...)
		}
	}

	return datapoints, nil
}

// extractControllerLetter extracts controller letter (A/B) from controller ID
func extractControllerLetter(controllerID string) string {
	if strings.Contains(strings.ToLower(controllerID), "_a") {
		return "A"
	} else if strings.Contains(strings.ToLower(controllerID), "_b") {
		return "B"
	}
	return ""
}

// collectVolumeMetrics collects all metrics for a single volume
func (c *StorageCollector) collectVolumeMetrics(ctx context.Context, volumePath, controllerID, controllerLetter, hostName string, timestamp time.Time) ([]data_store.DataPoint, error) {
	var datapoints []data_store.DataPoint

	volResp, err := c.client.Get(ctx, volumePath)
	if err != nil {
		return nil, err
	}

	// Parse volume information
	var volumeInfo VolumeInfo
	if err := json.Unmarshal(volResp.Raw, &volumeInfo); err != nil {
		return nil, fmt.Errorf("failed to unmarshal volume data: %w", err)
	}

	// Build volume tags
	volumeTags := c.buildVolumeTags(ctx, volumeInfo, controllerID, controllerLetter, hostName)

	// Collect health metrics
	datapoints = append(datapoints, c.collectVolumeHealthMetrics(volumeInfo, volumeTags, timestamp)...)

	// Collect capacity metrics
	capacityMetrics := c.collectVolumeCapacityMetrics(volumeInfo, volumeTags, timestamp)
	datapoints = append(datapoints, capacityMetrics...)

	// Collect I/O statistics
	ioMetrics := c.collectVolumeIOMetrics(volumeInfo, volumeTags, timestamp)
	datapoints = append(datapoints, ioMetrics...)

	// Process OEM-specific data
	oemMetrics := c.collectVolumeOEMMetrics(ctx, volResp.Raw, volumeInfo, volumeTags, timestamp)
	datapoints = append(datapoints, oemMetrics...)

	return datapoints, nil
}

// VolumeInfo contains volume information structure
type VolumeInfo struct {
	ID                       string   `json:"Id"`
	Name                     string   `json:"Name"`
	CapacityBytes            int64    `json:"CapacityBytes"`
	BlockSizeBytes           int      `json:"BlockSizeBytes"`
	Status                   *Status  `json:"Status"`
	Encrypted                bool     `json:"Encrypted"`
	RemainingCapacityPercent int      `json:"RemainingCapacityPercent"`
	WriteCachePolicy         string   `json:"WriteCachePolicy"`
	AccessCapabilities       []string `json:"AccessCapabilities"`
	EncryptionTypes          []string `json:"EncryptionTypes"`
	RAIDType                 string   `json:"RAIDType"`
	Capacity                 struct {
		Data struct {
			AllocatedBytes int64 `json:"AllocatedBytes"`
			ConsumedBytes  int64 `json:"ConsumedBytes"`
		} `json:"Data"`
	} `json:"Capacity"`
	IOStatistics struct {
		ReadHitIORequests  int64  `json:"ReadHitIORequests"`
		ReadIOKiBytes      int64  `json:"ReadIOKiBytes"`
		WriteHitIORequests int64  `json:"WriteHitIORequests"`
		WriteIOKiBytes     int64  `json:"WriteIOKiBytes"`
		ReadIORequestTime  string `json:"ReadIORequestTime"`
		WriteIORequestTime string `json:"WriteIORequestTime"`
	} `json:"IOStatistics"`
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

// buildVolumeTags constructs comprehensive tags for volume metrics
func (c *StorageCollector) buildVolumeTags(ctx context.Context, volumeInfo VolumeInfo, controllerID, controllerLetter, hostName string) []tags.Tag {
	volumeTags := []tags.Tag{
		{Key: "volume_id", Value: volumeInfo.ID},
		{Key: "volume_name", Value: volumeInfo.Name},
		{Key: "controller_id", Value: controllerID},
	}

	if controllerLetter != "" {
		volumeTags = append(volumeTags, tags.Tag{Key: "controller", Value: controllerLetter})
	}

	if hostName != "" {
		volumeTags = append(volumeTags, tags.Tag{Key: "host", Value: hostName})
	}

	// Add manufacturer/model from controller
	if controllerID != "" {
		volumeTags = c.addControllerInfo(ctx, controllerID, volumeTags)
	}

	// Extract and add pool information
	if poolID := extractPoolID(volumeInfo); poolID != "" {
		volumeTags = append(volumeTags, tags.Tag{Key: "pool_name", Value: poolID})
	}

	// Add volume configuration tags
	if volumeInfo.RAIDType != "" {
		volumeTags = append(volumeTags, tags.Tag{Key: "raid_type", Value: volumeInfo.RAIDType})
	}
	if volumeInfo.WriteCachePolicy != "" {
		volumeTags = append(volumeTags, tags.Tag{Key: "write_cache_policy", Value: volumeInfo.WriteCachePolicy})
	}
	if volumeInfo.BlockSizeBytes > 0 {
		volumeTags = append(volumeTags, tags.Tag{Key: "block_size_bytes", Value: fmt.Sprintf("%d", volumeInfo.BlockSizeBytes)})
	}
	if len(volumeInfo.AccessCapabilities) > 0 {
		volumeTags = append(volumeTags, tags.Tag{Key: "access_capabilities", Value: strings.Join(volumeInfo.AccessCapabilities, ",")})
	}
	if len(volumeInfo.EncryptionTypes) > 0 {
		volumeTags = append(volumeTags, tags.Tag{Key: "encryption_type", Value: strings.Join(volumeInfo.EncryptionTypes, ",")})
	}

	return volumeTags
}

// extractPoolID extracts pool ID from volume capacity sources
func extractPoolID(volumeInfo VolumeInfo) string {
	if len(volumeInfo.CapacitySources) > 0 && len(volumeInfo.CapacitySources[0].ProvidingPools.Members) > 0 {
		poolPath := volumeInfo.CapacitySources[0].ProvidingPools.Members[0].OdataID
		pathParts := strings.Split(poolPath, "/")
		if len(pathParts) > 0 {
			return pathParts[len(pathParts)-1]
		}
	}
	return ""
}

// addControllerInfo adds manufacturer and model information from controller
func (c *StorageCollector) addControllerInfo(ctx context.Context, controllerID string, volumeTags []tags.Tag) []tags.Tag {
	controllerPath := "Storage/" + controllerID
	ctrlResp, err := c.client.Get(ctx, controllerPath)
	if err != nil {
		return volumeTags
	}

	if len(ctrlResp.StorageControllers) > 0 {
		// Use first controller info only
		sc := ctrlResp.StorageControllers[0]
		if mfr, ok := sc["Manufacturer"].(string); ok && mfr != "" {
			volumeTags = append(volumeTags, tags.Tag{Key: "manufacturer", Value: mfr})
		}
		if mod, ok := sc["Model"].(string); ok && mod != "" {
			volumeTags = append(volumeTags, tags.Tag{Key: "model", Value: mod})
		}
	}

	return volumeTags
}

// collectVolumeHealthMetrics collects health and security metrics
func (c *StorageCollector) collectVolumeHealthMetrics(volumeInfo VolumeInfo, volumeTags []tags.Tag, timestamp time.Time) []data_store.DataPoint {
	var datapoints []data_store.DataPoint

	if volumeInfo.Status != nil && volumeInfo.Status.Health != "" {
		datapoints = append(datapoints, data_store.DataPoint{
			Name:      "hardware.storage.volume.health",
			Timestamp: timestamp,
			Value:     float32(mapHealthState(volumeInfo.Status.Health)),
			Tags:      volumeTags,
		})
	}

	datapoints = append(datapoints, data_store.DataPoint{
		Name:      "hardware.storage.volume.encrypted",
		Timestamp: timestamp,
		Value:     float32(boolToFloat(volumeInfo.Encrypted)),
		Tags:      volumeTags,
	})

	return datapoints
}

// collectVolumeCapacityMetrics collects capacity-related metrics
func (c *StorageCollector) collectVolumeCapacityMetrics(volumeInfo VolumeInfo, volumeTags []tags.Tag, timestamp time.Time) []data_store.DataPoint {
	var datapoints []data_store.DataPoint

	c.logger.Debug().
		Str("volume_id", volumeInfo.ID).
		Int64("capacity_bytes", volumeInfo.CapacityBytes).
		Int64("allocated_bytes", volumeInfo.Capacity.Data.AllocatedBytes).
		Int64("consumed_bytes", volumeInfo.Capacity.Data.ConsumedBytes).
		Int("remaining_capacity_percent", volumeInfo.RemainingCapacityPercent).
		Msg("Volume capacity data before calculation")

	// Determine effective capacity
	effectiveCapacity := volumeInfo.CapacityBytes
	if effectiveCapacity == 0 && volumeInfo.Capacity.Data.AllocatedBytes > 0 {
		effectiveCapacity = volumeInfo.Capacity.Data.AllocatedBytes
	}

	if effectiveCapacity > 0 {
		datapoints = append(datapoints, data_store.DataPoint{
			Name:      "hardware.storage.volume.capacity.total",
			Timestamp: timestamp,
			Value:     float32(effectiveCapacity),
			Tags:      volumeTags,
		})

		// Allocated capacity
		if volumeInfo.Capacity.Data.AllocatedBytes > 0 {
			allocatedBytes := float32(volumeInfo.Capacity.Data.AllocatedBytes)
			datapoints = append(datapoints, data_store.DataPoint{
				Name:      "hardware.storage.volume.capacity.allocated",
				Timestamp: timestamp,
				Value:     allocatedBytes,
				Tags:      volumeTags,
			})

			allocatedPercent := roundToTwoDecimals((allocatedBytes / float32(effectiveCapacity)) * 100.0)
			datapoints = append(datapoints, data_store.DataPoint{
				Name:      "hardware.storage.volume.capacity.allocated_percent",
				Timestamp: timestamp,
				Value:     allocatedPercent,
				Tags:      volumeTags,
			})
		}

		// Consumed capacity
		if volumeInfo.Capacity.Data.ConsumedBytes > 0 {
			consumedBytes := float32(volumeInfo.Capacity.Data.ConsumedBytes)
			datapoints = append(datapoints, data_store.DataPoint{
				Name:      "hardware.storage.volume.capacity.used",
				Timestamp: timestamp,
				Value:     consumedBytes,
				Tags:      volumeTags,
			})

			usedPercent := roundToTwoDecimals((consumedBytes / float32(effectiveCapacity)) * 100.0)
			datapoints = append(datapoints, data_store.DataPoint{
				Name:      "hardware.storage.volume.capacity.used_percent",
				Timestamp: timestamp,
				Value:     usedPercent,
				Tags:      volumeTags,
			})
		}

		// Remaining capacity
		if volumeInfo.RemainingCapacityPercent > 0 {
			datapoints = append(datapoints, data_store.DataPoint{
				Name:      "hardware.storage.volume.capacity.free_percent",
				Timestamp: timestamp,
				Value:     roundToTwoDecimals(float32(volumeInfo.RemainingCapacityPercent)),
				Tags:      volumeTags,
			})

			freeBytes := roundToTwoDecimals(float32(effectiveCapacity) * (float32(volumeInfo.RemainingCapacityPercent) / 100.0))
			datapoints = append(datapoints, data_store.DataPoint{
				Name:      "hardware.storage.volume.capacity.free",
				Timestamp: timestamp,
				Value:     freeBytes,
				Tags:      volumeTags,
			})
		}
	}

	return datapoints
}

// collectVolumeIOMetrics collects I/O performance metrics
func (c *StorageCollector) collectVolumeIOMetrics(volumeInfo VolumeInfo, volumeTags []tags.Tag, timestamp time.Time) []data_store.DataPoint {
	var datapoints []data_store.DataPoint

	if volumeInfo.IOStatistics.ReadHitIORequests == 0 && volumeInfo.IOStatistics.WriteHitIORequests == 0 {
		return datapoints
	}

	// Total I/O operations
	totalIO := float32(volumeInfo.IOStatistics.ReadHitIORequests + volumeInfo.IOStatistics.WriteHitIORequests)
	datapoints = append(datapoints, data_store.DataPoint{
		Name:      "hardware.storage.volume.io.total_ops",
		Timestamp: timestamp,
		Value:     totalIO,
		Tags:      volumeTags,
	})

	// Read operations
	datapoints = append(datapoints, data_store.DataPoint{
		Name:      "hardware.storage.volume.io.reads",
		Timestamp: timestamp,
		Value:     float32(volumeInfo.IOStatistics.ReadHitIORequests),
		Tags:      volumeTags,
	})

	// Write operations
	datapoints = append(datapoints, data_store.DataPoint{
		Name:      "hardware.storage.volume.io.writes",
		Timestamp: timestamp,
		Value:     float32(volumeInfo.IOStatistics.WriteHitIORequests),
		Tags:      volumeTags,
	})

	// Total bytes
	if volumeInfo.IOStatistics.ReadIOKiBytes > 0 || volumeInfo.IOStatistics.WriteIOKiBytes > 0 {
		totalBytes := float32((volumeInfo.IOStatistics.ReadIOKiBytes + volumeInfo.IOStatistics.WriteIOKiBytes) * 1024)
		datapoints = append(datapoints, data_store.DataPoint{
			Name:      "hardware.storage.volume.io.total_bytes",
			Timestamp: timestamp,
			Value:     totalBytes,
			Tags:      volumeTags,
		})
	}

	// Read bytes
	if volumeInfo.IOStatistics.ReadIOKiBytes > 0 {
		datapoints = append(datapoints, data_store.DataPoint{
			Name:      "hardware.storage.volume.io.read.bytes",
			Timestamp: timestamp,
			Value:     float32(volumeInfo.IOStatistics.ReadIOKiBytes * 1024),
			Tags:      volumeTags,
		})
	}

	// Write bytes
	if volumeInfo.IOStatistics.WriteIOKiBytes > 0 {
		datapoints = append(datapoints, data_store.DataPoint{
			Name:      "hardware.storage.volume.io.write.bytes",
			Timestamp: timestamp,
			Value:     float32(volumeInfo.IOStatistics.WriteIOKiBytes * 1024),
			Tags:      volumeTags,
		})
	}

	// Read latency
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

	// Write latency
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

	return datapoints
}

// collectVolumeOEMMetrics processes OEM-specific volume data
func (c *StorageCollector) collectVolumeOEMMetrics(ctx context.Context, rawData []byte, volumeInfo VolumeInfo, volumeTags []tags.Tag, timestamp time.Time) []data_store.DataPoint {
	var datapoints []data_store.DataPoint

	var volumeRawData map[string]interface{}
	if err := json.Unmarshal(rawData, &volumeRawData); err != nil {
		return datapoints
	}

	oemData, ok := volumeRawData["Oem"]
	if !ok {
		return datapoints
	}

	oemMap, ok := oemData.(map[string]interface{})
	if !ok {
		return datapoints
	}

	effectiveCapacity := volumeInfo.CapacityBytes
	if effectiveCapacity == 0 && volumeInfo.Capacity.Data.AllocatedBytes > 0 {
		effectiveCapacity = volumeInfo.Capacity.Data.AllocatedBytes
	}

	// Process Dell OEM data
	if dellData, ok := oemMap["Dell"]; ok {
		dellMetrics := c.processVolumeOemDellData(dellData, effectiveCapacity, volumeTags, timestamp)
		datapoints = append(datapoints, dellMetrics...)
	}

	// Process HPE OEM data
	if hpeData, ok := oemMap["Hpe"]; ok {
		hpeMetrics := c.processVolumeOemHpeData(hpeData, effectiveCapacity, volumeTags, timestamp)
		datapoints = append(datapoints, hpeMetrics...)
	}

	return datapoints
}

// processVolumeOemDellData processes Dell OEM volume data
func (c *StorageCollector) processVolumeOemDellData(dellData interface{}, capacityBytes int64, volumeTags []tags.Tag, timestamp time.Time) []data_store.DataPoint {
	var datapoints []data_store.DataPoint

	dellMap, ok := dellData.(map[string]interface{})
	if !ok {
		return datapoints
	}

	// Process UsedBytes
	if usedBytesRaw, ok := dellMap["UsedBytes"]; ok {
		if usedBytes, ok := usedBytesRaw.(float64); ok && usedBytes > 0 {
			datapoints = append(datapoints, data_store.DataPoint{
				Name:      "hardware.storage.volume.capacity.used",
				Timestamp: timestamp,
				Value:     float32(usedBytes),
				Tags:      volumeTags,
			})

			if capacityBytes > 0 {
				usedPercent := roundToTwoDecimals((float32(usedBytes) / float32(capacityBytes)) * 100.0)
				datapoints = append(datapoints, data_store.DataPoint{
					Name:      "hardware.storage.volume.capacity.used_percent",
					Timestamp: timestamp,
					Value:     usedPercent,
					Tags:      volumeTags,
				})
			}
		}
	}

	// Process IO Stats
	if ioStatsRaw, ok := dellMap["IOStats"]; ok {
		if ioStats, ok := ioStatsRaw.(map[string]interface{}); ok {
			ioMetrics := c.processDellIOStats(ioStats, volumeTags, timestamp)
			datapoints = append(datapoints, ioMetrics...)
		}
	}

	return datapoints
}

// processDellIOStats processes Dell I/O statistics
func (c *StorageCollector) processDellIOStats(ioStats map[string]interface{}, volumeTags []tags.Tag, timestamp time.Time) []data_store.DataPoint {
	var datapoints []data_store.DataPoint
	var totalOps, totalBytes float32

	// Read operations
	if readOpsRaw, ok := ioStats["ReadOps"]; ok {
		if readOps, ok := readOpsRaw.(float64); ok {
			datapoints = append(datapoints, data_store.DataPoint{
				Name:      "hardware.storage.volume.io.reads",
				Timestamp: timestamp,
				Value:     float32(readOps),
				Tags:      volumeTags,
			})
			totalOps += float32(readOps)
		}
	}

	// Write operations
	if writeOpsRaw, ok := ioStats["WriteOps"]; ok {
		if writeOps, ok := writeOpsRaw.(float64); ok {
			datapoints = append(datapoints, data_store.DataPoint{
				Name:      "hardware.storage.volume.io.writes",
				Timestamp: timestamp,
				Value:     float32(writeOps),
				Tags:      volumeTags,
			})
			totalOps += float32(writeOps)
		}
	}

	// Read bytes
	if readBytesRaw, ok := ioStats["ReadBytes"]; ok {
		if readBytes, ok := readBytesRaw.(float64); ok {
			datapoints = append(datapoints, data_store.DataPoint{
				Name:      "hardware.storage.volume.io.read.bytes",
				Timestamp: timestamp,
				Value:     float32(readBytes),
				Tags:      volumeTags,
			})
			totalBytes += float32(readBytes)
		}
	}

	// Write bytes
	if writeBytesRaw, ok := ioStats["WriteBytes"]; ok {
		if writeBytes, ok := writeBytesRaw.(float64); ok {
			datapoints = append(datapoints, data_store.DataPoint{
				Name:      "hardware.storage.volume.io.write.bytes",
				Timestamp: timestamp,
				Value:     float32(writeBytes),
				Tags:      volumeTags,
			})
			totalBytes += float32(writeBytes)
		}
	}

	// Total operations
	if totalOps > 0 {
		datapoints = append(datapoints, data_store.DataPoint{
			Name:      "hardware.storage.volume.io.total_ops",
			Timestamp: timestamp,
			Value:     totalOps,
			Tags:      volumeTags,
		})
	}

	// Total bytes
	if totalBytes > 0 {
		datapoints = append(datapoints, data_store.DataPoint{
			Name:      "hardware.storage.volume.io.total_bytes",
			Timestamp: timestamp,
			Value:     totalBytes,
			Tags:      volumeTags,
		})
	}

	// Latency metrics
	if readLatencyRaw, ok := ioStats["ReadLatency"]; ok {
		if readLatency, ok := readLatencyRaw.(float64); ok && readLatency > 0 {
			datapoints = append(datapoints, data_store.DataPoint{
				Name:      "hardware.storage.volume.io.read.latency",
				Timestamp: timestamp,
				Value:     float32(readLatency),
				Tags:      volumeTags,
			})
		}
	}

	if writeLatencyRaw, ok := ioStats["WriteLatency"]; ok {
		if writeLatency, ok := writeLatencyRaw.(float64); ok && writeLatency > 0 {
			datapoints = append(datapoints, data_store.DataPoint{
				Name:      "hardware.storage.volume.io.write.latency",
				Timestamp: timestamp,
				Value:     float32(writeLatency),
				Tags:      volumeTags,
			})
		}
	}

	return datapoints
}

// processVolumeOemHpeData processes HPE OEM volume data
func (c *StorageCollector) processVolumeOemHpeData(hpeData interface{}, capacityBytes int64, volumeTags []tags.Tag, timestamp time.Time) []data_store.DataPoint {
	var datapoints []data_store.DataPoint

	hpeMap, ok := hpeData.(map[string]interface{})
	if !ok {
		return datapoints
	}

	// Process VolumeSpaceInfo
	if spaceInfoRaw, ok := hpeMap["VolumeSpaceInfo"]; ok {
		if spaceInfo, ok := spaceInfoRaw.(map[string]interface{}); ok {
			spaceMetrics := c.processHPESpaceInfo(spaceInfo, volumeTags, timestamp)
			datapoints = append(datapoints, spaceMetrics...)
		}
	}

	// Process IOStatistics
	if statsRaw, ok := hpeMap["IOStatistics"]; ok {
		if stats, ok := statsRaw.(map[string]interface{}); ok {
			ioMetrics := c.processHPEIOStats(stats, volumeTags, timestamp)
			datapoints = append(datapoints, ioMetrics...)
		}
	}

	return datapoints
}

// processHPESpaceInfo processes HPE space information
func (c *StorageCollector) processHPESpaceInfo(spaceInfo map[string]interface{}, volumeTags []tags.Tag, timestamp time.Time) []data_store.DataPoint {
	var datapoints []data_store.DataPoint

	if usedRaw, ok := spaceInfo["UsedSpace"]; ok {
		if usedBytes, ok := usedRaw.(float64); ok && usedBytes > 0 {
			datapoints = append(datapoints, data_store.DataPoint{
				Name:      "hardware.storage.volume.capacity.used",
				Timestamp: timestamp,
				Value:     float32(usedBytes),
				Tags:      volumeTags,
			})
		}
	}

	if allocatedRaw, ok := spaceInfo["AllocatedSpace"]; ok {
		if allocatedBytes, ok := allocatedRaw.(float64); ok && allocatedBytes > 0 {
			datapoints = append(datapoints, data_store.DataPoint{
				Name:      "hardware.storage.volume.capacity.allocated",
				Timestamp: timestamp,
				Value:     float32(allocatedBytes),
				Tags:      volumeTags,
			})
		}
	}

	if reservedRaw, ok := spaceInfo["ReservedSpace"]; ok {
		if reservedBytes, ok := reservedRaw.(float64); ok && reservedBytes > 0 {
			datapoints = append(datapoints, data_store.DataPoint{
				Name:      "hardware.storage.volume.capacity.reserved",
				Timestamp: timestamp,
				Value:     float32(reservedBytes),
				Tags:      volumeTags,
			})
		}
	}

	return datapoints
}

// processHPEIOStats processes HPE I/O statistics
func (c *StorageCollector) processHPEIOStats(stats map[string]interface{}, volumeTags []tags.Tag, timestamp time.Time) []data_store.DataPoint {
	var datapoints []data_store.DataPoint

	if readOpsRaw, ok := stats["ReadIOCount"]; ok {
		if readOps, ok := readOpsRaw.(float64); ok {
			datapoints = append(datapoints, data_store.DataPoint{
				Name:      "hardware.storage.volume.io.reads",
				Timestamp: timestamp,
				Value:     float32(readOps),
				Tags:      volumeTags,
			})
		}
	}

	if readBytesRaw, ok := stats["ReadIOBytes"]; ok {
		if readBytes, ok := readBytesRaw.(float64); ok {
			datapoints = append(datapoints, data_store.DataPoint{
				Name:      "hardware.storage.volume.io.read.bytes",
				Timestamp: timestamp,
				Value:     float32(readBytes),
				Tags:      volumeTags,
			})
		}
	}

	if writeOpsRaw, ok := stats["WriteIOCount"]; ok {
		if writeOps, ok := writeOpsRaw.(float64); ok {
			datapoints = append(datapoints, data_store.DataPoint{
				Name:      "hardware.storage.volume.io.writes",
				Timestamp: timestamp,
				Value:     float32(writeOps),
				Tags:      volumeTags,
			})
		}
	}

	if writeBytesRaw, ok := stats["WriteIOBytes"]; ok {
		if writeBytes, ok := writeBytesRaw.(float64); ok {
			datapoints = append(datapoints, data_store.DataPoint{
				Name:      "hardware.storage.volume.io.write.bytes",
				Timestamp: timestamp,
				Value:     float32(writeBytes),
				Tags:      volumeTags,
			})
		}
	}

	return datapoints
}
