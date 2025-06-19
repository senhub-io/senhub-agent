// Package redfish provides monitoring capabilities for hardware systems via Redfish API
package redfish

import (
	"context"
	"senhub-agent.go/internal/agent/services/data_store"
	"time"
)

// VendorType represents supported hardware vendors
type VendorType string

// Supported vendor types
const (
	VendorGeneric    VendorType = "generic"
	VendorHPE        VendorType = "hpe"
	VendorDell       VendorType = "dell"
	VendorLenovo     VendorType = "lenovo"
	VendorCisco      VendorType = "cisco"
	VendorSupermicro VendorType = "supermicro"
	VendorFujitsu    VendorType = "fujitsu"
	VendorHuawei     VendorType = "huawei"
	VendorStorage    VendorType = "storage" // For storage systems like Dell PowerVault
)

// CollectionType represents the types of data that can be collected
type CollectionType string

// Available collection types
const (
	CollectionSystem         CollectionType = "system"
	CollectionThermal        CollectionType = "thermal"
	CollectionPower          CollectionType = "power"
	CollectionProcessor      CollectionType = "processor"
	CollectionMemory         CollectionType = "memory"
	CollectionStorage        CollectionType = "storage"
	CollectionDrives         CollectionType = "drives"
	CollectionNetwork        CollectionType = "network"
	CollectionNetworkAdapter CollectionType = "networkadapter"
)

// RedfishCollector defines the interface for vendor-specific collectors
type RedfishCollector interface {
	// GetVendorType returns the vendor this collector handles
	GetVendorType() VendorType

	// Connect establishes a connection to the Redfish API endpoint
	Connect(ctx context.Context) error

	// Disconnect closes the connection
	Disconnect(ctx context.Context) error

	// CollectMetrics gathers metrics for the specified collection type
	CollectMetrics(ctx context.Context, collectionType CollectionType, timestamp time.Time) ([]data_store.DataPoint, error)

	// IsSupported checks if a specific collection type is supported by this vendor
	IsSupported(collectionType CollectionType) bool

	// GetSupportedCollections returns a list of all supported collection types
	GetSupportedCollections() []CollectionType
}
