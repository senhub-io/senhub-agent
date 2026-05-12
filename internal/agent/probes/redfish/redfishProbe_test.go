package redfish

import (
	"context"
	"errors"
	"os"
	"senhub-agent.go/internal/agent/services/data_store"
	"senhub-agent.go/internal/agent/services/logger"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
)

// MockRedfishCollector is a mock implementation of the RedfishCollector interface for testing
type MockRedfishCollector struct {
	mock.Mock
}

func (m *MockRedfishCollector) GetVendorType() VendorType {
	args := m.Called()
	return args.Get(0).(VendorType)
}

func (m *MockRedfishCollector) Connect(ctx context.Context) error {
	args := m.Called(ctx)
	return args.Error(0)
}

func (m *MockRedfishCollector) Disconnect(ctx context.Context) error {
	args := m.Called(ctx)
	return args.Error(0)
}

func (m *MockRedfishCollector) CollectMetrics(ctx context.Context, collectionType CollectionType, timestamp time.Time) ([]data_store.DataPoint, error) {
	args := m.Called(ctx, collectionType, timestamp)
	return args.Get(0).([]data_store.DataPoint), args.Error(1)
}

func (m *MockRedfishCollector) IsSupported(collectionType CollectionType) bool {
	args := m.Called(collectionType)
	return args.Bool(0)
}

func (m *MockRedfishCollector) GetSupportedCollections() []CollectionType {
	args := m.Called()
	return args.Get(0).([]CollectionType)
}

// Test cases for NewRedfishProbe
func TestNewRedfishProbe(t *testing.T) {
	// Create a test logger
	testLogger := zerolog.New(os.Stderr)
	loggerPtr := (*logger.Logger)(&testLogger)

	// Define test cases
	tests := []struct {
		name        string
		config      map[string]interface{}
		expectError bool
		errorMsg    string
	}{
		{
			name: "Valid config with all required fields",
			config: map[string]interface{}{
				"endpoint": "https://redfish.example.com",
				"username": "admin",
				"password": "password123",
			},
			expectError: false,
		},
		{
			name: "Valid config with custom interval",
			config: map[string]interface{}{
				"endpoint":   "https://redfish.example.com",
				"username":   "admin",
				"password":   "password123",
				"interval":   600,
				"verify_ssl": false,
			},
			expectError: false,
		},
		{
			name: "Valid config with custom collections",
			config: map[string]interface{}{
				"endpoint":    "https://redfish.example.com",
				"username":    "admin",
				"password":    "password123",
				"collections": []interface{}{"system", "thermal", "power"},
			},
			expectError: false,
		},
		{
			name: "Valid config with custom cache duration",
			config: map[string]interface{}{
				"endpoint":       "https://redfish.example.com",
				"username":       "admin",
				"password":       "password123",
				"cache_duration": 300,
			},
			expectError: false,
		},
		{
			name: "Missing endpoint",
			config: map[string]interface{}{
				"username": "admin",
				"password": "password123",
			},
			expectError: true,
			errorMsg:    "redfish probe requires 'endpoint' configuration",
		},
		{
			name: "Missing username",
			config: map[string]interface{}{
				"endpoint": "https://redfish.example.com",
				"password": "password123",
			},
			expectError: true,
			errorMsg:    "redfish probe requires 'username' configuration",
		},
		{
			name: "Missing password",
			config: map[string]interface{}{
				"endpoint": "https://redfish.example.com",
				"username": "admin",
			},
			expectError: true,
			errorMsg:    "redfish probe requires 'password' configuration",
		},
	}

	// Run the tests
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			probe, err := NewRedfishProbe(tt.config, loggerPtr)

			if tt.expectError {
				assert.Error(t, err)
				assert.Contains(t, err.Error(), tt.errorMsg)
				assert.Nil(t, probe)
			} else {
				assert.NoError(t, err)
				assert.NotNil(t, probe)

				// Assert probe type
				redfishProbe, ok := probe.(*redfishProbe)
				assert.True(t, ok, "Probe should be of type *redfishProbe")

				// Verify the probe configuration
				assert.Equal(t, tt.config["endpoint"], redfishProbe.endpoint)
				assert.Equal(t, tt.config["username"], redfishProbe.username)
				assert.Equal(t, tt.config["password"], redfishProbe.password)

				// Verify default values if not specified in config
				if interval, ok := tt.config["interval"].(int); ok {
					assert.Equal(t, time.Duration(interval)*time.Second, redfishProbe.interval)
				} else {
					assert.Equal(t, 300*time.Second, redfishProbe.interval)
				}

				if verifySSL, ok := tt.config["verify_ssl"].(bool); ok {
					assert.Equal(t, verifySSL, redfishProbe.verifySSL)
				} else {
					assert.True(t, redfishProbe.verifySSL) // Default value should be true
				}

				// Verify collections
				if cfgCollections, ok := tt.config["collections"].([]interface{}); ok {
					assert.Len(t, redfishProbe.collections, len(cfgCollections))
				} else {
					defaultCollections := []CollectionType{
						CollectionSystem,
						CollectionThermal,
						CollectionPower,
						CollectionProcessor,
						CollectionMemory,
						CollectionStorage, // Added storage collection by default for PowerVault metrics
					}
					assert.Equal(t, defaultCollections, redfishProbe.collections)
				}
				assert.NotNil(t, redfishProbe.ctx)
				assert.NotNil(t, redfishProbe.cancelFunc)
			}
		})
	}
}

