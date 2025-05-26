// Package otel provides monitoring capabilities for OpenTelemetry data collection
package otel

import (
	"senhub-agent.go/internal/agent/services/data_store"
	"time"

	"github.com/stretchr/testify/mock"
)

// This file contains mocks for testing the OpenTelemetry probe.
// The tests are currently skipped, but this file is kept for reference.

// MockOtelCollectorFactory is a mock factory for creating collectors
type MockOtelCollectorFactory struct {
	mock.Mock
}

// CreateCollector creates a new OtelCollector
func (m *MockOtelCollectorFactory) CreateCollector(config map[string]interface{}) (OtelCollector, error) {
	args := m.Called(config)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(OtelCollector), args.Error(1)
}

// DetectProtocol detects the protocol from an endpoint
func (m *MockOtelCollectorFactory) DetectProtocol(endpoint string) (ProtocolType, error) {
	args := m.Called(endpoint)
	return args.Get(0).(ProtocolType), args.Error(1)
}

// MockHTTPCollector is a mock implementation for HTTP protocol
type MockHTTPCollector struct {
	MockOtelCollector
}

// GetProtocolType returns the protocol this collector handles
func (m *MockHTTPCollector) GetProtocolType() ProtocolType {
	return ProtocolHTTP
}

// IsSupported checks if telemetry type is supported
func (m *MockHTTPCollector) IsSupported(telemetryType TelemetryType) bool {
	return telemetryType == TelemetryMetrics || telemetryType == TelemetryLogs
}

// GetSupportedTelemetryTypes returns supported telemetry types
func (m *MockHTTPCollector) GetSupportedTelemetryTypes() []TelemetryType {
	return []TelemetryType{TelemetryMetrics, TelemetryLogs}
}

// MockGRPCCollector is a mock implementation for gRPC protocol
type MockGRPCCollector struct {
	MockOtelCollector
}

// GetProtocolType returns the protocol this collector handles
func (m *MockGRPCCollector) GetProtocolType() ProtocolType {
	return ProtocolGRPC
}

// IsSupported checks if telemetry type is supported
func (m *MockGRPCCollector) IsSupported(telemetryType TelemetryType) bool {
	return telemetryType == TelemetryMetrics || telemetryType == TelemetryTraces
}

// GetSupportedTelemetryTypes returns supported telemetry types
func (m *MockGRPCCollector) GetSupportedTelemetryTypes() []TelemetryType {
	return []TelemetryType{TelemetryMetrics, TelemetryTraces}
}

// NewMockHTTPCollector creates a new MockHTTPCollector
func NewMockHTTPCollector() *MockHTTPCollector {
	collector := &MockHTTPCollector{}
	
	// Setup default behaviors
	collector.On("Connect", mock.Anything).Return(nil)
	collector.On("Disconnect", mock.Anything).Return(nil)
	collector.On("CollectTelemetry", mock.Anything, mock.Anything, mock.Anything).Return(
		[]data_store.DataPoint{
			{Name: "mock_metric", Value: 42.0, Timestamp: time.Now()},
		}, nil,
	)
	
	return collector
}

// NewMockGRPCCollector creates a new MockGRPCCollector
func NewMockGRPCCollector() *MockGRPCCollector {
	collector := &MockGRPCCollector{}
	
	// Setup default behaviors
	collector.On("Connect", mock.Anything).Return(nil)
	collector.On("Disconnect", mock.Anything).Return(nil)
	collector.On("CollectTelemetry", mock.Anything, mock.Anything, mock.Anything).Return(
		[]data_store.DataPoint{
			{Name: "mock_metric", Value: 42.0, Timestamp: time.Now()},
		}, nil,
	)
	
	return collector
}