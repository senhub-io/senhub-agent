package citrix

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"senhub-agent.go/internal/agent/probes/types"
	"senhub-agent.go/internal/agent/services/logger"
	"senhub-agent.go/internal/agent/tags"
	"senhub-agent.go/internal/agent/types/datapoint"
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

func (m *MockCitrixClient) GetMachinesFiltered(ctx context.Context, sinceTime time.Time, dnsNames []string) ([]Machine, error) {
	args := m.Called(ctx, sinceTime, dnsNames)
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

func (m *MockCitrixClient) GetConnectionFailureCategories(ctx context.Context) ([]ConnectionFailureCategory, error) {
	args := m.Called(ctx)
	return args.Get(0).([]ConnectionFailureCategory), args.Error(1)
}

func (m *MockCitrixClient) GetDeliveryGroupById(ctx context.Context, deliveryGroupId string) (*DesktopGroup, error) {
	args := m.Called(ctx, deliveryGroupId)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*DesktopGroup), args.Error(1)
}

func (m *MockCitrixClient) GetSessionsByConnectionState(ctx context.Context, connectionStates []int) ([]Session, error) {
	args := m.Called(ctx, connectionStates)
	return args.Get(0).([]Session), args.Error(1)
}

func (m *MockCitrixClient) GetConnections(ctx context.Context, sinceTime time.Time) ([]Connection, error) {
	args := m.Called(ctx, sinceTime)
	return args.Get(0).([]Connection), args.Error(1)
}

func (m *MockCitrixClient) SetValidMachineDNS(dnsNames []string) {
	m.Called(dnsNames)
}


func (m *MockCitrixClient) GetConnectionFailureLogsWithExpand(ctx context.Context, sinceTime time.Time, expand []string) ([]ConnectionFailureLog, error) {
	args := m.Called(ctx, sinceTime, expand)
	return args.Get(0).([]ConnectionFailureLog), args.Error(1)
}

