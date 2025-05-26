// Package otel provides monitoring capabilities for OpenTelemetry data collection
package otel

import (
	"context"
	"senhub-agent.go/internal/agent/services/data_store"
	"time"
)

// TelemetryType represents the types of OpenTelemetry data that can be collected
type TelemetryType string

// Supported telemetry types
const (
	TelemetryMetrics TelemetryType = "metrics"
	TelemetryTraces  TelemetryType = "traces"
	TelemetryLogs    TelemetryType = "logs"
)

// ProtocolType defines the supported protocols for OpenTelemetry collection
type ProtocolType string

// Supported protocol types
const (
	ProtocolHTTP ProtocolType = "http"
	ProtocolGRPC ProtocolType = "grpc"
)

// OtelCollector defines the interface for OpenTelemetry protocol-specific collectors
type OtelCollector interface {
	// GetProtocolType returns the protocol this collector handles
	GetProtocolType() ProtocolType
	
	// Connect establishes a connection to the OpenTelemetry endpoint
	Connect(ctx context.Context) error
	
	// Disconnect closes the connection
	Disconnect(ctx context.Context) error
	
	// CollectTelemetry gathers telemetry data for the specified type
	CollectTelemetry(ctx context.Context, telemetryType TelemetryType, timestamp time.Time) ([]data_store.DataPoint, error)
	
	// IsSupported checks if a specific telemetry type is supported by this collector
	IsSupported(telemetryType TelemetryType) bool
	
	// GetSupportedTelemetryTypes returns a list of all supported telemetry types
	GetSupportedTelemetryTypes() []TelemetryType
}