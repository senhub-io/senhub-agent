package redfish

import (
	"context"
	"encoding/json"
	"fmt"
	"senhub-agent.go/internal/agent/services/data_store"
	"senhub-agent.go/internal/agent/services/logger"
	"strings"
	"time"
)

// GenericCollector provides a base implementation of the RedfishCollector interface
type GenericCollector struct {
	client         RedfishClientInterface
	vendorType     VendorType
	systems        []string
	chassis        []string
	logger         *logger.Logger
	endpoint       string                 // Configured endpoint URL
	redfishVersion string                 // Redfish API version
	schemaVersions map[string]string      // Schema versions by schema name
	oemVersionData map[string]interface{} // OEM-specific version data
}

// NewGenericCollector creates a new generic Redfish collector
func NewGenericCollector(endpoint, username, password string, logger *logger.Logger, verifySSL bool) (RedfishCollector, error) {
	client, err := NewRedfishClient(endpoint, username, password, logger, verifySSL)
	if err != nil {
		return nil, fmt.Errorf("failed to create Redfish client: %v", err)
	}

	return &GenericCollector{
		client:         client,
		vendorType:     VendorGeneric, // Default to generic, will be updated after detection
		logger:         logger,
		endpoint:       endpoint,
		schemaVersions: make(map[string]string),
		oemVersionData: make(map[string]interface{}),
	}, nil
}

// GetVendorType returns the vendor type this collector handles
func (c *GenericCollector) GetVendorType() VendorType {
	return c.vendorType
}

// Connect establishes a connection to the Redfish API endpoint and discover resources
func (c *GenericCollector) Connect(ctx context.Context) error {
	// Connect to Redfish API
	if err := c.client.Connect(ctx); err != nil {
		return fmt.Errorf("failed to connect to Redfish API: %v", err)
	}

	// Detect Redfish and schema versions
	versionInfo, err := c.client.DetectRedfishVersions(ctx)
	if err != nil {
		c.logger.Warn().
			Err(err).
			Msg("Failed to detect Redfish versions, continuing with limited compatibility")
	} else {
		c.redfishVersion = versionInfo.RedfishVersion
		c.schemaVersions = versionInfo.SchemaVersions
		c.oemVersionData = versionInfo.OemVersions

		c.logger.Info().
			Str("redfish_version", versionInfo.RedfishVersion).
			Interface("schema_versions", versionInfo.SchemaVersions).
			Msg("Detected Redfish specifications")
	}

	// Discover systems
	if err := c.discoverSystems(ctx); err != nil {
		return fmt.Errorf("failed to discover systems: %v", err)
	}

	// Discover chassis
	if err := c.discoverChassis(ctx); err != nil {
		return fmt.Errorf("failed to discover chassis: %v", err)
	}

	// Detect vendor if not already set
	if c.vendorType == VendorGeneric {
		if err := c.detectVendor(ctx); err != nil {
			c.logger.Warn().Err(err).Msg("Failed to detect vendor, using generic implementation")
			// Continue with generic implementation
		}
	}

	return nil
}

// discoverSystems finds available systems in the Redfish API
func (c *GenericCollector) discoverSystems(ctx context.Context) error {
	// Get systems collection
	resp, err := c.client.Get(ctx, "Systems")
	if err != nil {
		return fmt.Errorf("failed to get systems: %v", err)
	}

	// Extract system paths from members
	c.systems = make([]string, 0, len(resp.Members))
	for _, member := range resp.Members {
		if id, ok := member["@odata.id"]; ok {
			// Remove the /redfish/v1/ prefix from the path if it exists
			normalizedPath := strings.TrimPrefix(id, "/redfish/v1/")
			c.systems = append(c.systems, normalizedPath)
		}
	}

	c.logger.Debug().
		Int("count", len(c.systems)).
		Msg("Discovered systems")

	return nil
}

// discoverChassis finds available chassis in the Redfish API
func (c *GenericCollector) discoverChassis(ctx context.Context) error {
	// Get chassis collection
	resp, err := c.client.Get(ctx, "Chassis")
	if err != nil {
		return fmt.Errorf("failed to get chassis: %v", err)
	}

	// Extract chassis paths from members
	c.chassis = make([]string, 0, len(resp.Members))
	for _, member := range resp.Members {
		if id, ok := member["@odata.id"]; ok {
			// Remove the /redfish/v1/ prefix from the path if it exists
			normalizedPath := strings.TrimPrefix(id, "/redfish/v1/")
			c.chassis = append(c.chassis, normalizedPath)
		}
	}

	c.logger.Debug().
		Int("count", len(c.chassis)).
		Msg("Discovered chassis")

	return nil
}