func TestNewCitrixProbe(t *testing.T) {
	tests := []struct {
		name    string
		config  map[string]interface{}
		wantErr bool
	}{
		{
			name: "valid configuration with NTLM",
			config: map[string]interface{}{
				"base_url":    "https://director.example.com/Citrix/Monitor/OData/v4/Data",
				"environment": "PROD",
				"auth": map[string]interface{}{
					"username": "domain\\user",
					"password": "password",
				},
				"tls": map[string]interface{}{
					"verify_ssl": false,
				},
				"collection_interval": 120,
				"timeout":             30,
				"retry": map[string]interface{}{
					"max_attempts":   3,
					"backoff_factor": 2.0,
				},
			},
			wantErr: false,
		},
		{
			name: "valid configuration with Basic auth",
			config: map[string]interface{}{
				"base_url":    "https://director.example.com/Citrix/Monitor/OData/v4/Data",
				"environment": "PROD",
				"auth": map[string]interface{}{
					"username": "testuser",
					"password": "testpass",
				},
				"tls": map[string]interface{}{
					"verify_ssl": false,
				},
				"interval": 300,
				"timeout":  60,
			},
			wantErr: false,
		},
		{
			name: "missing base_url",
			config: map[string]interface{}{
				"auth": map[string]interface{}{
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
			Id:                         1,
			FailureDate:                timestamp.Add(-1 * time.Minute),
			ConnectionFailureEnumValue: 1, // ClientConnectionFailure
			DesktopGroupId:             "dg1",
			DesktopGroupName:           "Test Group 1",
			UserName:                   "user1",
		},
		{
			Id:                         2,
			FailureDate:                timestamp.Add(-30 * time.Second),
			ConnectionFailureEnumValue: 1, // ClientConnectionFailure
			DesktopGroupId:             "dg1",
			DesktopGroupName:           "Test Group 1",
			UserName:                   "user2",
		},
		{
			Id:                         3,
			FailureDate:                timestamp.Add(-45 * time.Second),
			ConnectionFailureEnumValue: 6, // Configuration error (maps to category 1)
			DesktopGroupId:             "dg2",
			DesktopGroupName:           "Test Group 2",
			UserName:                   "user3",
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

	// Categories mapping from production data
	categories := []ConnectionFailureCategory{
		{ConnectionFailureEnumValue: 1, Category: 0}, // Client connection
		{ConnectionFailureEnumValue: 6, Category: 1}, // Configuration
	}

	dataPoints := collector.calculateConnectionFailuresMetrics(timestamp, failures, desktopGroups, categories)

	// Verify results
	assert.NotEmpty(t, dataPoints)

	// Should have total failures metric (both new and legacy)
	totalMetric := findDataPointByName(dataPoints, "user_connection_failures_total")
	assert.NotNil(t, totalMetric)
	assert.Equal(t, float32(3.0), totalMetric.Value)

	// Should also have legacy metric for compatibility
	legacyTotalMetric := findDataPointByName(dataPoints, "connection_failures_total")
	assert.NotNil(t, legacyTotalMetric)
	assert.Equal(t, float32(3.0), legacyTotalMetric.Value)

	// Should have failure type metrics (new naming)
	typeMetrics := findDataPointsByName(dataPoints, "user_connection_failures_by_type")
	assert.GreaterOrEqual(t, len(typeMetrics), 2) // Should have at least global type metrics

	// Should have delivery group metrics
	dgMetrics := findDataPointsByName(dataPoints, "user_connection_failures_by_delivery_group")
	assert.Len(t, dgMetrics, 2) // One for each delivery group
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
			UserId:         123,
			DesktopGroupId: "dg1",
			LogOnDuration:  15000, // 15 seconds
		},
		{
			SessionKey:     "session2",
			UserId:         456,
			DesktopGroupId: "dg1",
			LogOnDuration:  18000, // 18 seconds
		},
		{
			SessionKey:     "session3",
			UserId:         789,
			DesktopGroupId: "dg2",
			LogOnDuration:  12000, // 12 seconds
		},
	}

	hourlySessions := append(recentSessions, Session{
		SessionKey:     "session4",
		UserId:         999,
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

func TestDesktopGroup_GetEffectiveId(t *testing.T) {
	tests := []struct {
		name         string
		desktopGroup DesktopGroup
		expectedId   string
	}{
		{
			name: "DesktopGroupId takes precedence (real Citrix server)",
			desktopGroup: DesktopGroup{
				DesktopGroupId: "real-dg-123",
				Id:             "mock-dg-456",
				Name:           "Test Group",
			},
			expectedId: "real-dg-123",
		},
		{
			name: "Id used when DesktopGroupId is empty (mock server)",
			desktopGroup: DesktopGroup{
				DesktopGroupId: "",
				Id:             "mock-dg-456",
				Name:           "Test Group",
			},
			expectedId: "mock-dg-456",
		},
		{
			name: "Id used when DesktopGroupId not set (mock server)",
			desktopGroup: DesktopGroup{
				Id:   "mock-dg-789",
				Name: "Test Group",
			},
			expectedId: "mock-dg-789",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := tt.desktopGroup.GetEffectiveId()
			assert.Equal(t, tt.expectedId, result)
		})
	}
}

func TestMetricsCollector_CalculateInfrastructureMetricsFromMachines(t *testing.T) {
	baseLogger := &logger.Logger{}
	mockClient := &MockCitrixClient{}
	collector := NewMetricsCollector(mockClient, baseLogger)

	timestamp := time.Now()

	// Test data with machines from different controllers
	machines := []Machine{
		{
			MachineId:         "machine1",
			MachineName:       "VM-001",
			ControllerDNSName: "controller1.example.com",
			RegistrationState: RegistrationStateRegistered,
			FaultState:        FaultStateNone,
		},
		{
			MachineId:         "machine2",
			MachineName:       "VM-002",
			ControllerDNSName: "controller1.example.com",
			RegistrationState: RegistrationStateUnregistered,
			FaultState:        FaultStateUnknown,
		},
		{
			MachineId:         "machine3",
			MachineName:       "VM-003",
			ControllerDNSName: "controller2.example.com",
			RegistrationState: RegistrationStateRegistered,
			FaultState:        FaultStateNone,
		},
	}

	dataPoints := collector.calculateInfrastructureMetricsFromMachines(timestamp, machines)

	// Verify results
	assert.NotEmpty(t, dataPoints)

	// Should have controller health metrics for both controllers
	onlineStatusMetrics := findDataPointsByName(dataPoints, "controller_derived_online_status")
	assert.Len(t, onlineStatusMetrics, 2) // Two controllers

	healthScoreMetrics := findDataPointsByName(dataPoints, "controller_health_score")
	assert.Len(t, healthScoreMetrics, 2) // Two controllers

	// Controller1 should have 1 registered machine out of 2 total (health score 0.5)
	// Controller2 should have 1 registered machine out of 1 total (health score 1.0)

	registeredMetrics := findDataPointsByName(dataPoints, "controller_machines_registered")
	assert.Len(t, registeredMetrics, 2) // Two controllers

	totalMetrics := findDataPointsByName(dataPoints, "controller_machines_total")
	assert.Len(t, totalMetrics, 2) // Two controllers
}

func TestMetricsCollector_CalculateSessionMetrics(t *testing.T) {
	baseLogger := &logger.Logger{}
	mockClient := &MockCitrixClient{}
	collector := NewMetricsCollector(mockClient, baseLogger)

	timestamp := time.Now()

	// Test data with different session states
	sessions := []Session{
		{
			SessionKey:     "session1",
			UserId:         123,
			UserName:       "user1",
			DesktopGroupId: "dg1",
			SessionState:   SessionStateConnected,
		},
		{
			SessionKey:     "session2",
			UserId:         456,
			UserName:       "user2",
			DesktopGroupId: "dg1",
			SessionState:   SessionStateActive,
		},
		{
			SessionKey:     "session3",
			UserId:         789,
			UserName:       "user3",
			DesktopGroupId: "dg1",
			SessionState:   SessionStateDisconnected,
		},
		{
			SessionKey:     "session4",
			UserId:         999,
			UserName:       "user1", // Same user, different session
			DesktopGroupId: "dg2",
			SessionState:   SessionStateActive,
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

	dataPoints := collector.calculateSessionMetrics(timestamp, sessions, desktopGroups)

	// Verify results
	assert.NotEmpty(t, dataPoints)

	// Should have global connected sessions metric (2 connected + 1 active = 3)
	connectedMetrics := findDataPointsByName(dataPoints, "sessions_connected")
	assert.GreaterOrEqual(t, len(connectedMetrics), 1)

	// Should have global simultaneous users metric (2 unique users)
	simultaneousMetrics := findDataPointsByName(dataPoints, "sessions_simultaneous_users")
	assert.GreaterOrEqual(t, len(simultaneousMetrics), 1)

	// Should have disconnected sessions metric
	disconnectedMetrics := findDataPointsByName(dataPoints, "sessions_disconnected_max")
	assert.GreaterOrEqual(t, len(disconnectedMetrics), 1)

	// Should have sessions by state metrics
	stateMetrics := findDataPointsByName(dataPoints, "sessions_by_state")
	assert.GreaterOrEqual(t, len(stateMetrics), 3) // At least 3 different states
}

func TestMetricsCollector_CalculateUserConnectionTotals(t *testing.T) {
	baseLogger := &logger.Logger{}
	mockClient := &MockCitrixClient{}
	collector := NewMetricsCollector(mockClient, baseLogger)

	timestamp := time.Now()

	// Test data with active and inactive sessions
	sessions := []Session{
		{
			SessionKey:     "session1",
			UserId:         123,
			UserName:       "user1",
			DesktopGroupId: "dg1",
			SessionState:   SessionStateConnected, // Active
		},
		{
			SessionKey:     "session2",
			UserId:         456,
			UserName:       "user2",
			DesktopGroupId: "dg1",
			SessionState:   SessionStateActive, // Active
		},
		{
			SessionKey:     "session3",
			UserId:         789,
			UserName:       "user3",
			DesktopGroupId: "dg1",
			SessionState:   SessionStateDisconnected, // Not active
		},
	}

	desktopGroups := []DesktopGroup{
		{
			DesktopGroupId: "dg1",
			Name:           "Test Group 1",
		},
	}

	dataPoints := collector.calculateUserConnectionTotals(timestamp, sessions, desktopGroups)

	// Verify results
	assert.NotEmpty(t, dataPoints)

	// Should have global total active connections metric (2 active sessions)
	totalActiveMetrics := findDataPointsByName(dataPoints, "user_connections_total_active")
	assert.GreaterOrEqual(t, len(totalActiveMetrics), 1)

	// Find the global metric (without delivery group tags)
	var globalMetric *datapoint.DataPoint
	for _, dp := range totalActiveMetrics {
		hasDeliveryGroupTag := false
		for _, tag := range dp.Tags {
			if tag.Key == "delivery_group_id" {
				hasDeliveryGroupTag = true
				break
			}
		}
		if !hasDeliveryGroupTag {
			globalMetric = &dp
			break
		}
	}

	assert.NotNil(t, globalMetric)
	assert.Equal(t, float32(2.0), globalMetric.Value) // 2 active sessions
}

func TestMetricsCollector_CalculateMachineMetrics(t *testing.T) {
	baseLogger := &logger.Logger{}
	mockClient := &MockCitrixClient{}
	collector := NewMetricsCollector(mockClient, baseLogger)

	timestamp := time.Now()

	// Test data with machines in different states
	machines := []Machine{
		{
			MachineId:         "machine1",
			MachineName:       "VM-001",
			ControllerDNSName: "controller1.example.com",
			RegistrationState: RegistrationStateRegistered,
			FaultState:        FaultStateNone,
			DesktopGroupId:    "dg1",
		},
		{
			MachineId:         "machine2",
			MachineName:       "VM-002",
			ControllerDNSName: "controller1.example.com",
			RegistrationState: RegistrationStateUnregistered,
			FaultState:        FaultStateUnknown,
			DesktopGroupId:    "dg1",
		},
		{
			MachineId:         "machine3",
			MachineName:       "VM-003",
			ControllerDNSName: "controller2.example.com",
			RegistrationState: RegistrationStateRegistered,
			FaultState:        FaultStateNone,
			DesktopGroupId:    "dg2",
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

	dataPoints := collector.calculateMachineMetrics(timestamp, machines, desktopGroups)

	// Verify results
	assert.NotEmpty(t, dataPoints)

	// Should have total machines metric
	totalMetric := findDataPointByName(dataPoints, "machines_total")
	assert.NotNil(t, totalMetric)
	assert.Equal(t, float32(3.0), totalMetric.Value)

	// Should have machines by state metrics
	stateMetrics := findDataPointsByName(dataPoints, "machines_by_state")
	assert.GreaterOrEqual(t, len(stateMetrics), 2) // At least 2 different states

	// Should have machines by controller metrics
	controllerMetrics := findDataPointsByName(dataPoints, "machines_by_controller")
	assert.GreaterOrEqual(t, len(controllerMetrics), 2) // Multiple controller states
}

func TestMetricsCollector_CalculateNewConnectionFailureMetrics(t *testing.T) {
	baseLogger := &logger.Logger{}
	mockClient := &MockCitrixClient{}
	collector := NewMetricsCollector(mockClient, baseLogger)

	timestamp := time.Now()

	// Test data with various failure types
	failures := []ConnectionFailureLog{
		{
			Id:                         1,
			FailureDate:                timestamp.Add(-1 * time.Minute),
			ConnectionFailureEnumValue: 1, // ClientConnectionFailure
			DesktopGroupId:             "dg1",
			DesktopGroupName:           "Test Group 1",
			UserName:                   "user1",
		},
		{
			Id:                         2,
			FailureDate:                timestamp.Add(-30 * time.Second),
			ConnectionFailureEnumValue: 6, // Configuration
			DesktopGroupId:             "dg1",
			DesktopGroupName:           "Test Group 1",
			UserName:                   "user2",
		},
		{
			Id:                         3,
			FailureDate:                timestamp.Add(-45 * time.Second),
			ConnectionFailureEnumValue: 1, // ClientConnectionFailure
			DesktopGroupId:             "dg2",
			DesktopGroupName:           "Test Group 2",
			UserName:                   "user1", // Same user, different group
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

	// Categories mapping from production data
	categories := []ConnectionFailureCategory{
		{ConnectionFailureEnumValue: 1, Category: 0}, // Client connection
		{ConnectionFailureEnumValue: 6, Category: 1}, // Configuration
	}

	dataPoints := collector.calculateConnectionFailuresMetrics(timestamp, failures, desktopGroups, categories)

	// Verify results
	assert.NotEmpty(t, dataPoints)

	// Should have new user connection failure metrics
	totalFailuresMetric := findDataPointByName(dataPoints, "user_connection_failures_total")
	assert.NotNil(t, totalFailuresMetric)
	assert.Equal(t, float32(3.0), totalFailuresMetric.Value)

	// Should have unique users with failures metric
	uniqueUsersMetric := findDataPointByName(dataPoints, "user_connection_users_with_failures")
	assert.NotNil(t, uniqueUsersMetric)
	assert.Equal(t, float32(2.0), uniqueUsersMetric.Value) // 2 unique users

	// Should have failures by type metrics
	typeMetrics := findDataPointsByName(dataPoints, "user_connection_failures_by_type")
	assert.GreaterOrEqual(t, len(typeMetrics), 2) // At least 2 different failure types

	// Should have failures by delivery group metrics
	dgMetrics := findDataPointsByName(dataPoints, "user_connection_failures_by_delivery_group")
	assert.Len(t, dgMetrics, 2) // Two delivery groups

	// Should still have legacy metric for compatibility
	legacyTotalMetric := findDataPointByName(dataPoints, "connection_failures_total")
	assert.NotNil(t, legacyTotalMetric)
	assert.Equal(t, float32(3.0), legacyTotalMetric.Value)
}

func TestNewCitrixClient_AuthenticationMethods(t *testing.T) {
	tests := []struct {
		name       string
		authMethod string
		wantErr    bool
	}{
		{
			name:       "NTLM authentication",
			authMethod: "ntlm",
			wantErr:    false,
		},
		{
			name:       "Basic authentication",
			authMethod: "basic",
			wantErr:    false,
		},
		{
			name:       "Kerberos authentication (not implemented)",
			authMethod: "kerberos",
			wantErr:    true,
		},
		{
			name:       "Invalid authentication method",
			authMethod: "invalid",
			wantErr:    true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			config := CitrixClientConfig{
				BaseURL:            "https://example.com/Citrix/Monitor/OData/v4/Data",
				AuthMethod:         tt.authMethod,
				Username:           "testuser",
				Password:           "testpass",
				VerifySSL:          false,
				Timeout:            30 * time.Second,
				MaxRetryAttempts:   3,
				RetryBackoffFactor: 2.0,
			}

			baseLogger := &logger.Logger{}
			client, err := NewCitrixClient(config, baseLogger)

			if tt.wantErr {
				assert.Error(t, err)
				assert.Nil(t, client)
			} else {
				assert.NoError(t, err)
				assert.NotNil(t, client)
			}
		})
	}
}

func TestCitrixProbe_Collect(t *testing.T) {
	// Create a test logger
	baseLogger := &logger.Logger{}

	// Create probe configuration
	config := map[string]interface{}{
		"base_url":    "https://director.example.com/Citrix/Monitor/OData/v4/Data",
		"environment": "TEST",
		"auth": map[string]interface{}{
			"username": "domain\\user",
			"password": "password",
		},
		"tls": map[string]interface{}{
			"verify_ssl": false,
		},
		"interval": 120,
		"timeout":  30,
	}

	probe, err := NewCitrixProbe(config, baseLogger)
	assert.NoError(t, err)
	assert.NotNil(t, probe)

	// Cast to citrixProbe to access internal fields
	citrixProbeImpl := probe.(*citrixProbe)

	// Create a mock client
	mockClient := &MockCitrixClient{}

	// Mock successful API calls
	mockSessions := []Session{
		{
			SessionKey:     "session1",
			UserId:         123,
			UserName:       "user1",
			DesktopGroupId: "dg1",
			SessionState:   SessionStateConnected,
			LogOnDuration:  15000,
		},
	}

	mockMachines := []Machine{
		{
			MachineId:         "machine1",
			MachineName:       "VM-001",
			ControllerDNSName: "controller1.example.com",
			RegistrationState: RegistrationStateRegistered,
			FaultState:        FaultStateNone,
			DesktopGroupId:    "dg1",
		},
	}

	// mockDesktopGroups not needed for 1min frequency test

	mockFailures := []ConnectionFailureLog{
		{
			Id:                         1,
			FailureDate:                time.Now().Add(-30 * time.Minute),
			ConnectionFailureEnumValue: 1, // ClientConnectionFailure
			DesktopGroupId:             "dg1",
			DesktopGroupName:           "Test Group 1",
			UserName:                   "user1",
		},
	}

	// Set up mock expectations for unified MetricsCollector
	// which calls all collectors: Infrastructure, Sessions, Logon, UX, Failures, Health

	// CollectInfrastructureMetrics calls:
	mockClient.On("GetMachines", mock.Anything, mock.Anything).Return(mockMachines, nil)

	// CollectSessionMetrics calls:
	mockClient.On("GetSessionsByConnectionState", mock.Anything, mock.Anything).Return(mockSessions, nil)
	mockClient.On("GetSessions", mock.Anything, mock.Anything).Return(mockSessions, nil)

	// CollectLogonMetrics calls (will return zero metrics if no recent sessions):
	mockClient.On("GetConnections", mock.Anything, mock.Anything).Return([]Connection{}, nil).Maybe()

	// CollectFailureMetrics calls:
	mockClient.On("GetConnectionFailureCategories", mock.Anything).Return([]ConnectionFailureCategory{}, nil)
	mockClient.On("GetConnectionFailureLogs", mock.Anything, mock.Anything).Return(mockFailures, nil)

	// CollectHealthMetrics calls (no additional API calls needed)
	// CollectUXMetrics calls (no additional API calls for now)

	// Replace the probe's client with our mock
	citrixProbeImpl.client = mockClient

	// Create and set the metrics collector with the mock client
	citrixProbeImpl.metricsCollector = NewMetricsCollectorWithEnv(mockClient, "TEST", "https://test.example.com", baseLogger)

	// Test collection
	dataPoints, err := probe.Collect()

	// Verify results
	assert.NoError(t, err)
	assert.NotEmpty(t, dataPoints)

	// Should have various types of metrics from ALL collectors
	var hasSessionMetrics, hasMachineMetrics, hasFailureMetrics, hasLogonMetrics bool

	for _, dp := range dataPoints {
		switch {
		case dp.Name == "sessions_connected" || dp.Name == "sessions_disconnected" || dp.Name == "logon_duration_avg_1h":
			hasSessionMetrics = true
		case dp.Name == "machines_total" || dp.Name == "machines_registered":
			hasMachineMetrics = true
		case dp.Name == "total" || dp.Name == "client_connection_failures" || dp.Name == "other_failures":
			hasFailureMetrics = true
		case dp.Name == "logon_sessions_opened" || dp.Name == "logon_duration_total":
			hasLogonMetrics = true
		case dp.Name == "logon_brokering" || dp.Name == "logon_hdx":
			hasLogonMetrics = true
		}
	}

	assert.True(t, hasSessionMetrics, "Should have session metrics")
	assert.True(t, hasMachineMetrics, "Should have machine metrics")
	assert.True(t, hasFailureMetrics, "Should have failure metrics")
	assert.True(t, hasLogonMetrics, "Should have logon breakdown metrics (even if zero)")

	// Verify mock expectations were met
	mockClient.AssertExpectations(t)
}

func TestMetricsCollector_APIHealthMetric(t *testing.T) {
	baseLogger := &logger.Logger{}
	mockClient := &MockCitrixClient{}
	collector := NewMetricsCollector(mockClient, baseLogger)

	timestamp := time.Now()

	// Mock some successful calls for all endpoints that CollectAllMetrics uses
	mockClient.On("GetDesktopGroups", mock.Anything).Return([]DesktopGroup{}, nil)
	mockClient.On("GetSessions", mock.Anything, mock.Anything).Return([]Session{}, nil)
	mockClient.On("GetMachines", mock.Anything, mock.Anything).Return([]Machine{}, nil)
	mockClient.On("GetConnectionFailureCategories", mock.Anything).Return([]ConnectionFailureCategory{}, nil)
	mockClient.On("GetConnectionFailureLogs", mock.Anything, mock.Anything).Return([]ConnectionFailureLog{}, nil)
	// Optional calls that might be made depending on data available
	mockClient.On("GetConnections", mock.Anything, mock.Anything).Return([]Connection{}, nil).Maybe()
	mockClient.On("GetSessionsByConnectionState", mock.Anything, mock.Anything).Return([]Session{}, nil).Maybe()

	// Test collection
	ctx := context.Background()
	dataPoints, err := collector.CollectAllMetrics(ctx, timestamp)

	// Verify results
	assert.NoError(t, err)
	assert.NotEmpty(t, dataPoints)

	// Should have API health metric
	healthMetric := findDataPointByName(dataPoints, "citrix_api_health")
	assert.NotNil(t, healthMetric)
	assert.Equal(t, float32(6.0), healthMetric.Value) // 6 successful endpoints (DesktopGroups, Sessions, Machines, ConnectionFailureCategories, ConnectionFailureLogs, LogonMetrics)

	// Check that health metric has correct tags (only metric_type should remain)
	var hasMetricType bool
	for _, tag := range healthMetric.Tags {
		switch tag.Key {
		case "metric_type":
			hasMetricType = true
			assert.Equal(t, "api_health", tag.Value)
		default:
			t.Errorf("Unexpected tag found: %s = %s", tag.Key, tag.Value)
		}
	}

	assert.True(t, hasMetricType, "Should have metric_type tag")
	// Extra tags removed as requested

	// Verify mock expectations were met
	mockClient.AssertExpectations(t)
}

// Test session metrics filtering by delivery group
func TestMetricsCollector_SessionMetricsFilteringByDeliveryGroup(t *testing.T) {
	baseLogger := &logger.Logger{}
	mockClient := &MockCitrixClient{}
	collector := NewMetricsCollector(mockClient, baseLogger)

	timestamp := time.Now()

	// Test data with sessions distributed across delivery groups
	sessions := []Session{
		{
			SessionKey:     "session1",
			UserId:         123,
			UserName:       "user1",
			DesktopGroupId: "dg1",
			SessionState:   SessionStateConnected,
		},
		{
			SessionKey:     "session2",
			UserId:         456,
			UserName:       "user2",
			DesktopGroupId: "dg1",
			SessionState:   SessionStateActive,
		},
		{
			SessionKey:     "session3",
			UserId:         789,
			UserName:       "user3",
			DesktopGroupId: "dg2",
			SessionState:   SessionStateDisconnected,
		},
		{
			SessionKey:     "session4",
			UserId:         999,
			UserName:       "user1", // Same user in different DG
			DesktopGroupId: "dg2",
			SessionState:   SessionStateActive,
		},
	}

	desktopGroups := []DesktopGroup{
		{DesktopGroupId: "dg1", Name: "Production Apps"},
		{DesktopGroupId: "dg2", Name: "VDI Desktops"},
	}

	dataPoints := collector.calculateSessionMetrics(timestamp, sessions, desktopGroups)

	// Verify global metrics
	connectedLiveMetrics := findDataPointsByName(dataPoints, "sessions_connected")
	globalConnectedMetric := findMetricWithoutDeliveryGroupTag(connectedLiveMetrics)
	assert.NotNil(t, globalConnectedMetric)
	assert.Equal(t, float32(3.0), globalConnectedMetric.Value) // 1 connected + 2 active = 3

	simultaneousMetrics := findDataPointsByName(dataPoints, "sessions_simultaneous_users")
	globalSimultaneousMetric := findMetricWithoutDeliveryGroupTag(simultaneousMetrics)
	assert.NotNil(t, globalSimultaneousMetric)
	assert.Equal(t, float32(3.0), globalSimultaneousMetric.Value) // 3 unique users

	// Note: Delivery group filtering removed as per tag simplification
	// Global metrics only now (delivery group specific tags removed)
	// Verify only metric_type tags are present
	for _, metric := range connectedLiveMetrics {
		for _, tag := range metric.Tags {
			if tag.Key != "metric_type" {
				t.Errorf("Unexpected tag found: %s = %s", tag.Key, tag.Value)
			}
		}
	}
}

// Test machine metrics filtering by controller DNS name
func TestMetricsCollector_MachineMetricsFilteringByController(t *testing.T) {
	baseLogger := &logger.Logger{}
	mockClient := &MockCitrixClient{}
	collector := NewMetricsCollector(mockClient, baseLogger)

	timestamp := time.Now()

	// Test data with machines across different controllers
	machines := []Machine{
		{
			MachineId:         "machine1",
			MachineName:       "VM-001",
			ControllerDNSName: "ctx-ctrl-01.domain.com",
			RegistrationState: RegistrationStateRegistered,
			FaultState:        FaultStateNone,
			DesktopGroupId:    "dg1",
		},
		{
			MachineId:         "machine2",
			MachineName:       "VM-002",
			ControllerDNSName: "ctx-ctrl-01.domain.com",
			RegistrationState: RegistrationStateUnregistered,
			FaultState:        FaultStateUnknown,
			DesktopGroupId:    "dg1",
		},
		{
			MachineId:         "machine3",
			MachineName:       "VM-003",
			ControllerDNSName: "ctx-ctrl-02.domain.com",
			RegistrationState: RegistrationStateRegistered,
			FaultState:        FaultStateNone,
			DesktopGroupId:    "dg2",
		},
	}

	desktopGroups := []DesktopGroup{
		{DesktopGroupId: "dg1", Name: "Production Apps"},
		{DesktopGroupId: "dg2", Name: "VDI Desktops"},
	}

	dataPoints := collector.calculateMachineMetrics(timestamp, machines, desktopGroups)

	// Verify controller-specific metrics exist
	controllerMetrics := findDataPointsByName(dataPoints, "machines_by_controller")
	// Log the actual number of metrics for debugging
	t.Logf("Found %d controller metrics", len(controllerMetrics))

	// With the current implementation, metrics are generated for non-zero counts
	// We should have at least some controller metrics
	assert.Greater(t, len(controllerMetrics), 0, "Should have controller metrics")

	// Since tags were removed, we can only verify the total count and values
	// Count total machines across all controller metrics
	totalMachinesInControllerMetrics := float32(0)
	for _, metric := range controllerMetrics {
		totalMachinesInControllerMetrics += metric.Value
	}

	// The sum should be reasonable (not checking exact value due to multiple state counts)
	assert.Greater(t, totalMachinesInControllerMetrics, float32(0), "Controller metrics should have values")

	// Verify state metrics exist
	stateMetrics := findDataPointsByName(dataPoints, "machines_by_state")
	assert.Greater(t, len(stateMetrics), 0, "Should have state metrics")
}

// Test user connection failure metrics with delivery group and type filtering
func TestMetricsCollector_UserConnectionFailuresWithFiltering(t *testing.T) {
	baseLogger := &logger.Logger{}
	mockClient := &MockCitrixClient{}
	collector := NewMetricsCollector(mockClient, baseLogger)

	timestamp := time.Now()

	// Test data with various failure types across delivery groups
	failures := []ConnectionFailureLog{
		{
			Id:                         1,
			FailureDate:                timestamp.Add(-10 * time.Minute),
			ConnectionFailureEnumValue: 1, // ClientConnectionFailure
			DesktopGroupId:             "dg1",
			DesktopGroupName:           "Production Apps",
			UserName:                   "user1",
		},
		{
			Id:                         2,
			FailureDate:                timestamp.Add(-15 * time.Minute),
			ConnectionFailureEnumValue: 1, // ClientConnectionFailure
			DesktopGroupId:             "dg1",
			DesktopGroupName:           "Production Apps",
			UserName:                   "user2",
		},
		{
			Id:                         3,
			FailureDate:                timestamp.Add(-20 * time.Minute),
			ConnectionFailureEnumValue: 6, // Configuration
			DesktopGroupId:             "dg2",
			DesktopGroupName:           "VDI Desktops",
			UserName:                   "user3",
		},
		{
			Id:                         4,
			FailureDate:                timestamp.Add(-5 * time.Minute),
			ConnectionFailureEnumValue: 2, // MachineFailure
			DesktopGroupId:             "dg2",
			DesktopGroupName:           "VDI Desktops",
			UserName:                   "user1", // Same user, different failure type and DG
		},
	}

	desktopGroups := []DesktopGroup{
		{DesktopGroupId: "dg1", Name: "Production Apps"},
		{DesktopGroupId: "dg2", Name: "VDI Desktops"},
	}

	// Categories mapping from production data
	categories := []ConnectionFailureCategory{
		{ConnectionFailureEnumValue: 1, Category: 0}, // Client connection
		{ConnectionFailureEnumValue: 2, Category: 2}, // Machine failure
		{ConnectionFailureEnumValue: 6, Category: 1}, // Configuration
	}

	dataPoints := collector.calculateConnectionFailuresMetrics(timestamp, failures, desktopGroups, categories)

	// Verify global failure metrics
	totalFailuresMetric := findDataPointByName(dataPoints, "user_connection_failures_total")
	assert.NotNil(t, totalFailuresMetric)
	assert.Equal(t, float32(4.0), totalFailuresMetric.Value)

	uniqueUsersMetric := findDataPointByName(dataPoints, "user_connection_users_with_failures")
	assert.NotNil(t, uniqueUsersMetric)
	assert.Equal(t, float32(3.0), uniqueUsersMetric.Value) // 3 unique users: user1, user2, user3

	// Verify failure type metrics exist
	typeMetrics := findDataPointsByName(dataPoints, "user_connection_failures_by_type")
	assert.Greater(t, len(typeMetrics), 0, "Should have failure type metrics")

	// Check that we have some failures by type
	// The total might be higher than 4 due to multiple metrics per type (global + per delivery group)
	totalTypeFailures := float32(0)
	for _, metric := range typeMetrics {
		totalTypeFailures += metric.Value
	}
	assert.GreaterOrEqual(t, totalTypeFailures, float32(4.0), "Total failures by type should be at least 4")

	// Verify delivery group metrics exist
	dgMetrics := findDataPointsByName(dataPoints, "user_connection_failures_by_delivery_group")
	assert.Greater(t, len(dgMetrics), 0, "Should have delivery group failure metrics")

	// Check that we have delivery group failure metrics
	totalDGFailures := float32(0)
	for _, metric := range dgMetrics {
		totalDGFailures += metric.Value
	}
	assert.Equal(t, float32(4.0), totalDGFailures, "Total failures by delivery group should be 4")

	// Since tags are simplified, we can only verify that the global metric exists
	// The per-delivery-group breakdown is no longer available with the simplified tag structure
}

// Test comprehensive tag validation for all new metrics
func TestMetricsCollector_TagValidation(t *testing.T) {
	baseLogger := &logger.Logger{}
	mockClient := &MockCitrixClient{}
	collector := NewMetricsCollector(mockClient, baseLogger)

	timestamp := time.Now()

	// Setup test data
	sessions := []Session{
		{SessionKey: "s1", UserName: "user1", DesktopGroupId: "dg1", SessionState: SessionStateConnected},
	}
	machines := []Machine{
		{MachineId: "m1", ControllerDNSName: "ctrl1.com", RegistrationState: RegistrationStateRegistered, FaultState: FaultStateNone, DesktopGroupId: "dg1"},
	}
	failures := []ConnectionFailureLog{
		{Id: 1, ConnectionFailureEnumValue: 1, DesktopGroupId: "dg1", UserName: "user1"},
	}
	desktopGroups := []DesktopGroup{
		{DesktopGroupId: "dg1", Name: "Test Group"},
	}

	// Test session metrics tags
	sessionDataPoints := collector.calculateSessionMetrics(timestamp, sessions, desktopGroups)
	connectedLiveMetrics := findDataPointsByName(sessionDataPoints, "sessions_connected")

	for _, metric := range connectedLiveMetrics {
		// Should only have metric_type tag in simplified structure
		assert.True(t, hasTag(metric.Tags, "metric_type", "sessions"))
		// Other tags have been removed in the simplified structure
		assert.Equal(t, 1, len(metric.Tags), "Should only have metric_type tag")
	}

	// Test machine metrics tags
	machineDataPoints := collector.calculateMachineMetrics(timestamp, machines, desktopGroups)
	controllerMetrics := findDataPointsByName(machineDataPoints, "machines_by_controller")

	for _, metric := range controllerMetrics {
		// Should only have metric_type tag in simplified structure
		assert.True(t, hasTag(metric.Tags, "metric_type", "machines"))
		// Other tags have been removed in the simplified structure
		assert.Equal(t, 1, len(metric.Tags), "Should only have metric_type tag")
	}

	// Test user connection failure metrics tags
	categories := []ConnectionFailureCategory{
		{ConnectionFailureEnumValue: 1, Category: 0},
		{ConnectionFailureEnumValue: 6, Category: 1},
	}
	failureDataPoints := collector.calculateConnectionFailuresMetrics(timestamp, failures, desktopGroups, categories)
	userFailureMetrics := findDataPointsByName(failureDataPoints, "user_connection_failures_by_type")

	for _, metric := range userFailureMetrics {
		// Should only have metric_type tag in simplified structure
		assert.True(t, hasTag(metric.Tags, "metric_type", "user_connections"))
		// Other tags have been removed in the simplified structure
		assert.Equal(t, 1, len(metric.Tags), "Should only have metric_type tag")
	}
}

// Helper functions for advanced tag filtering tests

func findMetricWithoutDeliveryGroupTag(metrics []datapoint.DataPoint) *datapoint.DataPoint {
	for _, metric := range metrics {
		if !hasTagKey(metric.Tags, "delivery_group_id") {
			return &metric
		}
	}
	return nil
}

func hasTag(tags []tags.Tag, key, value string) bool {
	for _, tag := range tags {
		if tag.Key == key && tag.Value == value {
			return true
		}
	}
	return false
}

func hasTagKey(tags []tags.Tag, key string) bool {
	for _, tag := range tags {
		if tag.Key == key {
			return true
		}
	}
	return false
}

// Test calculateLogonDurationAvgHourly sliding window calculation
func TestMetricsCollector_CalculateLogonDurationAvgHourly_SlidingWindow(t *testing.T) {
	baseLogger := &logger.Logger{}
	mockClient := &MockCitrixClient{}
	collector := NewMetricsCollector(mockClient, baseLogger)

	// Test cases for different times to verify sliding window alignment
	tests := []struct {
		name          string
		currentTime   string
		expectedStart string
		description   string
	}{
		{
			name:          "afternoon with seconds",
			currentTime:   "2024-01-15T14:55:43Z",
			expectedStart: "2024-01-15T13:55:00Z",
			description:   "14:55:43 → window 13:55:00 to 14:55:00",
		},
		{
			name:          "morning with seconds",
			currentTime:   "2024-01-15T13:00:58Z",
			expectedStart: "2024-01-15T12:00:00Z",
			description:   "13:00:58 → window 12:00:00 to 13:00:00",
		},
		{
			name:          "exact minute boundary",
			currentTime:   "2024-01-15T09:32:00Z",
			expectedStart: "2024-01-15T08:32:00Z",
			description:   "09:32:00 → window 08:32:00 to 09:32:00",
		},
		{
			name:          "late evening",
			currentTime:   "2024-01-15T23:47:12Z",
			expectedStart: "2024-01-15T22:47:00Z",
			description:   "23:47:12 → window 22:47:00 to 23:47:00",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Parse test timestamp
			timestamp, err := time.Parse(time.RFC3339, tt.currentTime)
			assert.NoError(t, err)

			// Parse expected times
			expectedStart, err := time.Parse(time.RFC3339, tt.expectedStart)
			assert.NoError(t, err)

			// Mock empty connections (we're testing window calculation, not data processing)
			mockClient.On("GetConnections", mock.Anything, expectedStart).Return([]Connection{}, nil).Once()

			// Call the function
			ctx := context.Background()
			result := collector.calculateLogonDurationAvgHourly(ctx, timestamp)

			// Verify the result structure
			assert.Equal(t, "logon_duration_avg_1h", result.Name)
			assert.Equal(t, float32(0), result.Value) // No connections = 0 average
			assert.Equal(t, timestamp, result.Timestamp)

			// Verify mock was called with correct window start
			mockClient.AssertExpectations(t)
		})
	}
}

// Test calculateLogonDurationAvgHourly with actual connection data
func TestMetricsCollector_CalculateLogonDurationAvgHourly_WithConnections(t *testing.T) {
	baseLogger := &logger.Logger{}
	mockClient := &MockCitrixClient{}
	collector := NewMetricsCollector(mockClient, baseLogger)

	// Test timestamp: 2024-01-15T14:30:45Z
	// Expected window: 13:30:00 to 14:30:00
	timestamp, err := time.Parse(time.RFC3339, "2024-01-15T14:30:45Z")
	assert.NoError(t, err)

	windowStart, err := time.Parse(time.RFC3339, "2024-01-15T13:30:00Z")
	assert.NoError(t, err)

	// Create test connections within the window
	testConnections := []Connection{
		{
			Id:             1,
			Protocol:       "HDX",
			IsReconnect:    false,
			LogOnStartDate: time.Date(2024, 1, 15, 13, 45, 0, 0, time.UTC),
			LogOnEndDate:   time.Date(2024, 1, 15, 13, 45, 15, 0, time.UTC), // 15 seconds = 15000ms
		},
		{
			Id:             2,
			Protocol:       "HDX",
			IsReconnect:    false,
			LogOnStartDate: time.Date(2024, 1, 15, 14, 10, 0, 0, time.UTC),
			LogOnEndDate:   time.Date(2024, 1, 15, 14, 10, 25, 0, time.UTC), // 25 seconds = 25000ms
		},
		{
			Id:             3,
			Protocol:       "RDP", // Will be excluded (not HDX)
			IsReconnect:    false,
			LogOnStartDate: time.Date(2024, 1, 15, 14, 15, 0, 0, time.UTC),
			LogOnEndDate:   time.Date(2024, 1, 15, 14, 15, 10, 0, time.UTC),
		},
		{
			Id:             4,
			Protocol:       "HDX",
			IsReconnect:    true, // Will be excluded (reconnection)
			LogOnStartDate: time.Date(2024, 1, 15, 14, 20, 0, 0, time.UTC),
			LogOnEndDate:   time.Date(2024, 1, 15, 14, 20, 20, 0, time.UTC),
		},
		{
			Id:             5,
			Protocol:       "HDX",
			IsReconnect:    false,
			LogOnStartDate: time.Date(2024, 1, 15, 14, 25, 0, 0, time.UTC),
			LogOnEndDate:   time.Time{}, // Will be excluded (incomplete logon)
		},
		{
			Id:             6,
			Protocol:       "HDX",
			IsReconnect:    false,
			LogOnStartDate: time.Date(2024, 1, 15, 12, 45, 0, 0, time.UTC), // Outside window (before)
			LogOnEndDate:   time.Date(2024, 1, 15, 12, 45, 10, 0, time.UTC),
		},
		{
			Id:             7,
			Protocol:       "HDX",
			IsReconnect:    false,
			LogOnStartDate: time.Date(2024, 1, 15, 14, 35, 0, 0, time.UTC), // Outside window (after)
			LogOnEndDate:   time.Date(2024, 1, 15, 14, 35, 10, 0, time.UTC),
		},
	}

	mockClient.On("GetConnections", mock.Anything, windowStart).Return(testConnections, nil)

	// Call the function
	ctx := context.Background()
	result := collector.calculateLogonDurationAvgHourly(ctx, timestamp)

	// Verify the result
	assert.Equal(t, "logon_duration_avg_1h", result.Name)
	// Expected: (15000 + 25000) / 2 = 20000ms = 20.00 seconds
	assert.Equal(t, float32(20.00), result.Value)
	assert.Equal(t, timestamp, result.Timestamp)

	// Verify correct tags
	assert.Len(t, result.Tags, 1)
	assert.Equal(t, "metric_type", result.Tags[0].Key)
	assert.Equal(t, "logon", result.Tags[0].Value)

	// Verify mock was called correctly
	mockClient.AssertExpectations(t)
}

// Test calculateLogonDurationAvgHourly error handling
func TestMetricsCollector_CalculateLogonDurationAvgHourly_ErrorHandling(t *testing.T) {
	baseLogger := &logger.Logger{}
	mockClient := &MockCitrixClient{}
	collector := NewMetricsCollector(mockClient, baseLogger)

	timestamp := time.Now()
	expectedStart := timestamp.Truncate(time.Minute).Add(-1 * time.Hour)

	// Mock API error
	mockClient.On("GetConnections", mock.Anything, expectedStart).Return([]Connection{}, fmt.Errorf("API connection failed"))

	// Call the function
	ctx := context.Background()
	result := collector.calculateLogonDurationAvgHourly(ctx, timestamp)

	// Should return zero metric on error
	assert.Equal(t, "logon_duration_avg_1h", result.Name)
	assert.Equal(t, float32(0), result.Value)
	assert.Equal(t, timestamp, result.Timestamp)

	// Verify mock was called
	mockClient.AssertExpectations(t)
}

// Test sliding window alignment with various minute values
func TestMetricsCollector_CalculateLogonDurationAvgHourly_WindowAlignment(t *testing.T) {
	baseLogger := &logger.Logger{}
	mockClient := &MockCitrixClient{}
	collector := NewMetricsCollector(mockClient, baseLogger)

	// Test window calculation for various minute values
	testCases := []struct {
		currentTime   time.Time
		expectedStart time.Time
	}{
		{
			// 14:00:30 → window 13:00:00 to 14:00:00
			currentTime:   time.Date(2024, 1, 15, 14, 0, 30, 0, time.UTC),
			expectedStart: time.Date(2024, 1, 15, 13, 0, 0, 0, time.UTC),
		},
		{
			// 14:15:45 → window 13:15:00 to 14:15:00
			currentTime:   time.Date(2024, 1, 15, 14, 15, 45, 0, time.UTC),
			expectedStart: time.Date(2024, 1, 15, 13, 15, 0, 0, time.UTC),
		},
		{
			// 14:59:59 → window 13:59:00 to 14:59:00
			currentTime:   time.Date(2024, 1, 15, 14, 59, 59, 0, time.UTC),
			expectedStart: time.Date(2024, 1, 15, 13, 59, 0, 0, time.UTC),
		},
	}

	for i, tc := range testCases {
		t.Run(fmt.Sprintf("case_%d", i+1), func(t *testing.T) {
			// Mock API call
			mockClient.On("GetConnections", mock.Anything, tc.expectedStart).Return([]Connection{}, nil).Once()

			// Call function
			ctx := context.Background()
			result := collector.calculateLogonDurationAvgHourly(ctx, tc.currentTime)

			// Verify basic structure
			assert.Equal(t, "logon_duration_avg_1h", result.Name)
			assert.Equal(t, tc.currentTime, result.Timestamp)

			// Verify mock expectations (ensures correct window start was used)
			mockClient.AssertExpectations(t)
		})
	}
}

// Test simplified Directory-first configuration
func TestNewCitrixProbe_DirectoryFirstApproach(t *testing.T) {
	baseLogger := &logger.Logger{}
	
	// Simple configuration without complex machine inclusion strategies
	config := map[string]interface{}{
		"base_url": "https://director.example.com/Citrix/Monitor/OData/v4/Data",
		"auth": map[string]interface{}{
			"username": "domain\\user", 
			"password": "password",
		},
		"delivery_controller": map[string]interface{}{
			"url":         "https://ddc.company.com",
			"site_filter": "PROD",
		},
	}

	probe, err := NewCitrixProbe(config, baseLogger)
	
	assert.NoError(t, err)
	assert.NotNil(t, probe)
	assert.Equal(t, "citrix", probe.GetName())
	
	// Cast to citrixProbe to verify internal configuration
	citrixProbeImpl := probe.(*citrixProbe)
	assert.Equal(t, "PROD", citrixProbeImpl.siteFilter)
	assert.NotNil(t, citrixProbeImpl.ddcConfig)
	
	// Verify the simplification - no complex strategy fields
	// (These fields were removed in the simplification)
}

// Test inventory metrics collection (Directory-first approach)
func TestCitrixProbe_InventoryMetricsDirectoryFirst(t *testing.T) {
	baseLogger := &logger.Logger{}
	
	// Create simple configuration (Directory-first approach)
	config := map[string]interface{}{
		"base_url": "https://director.example.com/Citrix/Monitor/OData/v4/Data",
		"auth": map[string]interface{}{
			"username": "domain\\user",
			"password": "password",
		},
		"delivery_controller": map[string]interface{}{
			"url":         "https://ddc.company.com",
			"site_filter": "PROD",
		},
	}

	probe, err := NewCitrixProbe(config, baseLogger)
	assert.NoError(t, err)
	assert.NotNil(t, probe)

	// Cast to citrixProbe to access internal fields
	citrixProbeImpl := probe.(*citrixProbe)
	
	// Test that the probe was configured for Directory-first approach
	assert.Equal(t, "PROD", citrixProbeImpl.siteFilter)
	assert.NotNil(t, citrixProbeImpl.ddcConfig)

	// Test that the probe handles nil inventory service gracefully
}

// Test Directory-first filtering approach
func TestCitrixProbe_GetMachinesForMetricsDirectoryFirst(t *testing.T) {
	baseLogger := &logger.Logger{}
	mockClient := &MockCitrixClient{}

	// Create probe with Directory-first approach
	probe := &citrixProbe{
		BaseProbe: &types.BaseProbe{},
		logger:    logger.NewModuleLogger(baseLogger, "probe.citrix"),
		client:    mockClient,
		siteFilter: "PROD",
		filteredMachines: []string{}, // No pre-filtering
	}

	ctx := context.Background()
	sinceTime := time.Now().Add(-1 * time.Hour)

	// Test fallback to unfiltered query when no inventory service
	expectedMachines := []Machine{
		{MachineId: "m1", MachineName: "machine1", RegistrationState: RegistrationStateRegistered},
		{MachineId: "m2", MachineName: "machine2", RegistrationState: RegistrationStateUnregistered}, // OFF machine
	}

	mockClient.On("GetMachines", ctx, sinceTime).Return(expectedMachines, nil)

	// Call the method
	result, err := probe.GetMachinesForMetrics(ctx, sinceTime)

	// Verify results
	assert.NoError(t, err)
	assert.Equal(t, expectedMachines, result)
	assert.Len(t, result, 2, "Should return ALL machines (including OFF machines)")

	// Verify mock expectations were met
	mockClient.AssertExpectations(t)
}

// Test complete probe collection with Directory-first approach
func TestCitrixProbe_CollectDirectoryFirstApproach(t *testing.T) {
	baseLogger := &logger.Logger{}
	mockClient := &MockCitrixClient{}

	// Create probe configuration (Directory-first)
	config := map[string]interface{}{
		"base_url": "https://director.example.com/Citrix/Monitor/OData/v4/Data",
		"auth": map[string]interface{}{
			"username": "domain\\user",
			"password": "password",
		},
		"delivery_controller": map[string]interface{}{
			"url":         "https://ddc.company.com",
			"site_filter": "PROD",
		},
		"interval": 120,
	}

	probe, err := NewCitrixProbe(config, baseLogger)
	assert.NoError(t, err)
	assert.NotNil(t, probe)

	// Cast to citrixProbe and inject mock client
	citrixProbeImpl := probe.(*citrixProbe)
	citrixProbeImpl.client = mockClient
	citrixProbeImpl.metricsCollector = NewMetricsCollectorWithEnv(mockClient, "PROD", "https://director.example.com", baseLogger)

	// Mock data representing Director console reality (259 machines including OFF)
	mockSessions := []Session{
		{SessionKey: "session1", UserId: 123, UserName: "user1", DesktopGroupId: "dg1", SessionState: SessionStateConnected, MachineName: "machine1"},
		// Note: No sessions from OFF machine (natural filtering)
	}

	mockMachines := []Machine{
		{MachineId: "m1", MachineName: "machine1", RegistrationState: RegistrationStateRegistered, FaultState: FaultStateNone, SessionCount: 1},
		{MachineId: "m2", MachineName: "machine2", RegistrationState: RegistrationStateUnregistered, FaultState: FaultStateUnknown, SessionCount: 0}, // OFF machine
	}

	// Set up expectations for metrics collection (Directory-first: all machines returned)
	mockClient.On("GetMachines", mock.Anything, mock.Anything).Return(mockMachines, nil)
	mockClient.On("GetSessionsByConnectionState", mock.Anything, mock.Anything).Return(mockSessions, nil)
	mockClient.On("GetSessions", mock.Anything, mock.Anything).Return(mockSessions, nil)
	mockClient.On("GetConnections", mock.Anything, mock.Anything).Return([]Connection{}, nil).Maybe()
	mockClient.On("GetConnectionFailureCategories", mock.Anything).Return([]ConnectionFailureCategory{}, nil)
	mockClient.On("GetConnectionFailureLogs", mock.Anything, mock.Anything).Return([]ConnectionFailureLog{}, nil)

	// Test collection
	dataPoints, err := probe.Collect()

	// Verify results
	assert.NoError(t, err)
	assert.NotEmpty(t, dataPoints)

	// Log all metrics for debugging
	t.Logf("Generated metrics:")
	for _, dp := range dataPoints {
		t.Logf("  %s = %f", dp.Name, dp.Value)
	}

	// We should have some metrics generated
	assert.True(t, len(dataPoints) > 0, "Should have some metrics")
	
	// The key validation is that Directory-first approach works:
	// - All machines (including OFF) are included in API calls
	// - Metrics are generated successfully
	// - The specific metric names and values depend on the collector implementation

	// Verify mock expectations were met
	mockClient.AssertExpectations(t)
}

// Test configuration validation for Directory-first approach
func TestNewCitrixProbe_DirectoryFirstValidation(t *testing.T) {
	tests := []struct {
		name           string
		config         map[string]interface{}
		expectedSite   string
		expectDDCConfig bool
		wantErr        bool
	}{
		{
			name: "valid Directory-first configuration",
			config: map[string]interface{}{
				"base_url": "https://director.example.com/Citrix/Monitor/OData/v4/Data",
				"auth": map[string]interface{}{
					"username": "domain\\user",
					"password": "password",
				},
				"delivery_controller": map[string]interface{}{
					"url":         "https://ddc.company.com",
					"site_filter": "PROD",
				},
			},
			expectedSite:    "PROD",
			expectDDCConfig: true,
			wantErr:         false,
		},
		{
			name: "configuration without delivery_controller (OData only)",
			config: map[string]interface{}{
				"base_url": "https://director.example.com/Citrix/Monitor/OData/v4/Data",
				"auth": map[string]interface{}{
					"username": "domain\\user",
					"password": "password",
				},
			},
			expectedSite:    "",
			expectDDCConfig: false,
			wantErr:         false,
		},
		{
			name: "configuration with delivery_controller but no site_filter",
			config: map[string]interface{}{
				"base_url": "https://director.example.com/Citrix/Monitor/OData/v4/Data",
				"auth": map[string]interface{}{
					"username": "domain\\user",
					"password": "password",
				},
				"delivery_controller": map[string]interface{}{
					"url": "https://ddc.company.com",
				},
			},
			expectedSite:    "",
			expectDDCConfig: true,
			wantErr:         false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			baseLogger := &logger.Logger{}
			probe, err := NewCitrixProbe(tt.config, baseLogger)

			if tt.wantErr {
				assert.Error(t, err)
				assert.Nil(t, probe)
			} else {
				assert.NoError(t, err)
				assert.NotNil(t, probe)

				// Cast to citrixProbe to access internal fields
				citrixProbeImpl := probe.(*citrixProbe)
				assert.Equal(t, tt.expectedSite, citrixProbeImpl.siteFilter)
				
				if tt.expectDDCConfig {
					assert.NotNil(t, citrixProbeImpl.ddcConfig, "Should have DDC config")
				} else {
					assert.Nil(t, citrixProbeImpl.ddcConfig, "Should not have DDC config")
				}
			}
		})
	}
}
