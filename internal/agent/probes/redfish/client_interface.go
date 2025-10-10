// Package redfish provides monitoring capabilities for hardware systems via Redfish API
package redfish

import (
	"context"
)

// RedfishClientInterface defines the interface for Redfish API clients
type RedfishClientInterface interface {
	// Connect establishes a session with the Redfish API
	Connect(ctx context.Context) error

	// Disconnect closes the session with the Redfish API
	Disconnect(ctx context.Context) error

	// Get performs a GET request to the specified path
	Get(ctx context.Context, path string) (*RedfishResponse, error)

	// GetRaw performs a GET request and returns the raw response body
	GetRaw(ctx context.Context, path string) ([]byte, error)

	// DetectRedfishVersions retrieves the Redfish and schema versions
	DetectRedfishVersions(ctx context.Context) (*RedfishVersionInfo, error)
}