// Test cases for base probe methods
func TestRedfishProbeBaseMethods(t *testing.T) {
	// Create a test logger
	testLogger := zerolog.New(os.Stderr)
	loggerPtr := (*logger.Logger)(&testLogger)

	config := map[string]interface{}{
		"endpoint": "https://redfish.example.com",
		"username": "admin",
		"password": "password123",
		"interval": 600,
	}

	probe, err := NewRedfishProbe(config, loggerPtr)
	assert.NoError(t, err)
	assert.NotNil(t, probe)

	// Test GetName - BaseProbe inheritance: SetName() and GetName()
	probe.(interface{ SetName(string) }).SetName("redfish")
	assert.Equal(t, "redfish", probe.GetName())

	// Test ShouldStart
	assert.True(t, probe.ShouldStart())

	// Test GetInterval
	assert.Equal(t, 600*time.Second, probe.GetInterval())
}

// Test cases for OnStart method
func TestRedfishProbeOnStart(t *testing.T) {
	// Create a test logger
	testLogger := zerolog.New(os.Stderr)
	loggerPtr := (*logger.Logger)(&testLogger)

	config := map[string]interface{}{
		"endpoint": "https://redfish.example.com",
		"username": "admin",
		"password": "password123",
	}

	t.Run("OnStart with successful connection", func(t *testing.T) {
		probe, err := NewRedfishProbe(config, loggerPtr)
		assert.NoError(t, err)
		assert.NotNil(t, probe)

		// This test relies on the real implementation which would create a real
		// collector and try to connect to a real endpoint.
		// Since we can't easily mock the collector creation, we'll just verify
		// that the OnStart method handles errors correctly when trying to connect.

		// With a non-existent endpoint, we expect the connection to fail
		err = probe.OnStart(make(chan struct{}))
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "failed to connect to Redfish API")
	})
}

// Test cases for OnShutdown method
func TestRedfishProbeOnShutdown(t *testing.T) {
	// Create a test logger
	testLogger := zerolog.New(os.Stderr)
	loggerPtr := (*logger.Logger)(&testLogger)

	config := map[string]interface{}{
		"endpoint": "https://redfish.example.com",
		"username": "admin",
		"password": "password123",
	}

	t.Run("OnShutdown with no collector", func(t *testing.T) {
		probe, err := NewRedfishProbe(config, loggerPtr)
		assert.NoError(t, err)
		assert.NotNil(t, probe)

		// When probe has no collector initialized, OnShutdown should return nil
		err = probe.OnShutdown(context.Background())
		assert.NoError(t, err)
	})

	t.Run("OnShutdown with successful disconnect", func(t *testing.T) {
		probe, err := NewRedfishProbe(config, loggerPtr)
		assert.NoError(t, err)
		redfishProbe := probe.(*redfishProbe)

		// Setup a mock collector
		mockCollector := new(MockRedfishCollector)
		mockCollector.On("Disconnect", mock.Anything).Return(nil)
		redfishProbe.collector = mockCollector

		// OnShutdown should successfully disconnect
		err = redfishProbe.OnShutdown(context.Background())
		assert.NoError(t, err)
		mockCollector.AssertExpectations(t)
	})

	t.Run("OnShutdown with disconnect error", func(t *testing.T) {
		probe, err := NewRedfishProbe(config, loggerPtr)
		assert.NoError(t, err)
		redfishProbe := probe.(*redfishProbe)

		// Setup a mock collector that returns an error
		mockCollector := new(MockRedfishCollector)
		mockCollector.On("Disconnect", mock.Anything).Return(errors.New("disconnect error"))
		redfishProbe.collector = mockCollector

		// OnShutdown should return the error from Disconnect
		err = redfishProbe.OnShutdown(context.Background())
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "disconnect error")
		mockCollector.AssertExpectations(t)
	})
}

