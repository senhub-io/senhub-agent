package citrix

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"senhub-agent.go/internal/agent/types/datapoint"
	"senhub-agent.go/internal/agent/services/logger"
)

// MockCitrixClient is a mock implementation of CitrixClient for testing
type MockCitrixClient struct {
	mock.Mock
}

func (m *MockCitrixClient) Connect(ctx context.Context) error {
	args := m.Called(ctx)
	return args.Error(0)
}

func (m *MockCitrixClient) Disconnect(ctx context.Context) error {
	args := m.Called(ctx)
	return args.Error(0)
}

func (m *MockCitrixClient) GetSessions(ctx context.Context, sinceTime time.Time) ([]Session, error) {
	args := m.Called(ctx, sinceTime)
	return args.Get(0).([]Session), args.Error(1)
}

func (m *MockCitrixClient) GetMachines(ctx context.Context, sinceTime time.Time) ([]Machine, error) {
	args := m.Called(ctx, sinceTime)
	return args.Get(0).([]Machine), args.Error(1)
}

func (m *MockCitrixClient) GetDesktopGroups(ctx context.Context) ([]DesktopGroup, error) {
	args := m.Called(ctx)
	return args.Get(0).([]DesktopGroup), args.Error(1)
}

func (m *MockCitrixClient) GetConnectionFailureLogs(ctx context.Context, sinceTime time.Time) ([]ConnectionFailureLog, error) {
	args := m.Called(ctx, sinceTime)
	return args.Get(0).([]ConnectionFailureLog), args.Error(1)
}

func (m *MockCitrixClient) GetControllerStatus(ctx context.Context) ([]Controller, error) {
	args := m.Called(ctx)
	return args.Get(0).([]Controller), args.Error(1)
}

func TestNewCitrixProbe(t *testing.T) {
	tests := []struct {
		name    string
		config  map[string]interface{}
		wantErr bool
	}{
		{
			name: "valid configuration",
			config: map[string]interface{}{
				"base_url":    "https://director.example.com/Citrix/Monitor/OData/v4/Data",
				"environment": "PROD",
				"auth": map[string]interface{}{
					"method":   "ntlm",
					"username": "domain\\user",
					"password": "password",
				},
				"tls": map[string]interface{}{
					"verify_ssl": false,
				},
				"collection_interval": 120,
				"timeout":             30,
				"retry": map[string]interface{}{
					"max_attempts":    3,
					"backoff_factor":  2.0,
				},
			},
			wantErr: false,
		},
		{
			name: "missing base_url",
			config: map[string]interface{}{
				"auth": map[string]interface{}{
					"method":   "ntlm",
					"username": "domain\\user",
					"password": "password",
				},
			},
			wantErr: true,
		},
		{
			name: "missing auth configuration",
			config: map[string]interface{}{
				"base_url": "https://director.example.com/Citrix/Monitor/OData/v4/Data",
			},
			wantErr: true,
		},
		{
			name: "missing username",
			config: map[string]interface{}{
				"base_url": "https://director.example.com/Citrix/Monitor/OData/v4/Data",
				"auth": map[string]interface{}{
					"method":   "ntlm",
					"password": "password",
				},
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create a test logger
			baseLogger := &logger.Logger{}

			probe, err := NewCitrixProbe(tt.config, baseLogger)

			if tt.wantErr {
				assert.Error(t, err)
				assert.Nil(t, probe)
			} else {
				assert.NoError(t, err)
				assert.NotNil(t, probe)
				assert.Equal(t, "citrix", probe.GetName())
				assert.True(t, probe.ShouldStart())
			}
		})
	}
}

func TestCitrixProbe_GetInterval(t *testing.T) {
	config := map[string]interface{}{
		"base_url": "https://director.example.com/Citrix/Monitor/OData/v4/Data",
		"interval": 300, // 5 minutes
		"auth": map[string]interface{}{
			"method":   "ntlm",
			"username": "domain\\user",
			"password": "password",
		},
	}

	baseLogger := &logger.Logger{}
	probe, err := NewCitrixProbe(config, baseLogger)

	assert.NoError(t, err)
	assert.Equal(t, 300*time.Second, probe.GetInterval())
}

func TestCitrixProbe_GetTargetStrategies(t *testing.T) {
	config := map[string]interface{}{
		"base_url": "https://director.example.com/Citrix/Monitor/OData/v4/Data",
		"auth": map[string]interface{}{
			"method":   "ntlm",
			"username": "domain\\user",
			"password": "password",
		},
	}

	baseLogger := &logger.Logger{}
	probe, err := NewCitrixProbe(config, baseLogger)

	assert.NoError(t, err)
	
	// Cast to citrixProbe to access GetTargetStrategies method
	citrixProbeImpl := probe.(*citrixProbe)
	expectedStrategies := []string{"senhub", "prtg", "http"}
	assert.Equal(t, expectedStrategies, citrixProbeImpl.GetTargetStrategies())
}