// detectVendor attempts to determine the hardware vendor
func (c *GenericCollector) detectVendor(ctx context.Context) error {
	// Check if this is a storage device first (some don't have systems collection)
	storageResp, err := c.client.Get(ctx, "Storage")
	if err == nil && len(storageResp.Members) > 0 {
		// Check for Seagate/PowerVault specific info
		rootResp, err := c.client.Get(ctx, "")
		if err == nil && rootResp.Oem != nil {
			rawJSON, _ := json.Marshal(rootResp.Oem)
			var oemData map[string]interface{}
			if err := json.Unmarshal(rawJSON, &oemData); err == nil {
				// Check for Seagate or other storage vendors in OEM data
				if _, hasSeagate := oemData["Seagate"]; hasSeagate {
					c.vendorType = VendorStorage
					c.logger.Info().Msg("Detected storage system based on Seagate OEM data")
					return nil
				}
			}
		}
	}

	// If no systems are found, we can't detect the vendor using systems approach
	if len(c.systems) == 0 {
		// Check if we have storage collection instead
		if storageResp != nil && len(storageResp.Members) > 0 {
			c.vendorType = VendorStorage
			c.logger.Info().Msg("Detected storage system based on Storage collection")
			return nil
		}
		return fmt.Errorf("no systems found to detect vendor")
	}

	// Get first system details
	resp, err := c.client.Get(ctx, c.systems[0])
	if err != nil {
		return fmt.Errorf("failed to get system details: %v", err)
	}

	// Try to detect vendor from manufacturer
	if resp.Manufacturer != "" {
		manufacturer := resp.Manufacturer
		switch {
		case containsIgnoreCase(manufacturer, "hp") || containsIgnoreCase(manufacturer, "hewlett packard"):
			c.vendorType = VendorHPE
		case containsIgnoreCase(manufacturer, "dell"):
			// Check if it's a PowerVault storage device
			if containsIgnoreCase(resp.Model, "powervault") || containsIgnoreCase(resp.Model, "me") {
				c.vendorType = VendorStorage
			} else {
				c.vendorType = VendorDell
			}
		case containsIgnoreCase(manufacturer, "lenovo"):
			c.vendorType = VendorLenovo
		case containsIgnoreCase(manufacturer, "cisco"):
			c.vendorType = VendorCisco
		case containsIgnoreCase(manufacturer, "huawei"):
			c.vendorType = VendorHuawei
		case containsIgnoreCase(manufacturer, "fujitsu"):
			c.vendorType = VendorFujitsu
		case containsIgnoreCase(manufacturer, "supermicro"):
			c.vendorType = VendorSupermicro
		}
	}

	// If vendor is still not detected, try OEM data
	if c.vendorType == VendorGeneric && resp.Oem != nil {
		// Check for known OEM keys
		for key := range resp.Oem {
			switch {
			case containsIgnoreCase(key, "hp") || containsIgnoreCase(key, "hpe"):
				c.vendorType = VendorHPE
			case containsIgnoreCase(key, "dell"):
				c.vendorType = VendorDell
			case containsIgnoreCase(key, "lenovo"):
				c.vendorType = VendorLenovo
			case containsIgnoreCase(key, "cisco"):
				c.vendorType = VendorCisco
			case containsIgnoreCase(key, "seagate"):
				c.vendorType = VendorStorage
			}
		}
	}

	c.logger.Info().
		Str("vendor", string(c.vendorType)).
		Str("manufacturer", resp.Manufacturer).
		Msg("Detected vendor")

	return nil
}

// Disconnect closes the connection
func (c *GenericCollector) Disconnect(ctx context.Context) error {
	return c.client.Disconnect(ctx)
}

// CollectMetrics gathers metrics for the specified collection type
func (c *GenericCollector) CollectMetrics(ctx context.Context, collectionType CollectionType, timestamp time.Time) ([]data_store.DataPoint, error) {
	switch collectionType {
	case CollectionSystem:
		return c.collectSystemMetrics(ctx, timestamp)
	case CollectionThermal:
		return c.collectThermalMetrics(ctx, timestamp)
	case CollectionPower:
		return c.collectPowerMetrics(ctx, timestamp)
	case CollectionProcessor:
		return c.collectProcessorMetrics(ctx, timestamp)
	case CollectionMemory:
		return c.collectMemoryMetrics(ctx, timestamp)
	case CollectionStorage:
		return c.collectStorageMetrics(ctx, timestamp)
	case CollectionNetworkAdapter:
		return c.collectNetworkMetrics(ctx, timestamp)
	default:
		return nil, fmt.Errorf("unsupported collection type: %s", collectionType)
	}
}

// IsSupported checks if a specific collection type is supported
func (c *GenericCollector) IsSupported(collectionType CollectionType) bool {
	// Generic implementation supports all basic collections
	switch collectionType {
	case CollectionSystem, CollectionThermal, CollectionPower:
		return true
	case CollectionProcessor, CollectionMemory:
		return true
	case CollectionStorage, CollectionNetworkAdapter:
		return true
	default:
		return false
	}
}

// GetSupportedCollections returns all supported collection types
func (c *GenericCollector) GetSupportedCollections() []CollectionType {
	return []CollectionType{
		CollectionSystem,
		CollectionThermal,
		CollectionPower,
		CollectionProcessor,
		CollectionMemory,
		CollectionStorage,
		CollectionNetworkAdapter,
	}
}