func TestRedfishProbeCollect(t *testing.T) {
	testLogger := zerolog.New(os.Stderr)
	loggerPtr := (*logger.Logger)(&testLogger)

	config := map[string]interface{}{
		"endpoint":    "https://redfish.example.com",
		"username":    "admin",
		"password":    "password123",
		"collections": []interface{}{"system", "thermal", "power"},
	}

	t.Run("Collect with no collector initialized", func(t *testing.T) {
		probe, err := NewRedfishProbe(config, loggerPtr)
		assert.NoError(t, err)
		redfishProbe := probe.(*redfishProbe)

		// No collector initialized
		redfishProbe.collector = nil

		datapoints, err := redfishProbe.Collect()
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "collector not initialized")
		assert.Nil(t, datapoints)
	})

	t.Run("Collect with successful collection", func(t *testing.T) {
		probe, err := NewRedfishProbe(config, loggerPtr)
		assert.NoError(t, err)
		redfishProbe := probe.(*redfishProbe)

		mockCollector := new(MockRedfishCollector)
		mockCollector.On("GetVendorType").Return(VendorDell)

		// Mock IsSupported for all collections
		mockCollector.On("IsSupported", CollectionSystem).Return(true)
		mockCollector.On("IsSupported", CollectionThermal).Return(true)
		mockCollector.On("IsSupported", CollectionPower).Return(true)

		// Mock CollectMetrics for all collections
		systemMetrics := []data_store.DataPoint{
			{Name: "system.health", Value: 1.0, Timestamp: time.Now()},
		}
		thermalMetrics := []data_store.DataPoint{
			{Name: "thermal.temp", Value: 50.0, Timestamp: time.Now()},
		}
		powerMetrics := []data_store.DataPoint{
			{Name: "power.watts", Value: 500.0, Timestamp: time.Now()},
		}

		mockCollector.On("CollectMetrics", mock.Anything, CollectionSystem, mock.Anything).Return(systemMetrics, nil)
		mockCollector.On("CollectMetrics", mock.Anything, CollectionThermal, mock.Anything).Return(thermalMetrics, nil)
		mockCollector.On("CollectMetrics", mock.Anything, CollectionPower, mock.Anything).Return(powerMetrics, nil)

		redfishProbe.collector = mockCollector

		datapoints, err := redfishProbe.Collect()
		assert.NoError(t, err)
		assert.NotNil(t, datapoints)
		// Should have metrics from all 3 collections
		assert.GreaterOrEqual(t, len(datapoints), 3)

		mockCollector.AssertExpectations(t)
	})

	t.Run("Collect with unsupported collection", func(t *testing.T) {
		probe, err := NewRedfishProbe(config, loggerPtr)
		assert.NoError(t, err)
		redfishProbe := probe.(*redfishProbe)

		mockCollector := new(MockRedfishCollector)
		mockCollector.On("GetVendorType").Return(VendorGeneric)

		// Mock IsSupported - only system is supported
		mockCollector.On("IsSupported", CollectionSystem).Return(true)
		mockCollector.On("IsSupported", CollectionThermal).Return(false)
		mockCollector.On("IsSupported", CollectionPower).Return(false)

		// Mock CollectMetrics only for system
		systemMetrics := []data_store.DataPoint{
			{Name: "system.health", Value: 1.0, Timestamp: time.Now()},
		}
		mockCollector.On("CollectMetrics", mock.Anything, CollectionSystem, mock.Anything).Return(systemMetrics, nil)

		redfishProbe.collector = mockCollector

		datapoints, err := redfishProbe.Collect()
		assert.NoError(t, err)
		assert.NotNil(t, datapoints)
		// Should only have system metrics
		assert.GreaterOrEqual(t, len(datapoints), 1)

		mockCollector.AssertExpectations(t)
	})

	t.Run("Collect with collection error", func(t *testing.T) {
		probe, err := NewRedfishProbe(config, loggerPtr)
		assert.NoError(t, err)
		redfishProbe := probe.(*redfishProbe)

		mockCollector := new(MockRedfishCollector)
		mockCollector.On("GetVendorType").Return(VendorDell)

		// All collections supported
		mockCollector.On("IsSupported", mock.Anything).Return(true)

		// First collection succeeds
		systemMetrics := []data_store.DataPoint{
			{Name: "system.health", Value: 1.0, Timestamp: time.Now()},
		}
		mockCollector.On("CollectMetrics", mock.Anything, CollectionSystem, mock.Anything).Return(systemMetrics, nil)

		// Second collection fails
		mockCollector.On("CollectMetrics", mock.Anything, CollectionThermal, mock.Anything).Return([]data_store.DataPoint{}, errors.New("thermal collection error"))

		// Third collection succeeds
		powerMetrics := []data_store.DataPoint{
			{Name: "power.watts", Value: 500.0, Timestamp: time.Now()},
		}
		mockCollector.On("CollectMetrics", mock.Anything, CollectionPower, mock.Anything).Return(powerMetrics, nil)

		redfishProbe.collector = mockCollector

		// Collect should continue despite one error
		datapoints, err := redfishProbe.Collect()
		assert.NoError(t, err) // Errors are logged but don't stop collection
		assert.NotNil(t, datapoints)
		// Should have metrics from system and power (thermal failed)
		assert.GreaterOrEqual(t, len(datapoints), 2)

		mockCollector.AssertExpectations(t)
	})
}