func TestMetricsCollector_CalculateConnectionFailuresMetrics(t *testing.T) {
	baseLogger := &logger.Logger{}
	mockClient := &MockCitrixClient{}
	collector := NewMetricsCollector(mockClient, baseLogger)

	timestamp := time.Now()
	
	// Test data
	failures := []ConnectionFailureLog{
		{
			Id:               "1",
			FailureDate:      timestamp.Add(-1 * time.Minute),
			FailureType:      FailureTypeClientConnection,
			DesktopGroupId:   "dg1",
			DesktopGroupName: "Test Group 1",
			UserName:         "user1",
		},
		{
			Id:               "2",
			FailureDate:      timestamp.Add(-30 * time.Second),
			FailureType:      FailureTypeClientConnection,
			DesktopGroupId:   "dg1",
			DesktopGroupName: "Test Group 1",
			UserName:         "user2",
		},
		{
			Id:               "3",
			FailureDate:      timestamp.Add(-45 * time.Second),
			FailureType:      FailureTypeConfiguration,
			DesktopGroupId:   "dg2",
			DesktopGroupName: "Test Group 2",
			UserName:         "user3",
		},
	}

	desktopGroups := []DesktopGroup{
		{
			DesktopGroupId: "dg1",
			Name:           "Test Group 1",
		},
		{
			DesktopGroupId: "dg2",
			Name:           "Test Group 2",
		},
	}

	dataPoints := collector.calculateConnectionFailuresMetrics(timestamp, failures, desktopGroups)

	// Verify results
	assert.NotEmpty(t, dataPoints)
	
	// Should have total failures metric
	totalMetric := findDataPointByName(dataPoints, "connection_failures_total")
	assert.NotNil(t, totalMetric)
	assert.Equal(t, float32(3.0), totalMetric.Value)

	// Should have failure type metrics
	typeMetrics := findDataPointsByName(dataPoints, "connection_failures_by_type")
	assert.Len(t, typeMetrics, 2) // client_connection_failures and configuration_errors

	// Should have delivery group metrics
	dgMetrics := findDataPointsByName(dataPoints, "connection_failures_by_delivery_group")
	assert.Len(t, dgMetrics, 2) // One for each delivery group failure type combination
}

func TestMetricsCollector_CalculateLogonPerformanceMetrics(t *testing.T) {
	baseLogger := &logger.Logger{}
	mockClient := &MockCitrixClient{}
	collector := NewMetricsCollector(mockClient, baseLogger)

	timestamp := time.Now()
	
	// Test data with valid logon durations
	recentSessions := []Session{
		{
			SessionKey:     "session1",
			UserId:         "user1",
			DesktopGroupId: "dg1",
			LogOnDuration:  15000, // 15 seconds
		},
		{
			SessionKey:     "session2",
			UserId:         "user2",
			DesktopGroupId: "dg1",
			LogOnDuration:  18000, // 18 seconds
		},
		{
			SessionKey:     "session3",
			UserId:         "user3",
			DesktopGroupId: "dg2",
			LogOnDuration:  12000, // 12 seconds
		},
	}

	hourlySessions := append(recentSessions, Session{
		SessionKey:     "session4",
		UserId:         "user4",
		DesktopGroupId: "dg1",
		LogOnDuration:  20000, // 20 seconds
	})

	desktopGroups := []DesktopGroup{
		{
			DesktopGroupId: "dg1",
			Name:           "Test Group 1",
		},
		{
			DesktopGroupId: "dg2",
			Name:           "Test Group 2",
		},
	}

	dataPoints := collector.calculateLogonPerformanceMetrics(timestamp, recentSessions, hourlySessions, desktopGroups)

	// Verify results
	assert.NotEmpty(t, dataPoints)
	
	// Should have current period count
	countMetric := findDataPointByName(dataPoints, "logon_count_current")
	assert.NotNil(t, countMetric)
	assert.Equal(t, float32(3.0), countMetric.Value)

	// Should have average duration metric
	avgMetric := findDataPointByName(dataPoints, "logon_duration_average_ms")
	assert.NotNil(t, avgMetric)
	assert.Equal(t, float32(15000.0), avgMetric.Value) // (15000+18000+12000)/3 = 15000

	// Should have 1-hour rolling average
	hourlyAvgMetric := findDataPointByName(dataPoints, "logon_duration_1h_average_ms")
	assert.NotNil(t, hourlyAvgMetric)
	assert.Equal(t, float32(16250.0), hourlyAvgMetric.Value) // (15000+18000+12000+20000)/4 = 16250
}

// Helper functions for tests

func findDataPointByName(dataPoints []datapoint.DataPoint, name string) *datapoint.DataPoint {
	for _, dp := range dataPoints {
		if dp.Name == name {
			return &dp
		}
	}
	return nil
}

func findDataPointsByName(dataPoints []datapoint.DataPoint, name string) []datapoint.DataPoint {
	var result []datapoint.DataPoint
	for _, dp := range dataPoints {
		if dp.Name == name {
			result = append(result, dp)
		}
	}
	return result
}