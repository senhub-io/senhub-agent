// Package redfish provides monitoring capabilities for hardware systems via Redfish API
package redfish

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"senhub-agent.go/internal/agent/services/logger"
	"strings"
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
		if err := c.discoverStorageResources(ctx, storageResp); err != nil {
			return err
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

// discoverStorageResources discovers controllers, volumes, and pools
func (c *StorageCollector) discoverStorageResources(ctx context.Context, storageResp *RedfishResponse) error {
	// Extract storage controllers from members
	c.storageControllers = make([]string, 0, len(storageResp.Members))
	for _, member := range storageResp.Members {
		if id, ok := member["@odata.id"]; ok {
			normalizedPath := strings.TrimPrefix(id, "/redfish/v1/")
			c.storageControllers = append(c.storageControllers, normalizedPath)

			// Discover volumes for this controller
			if err := c.discoverVolumes(ctx, normalizedPath); err != nil {
				c.logger.Warn().Err(err).Str("controller", normalizedPath).Msg("Failed to discover volumes")
			}

			// Discover storage pools for this controller
			if err := c.discoverPools(ctx, normalizedPath); err != nil {
				c.logger.Warn().Err(err).Str("controller", normalizedPath).Msg("Failed to discover pools")
			}
		}
	}

	return nil
}

// discoverVolumes discovers volumes for a controller
func (c *StorageCollector) discoverVolumes(ctx context.Context, controllerPath string) error {
	volumesPath := controllerPath + "/Volumes"
	volumesResp, err := c.client.Get(ctx, volumesPath)
	if err != nil || len(volumesResp.Members) == 0 {
		return err
	}

	for _, volume := range volumesResp.Members {
		if volId, ok := volume["@odata.id"]; ok {
			c.storageVolumes = append(c.storageVolumes, strings.TrimPrefix(volId, "/redfish/v1/"))
		}
	}

	return nil
}

// discoverPools discovers storage pools for a controller
func (c *StorageCollector) discoverPools(ctx context.Context, controllerPath string) error {
	poolsPath := controllerPath + "/StoragePools"
	poolsResp, err := c.client.Get(ctx, poolsPath)
	if err != nil || len(poolsResp.Members) == 0 {
		return err
	}

	for _, pool := range poolsResp.Members {
		if poolId, ok := pool["@odata.id"]; ok {
			poolPath := strings.TrimPrefix(poolId, "/redfish/v1/")

			// Get pool details to filter out disk groups
			if isActualPool, err := c.isActualStoragePool(ctx, poolPath); err != nil {
				c.logger.Warn().Err(err).Str("path", poolPath).Msg("Failed to verify pool type")
				continue
			} else if isActualPool {
				c.storagePools = append(c.storagePools, poolPath)
			}
		}
	}

	return nil
}

// isActualStoragePool checks if a pool is an actual pool and not a disk group
func (c *StorageCollector) isActualStoragePool(ctx context.Context, poolPath string) (bool, error) {
	poolResp, err := c.client.Get(ctx, poolPath)
	if err != nil {
		return false, err
	}

	// Parse pool info to check Description field
	var poolInfo struct {
		Description string `json:"Description"`
		Name        string `json:"Name"`
	}

	if err := json.Unmarshal(poolResp.Raw, &poolInfo); err != nil {
		return false, err
	}

	// Only include actual storage pools, skip disk groups
	isPool := poolInfo.Description != "DiskGroup"

	if isPool {
		c.logger.Debug().
			Str("pool_name", poolInfo.Name).
			Str("description", poolInfo.Description).
			Str("path", poolPath).
			Msg("Added storage pool for monitoring")
	} else {
		c.logger.Debug().
			Str("pool_name", poolInfo.Name).
			Str("description", poolInfo.Description).
			Str("path", poolPath).
			Msg("Skipped disk group (redundant with storage pool)")
	}

	return isPool, nil
}

// GetVendorType returns the vendor type for this collector
func (c *StorageCollector) GetVendorType() VendorType {
	return VendorStorage
}

// IsSupported checks if a specific collection type is supported
func (c *StorageCollector) IsSupported(collectionType CollectionType) bool {
	switch collectionType {
	case CollectionSystem, CollectionThermal, CollectionPower, CollectionProcessor, CollectionMemory, CollectionStorage, CollectionDrives, CollectionNetworkAdapter:
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
		CollectionProcessor,
		CollectionMemory,
		CollectionStorage,
		CollectionDrives,
		CollectionNetworkAdapter,
	}
}

// CollectMetrics implementation is in collect_metrics.go

// roundToTwoDecimals rounds a float32 to 2 decimal places
func roundToTwoDecimals(value float32) float32 {
	return float32(math.Round(float64(value)*100) / 100)
}

// boolToFloat converts bool to float
func boolToFloat(b bool) float64 {
	if b {
		return 1.0
	}
	return 0.0
}