func TestRedfishProbeGetTargetStrategies(t *testing.T) {
	testLogger := zerolog.New(os.Stderr)
	loggerPtr := (*logger.Logger)(&testLogger)

	config := map[string]interface{}{
		"endpoint": "https://redfish.example.com",
		"username": "admin",
		"password": "password123",
	}

	probe, err := NewRedfishProbe(config, loggerPtr)
	assert.NoError(t, err)

	redfishProbe := probe.(*redfishProbe)
	strategies := redfishProbe.GetTargetStrategies()

	expected := []string{"senhub", "prtg", "http", "otlp"}
	assert.Equal(t, expected, strategies)
}

func TestRedfishProbeConfigurationValidation(t *testing.T) {
	testLogger := zerolog.New(os.Stderr)
	loggerPtr := (*logger.Logger)(&testLogger)

	tests := []struct {
		name        string
		config      map[string]interface{}
		expectError bool
		description string
	}{
		{
			name: "with verify_ssl true",
			config: map[string]interface{}{
				"endpoint":   "https://redfish.example.com",
				"username":   "admin",
				"password":   "password123",
				"verify_ssl": true,
			},
			expectError: false,
			description: "Should accept verify_ssl=true",
		},
		{
			name: "with large interval",
			config: map[string]interface{}{
				"endpoint": "https://redfish.example.com",
				"username": "admin",
				"password": "password123",
				"interval": 3600, // 1 hour
			},
			expectError: false,
			description: "Should accept large interval",
		},
		{
			name: "with all collection types",
			config: map[string]interface{}{
				"endpoint": "https://redfish.example.com",
				"username": "admin",
				"password": "password123",
				"collections": []interface{}{
					"system", "thermal", "power",
					"processor", "memory", "network", "storage",
				},
			},
			expectError: false,
			description: "Should accept all collection types",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			probe, err := NewRedfishProbe(tt.config, loggerPtr)
			if tt.expectError {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
				assert.NotNil(t, probe)
			}
		})
	}
}
