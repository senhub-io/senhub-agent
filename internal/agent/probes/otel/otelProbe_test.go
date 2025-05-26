// Package otel provides monitoring capabilities for OpenTelemetry data collection
package otel

import (
	"context"
	"senhub-agent.go/internal/agent/probes/types"
	"senhub-agent.go/internal/agent/services/data_store"
	"senhub-agent.go/internal/agent/services/logger"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
)

// MockOtelCollector is a mock implementation of the OtelCollector interface
type MockOtelCollector struct {
	mock.Mock
}

func (m *MockOtelCollector) GetProtocolType() ProtocolType {
	args := m.Called()
	return args.Get(0).(ProtocolType)
}

func (m *MockOtelCollector) Connect(ctx context.Context) error {
	args := m.Called(ctx)
	return args.Error(0)
}

func (m *MockOtelCollector) Disconnect(ctx context.Context) error {
	args := m.Called(ctx)
	return args.Error(0)
}

func (m *MockOtelCollector) CollectTelemetry(ctx context.Context, telemetryType TelemetryType, timestamp time.Time) ([]data_store.DataPoint, error) {
	args := m.Called(ctx, telemetryType, timestamp)
	return args.Get(0).([]data_store.DataPoint), args.Error(1)
}

func (m *MockOtelCollector) IsSupported(telemetryType TelemetryType) bool {
	args := m.Called(telemetryType)
	return args.Bool(0)
}

func (m *MockOtelCollector) GetSupportedTelemetryTypes() []TelemetryType {
	args := m.Called()
	return args.Get(0).([]TelemetryType)
}

// TestOtelProbeBasicConstruction tests just creation without network calls
func TestOtelProbeBasicConstruction(t *testing.T) {
	t.Skip("Skipping tests until we have proper logger setup in tests")
	
	// Get a test logger
	testLogger := &logger.Logger{}
	
	// Define test config
	config := map[string]interface{}{
		"endpoint": "http://otel.example.com:4318/v1/metrics",
		"protocol": "http",
	}
	
	// Create probe
	probe, err := NewOtelProbe(config, testLogger)
	assert.NoError(t, err)
	assert.NotNil(t, probe)
	
	// Verify it implements the Probe interface
	_, ok := probe.(types.Probe)
	assert.True(t, ok, "Should implement types.Probe")
}