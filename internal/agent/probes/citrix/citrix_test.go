package citrix

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"senhub-agent.go/internal/agent/types/datapoint"
	"senhub-agent.go/internal/agent/services/logger"
	"senhub-agent.go/internal/agent/tags"
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
			name: "valid configuration with Basic auth",
			config: map[string]interface{}{
				"base_url":    "https://director.example.com/Citrix/Monitor/OData/v4/Data",
				"environment": "PROD",
				"auth": map[string]interface{}{
					"method":   "basic",
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
			Id:                        1,
			FailureDate:              timestamp.Add(-1 * time.Minute),
			ConnectionFailureEnumValue: 1, // ClientConnectionFailure
			DesktopGroupId:           "dg1",
			DesktopGroupName:         "Test Group 1",
			UserName:                 "user1",
		},
		{
			Id:                        2,
			FailureDate:              timestamp.Add(-30 * time.Second),
			ConnectionFailureEnumValue: 1, // ClientConnectionFailure
			DesktopGroupId:           "dg1",
			DesktopGroupName:         "Test Group 1",
			UserName:                 "user2",
		},
		{
			Id:                        3,
			FailureDate:              timestamp.Add(-45 * time.Second),
			ConnectionFailureEnumValue: 6, // Configuration error (maps to category 1)
			DesktopGroupId:           "dg2",
			DesktopGroupName:         "Test Group 2",
			UserName:                 "user3",
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
		{ConnectionFailureEnumValue: 1, Category: 0},  // Client connection
		{ConnectionFailureEnumValue: 6, Category: 1},  // Configuration
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
		name           string
		desktopGroup   DesktopGroup
		expectedId     string
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
			MachineId:           "machine1",
			MachineName:         "VM-001",
			ControllerDNSName:   "controller1.example.com",
			RegistrationState:   RegistrationStateRegistered,
			FaultState:          FaultStateNone,
		},
		{
			MachineId:           "machine2",
			MachineName:         "VM-002",
			ControllerDNSName:   "controller1.example.com",
			RegistrationState:   RegistrationStateUnregistered,
			FaultState:          FaultStateUnknown,
		},
		{
			MachineId:           "machine3",
			MachineName:         "VM-003",
			ControllerDNSName:   "controller2.example.com",
			RegistrationState:   RegistrationStateRegistered,
			FaultState:          FaultStateNone,
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
			MachineId:           "machine1",
			MachineName:         "VM-001",
			ControllerDNSName:   "controller1.example.com",
			RegistrationState:   RegistrationStateRegistered,
			FaultState:          FaultStateNone,
			DesktopGroupId:      "dg1",
		},
		{
			MachineId:           "machine2",
			MachineName:         "VM-002",
			ControllerDNSName:   "controller1.example.com",
			RegistrationState:   RegistrationStateUnregistered,
			FaultState:          FaultStateUnknown,
			DesktopGroupId:      "dg1",
		},
		{
			MachineId:           "machine3",
			MachineName:         "VM-003",
			ControllerDNSName:   "controller2.example.com",
			RegistrationState:   RegistrationStateRegistered,
			FaultState:          FaultStateNone,
			DesktopGroupId:      "dg2",
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
			Id:               1,
			FailureDate:      timestamp.Add(-1 * time.Minute),
			ConnectionFailureEnumValue: 1, // ClientConnectionFailure
			DesktopGroupId:   "dg1",
			DesktopGroupName: "Test Group 1",
			UserName:         "user1",
		},
		{
			Id:               2,
			FailureDate:      timestamp.Add(-30 * time.Second),
			ConnectionFailureEnumValue: 6, // Configuration
			DesktopGroupId:   "dg1",
			DesktopGroupName: "Test Group 1",
			UserName:         "user2",
		},
		{
			Id:               3,
			FailureDate:      timestamp.Add(-45 * time.Second),
			ConnectionFailureEnumValue: 1, // ClientConnectionFailure
			DesktopGroupId:   "dg2",
			DesktopGroupName: "Test Group 2",
			UserName:         "user1", // Same user, different group
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
		{ConnectionFailureEnumValue: 1, Category: 0},  // Client connection
		{ConnectionFailureEnumValue: 6, Category: 1},  // Configuration
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
			"method":   "ntlm",
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
			MachineId:           "machine1",
			MachineName:         "VM-001",
			ControllerDNSName:   "controller1.example.com",
			RegistrationState:   RegistrationStateRegistered,
			FaultState:          FaultStateNone,
			DesktopGroupId:      "dg1",
		},
	}
	
	// mockDesktopGroups not needed for 1min frequency test
	
	mockFailures := []ConnectionFailureLog{
		{
			Id:               1,
			FailureDate:      time.Now().Add(-30 * time.Minute),
			ConnectionFailureEnumValue: 1, // ClientConnectionFailure
			DesktopGroupId:   "dg1",
			DesktopGroupName: "Test Group 1",
			UserName:         "user1",
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
		case dp.Name == "connection_failures_count":
			hasFailureMetrics = true
		case dp.Name == "logon_sessions_opened_2m" || dp.Name == "logon_duration_total_2m":
			hasLogonMetrics = true
		case dp.Name == "logon_brokering_avg_2m" || dp.Name == "logon_hdx_avg_2m":
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

	// Verify controller-specific metrics
	controllerMetrics := findDataPointsByName(dataPoints, "machines_by_controller")
	assert.GreaterOrEqual(t, len(controllerMetrics), 4) // At least total + registered/unregistered/etc for each controller

	// Check controller1 metrics
	ctrl1Metrics := findMetricsWithTag(controllerMetrics, "controller_dns_name", "ctx-ctrl-01.domain.com")
	assert.GreaterOrEqual(t, len(ctrl1Metrics), 2) // Should have multiple state metrics for this controller
	
	// Find total machines for controller1
	ctrl1TotalMetric := findMetricWithTags(ctrl1Metrics, map[string]string{
		"controller_dns_name": "ctx-ctrl-01.domain.com",
		"machine_state":      "total",
	})
	assert.NotNil(t, ctrl1TotalMetric)
	assert.Equal(t, float32(2.0), ctrl1TotalMetric.Value)

	// Check controller2 metrics
	ctrl2Metrics := findMetricsWithTag(controllerMetrics, "controller_dns_name", "ctx-ctrl-02.domain.com")
	assert.GreaterOrEqual(t, len(ctrl2Metrics), 1)
	
	ctrl2TotalMetric := findMetricWithTags(ctrl2Metrics, map[string]string{
		"controller_dns_name": "ctx-ctrl-02.domain.com",
		"machine_state":      "total",
	})
	assert.NotNil(t, ctrl2TotalMetric)
	assert.Equal(t, float32(1.0), ctrl2TotalMetric.Value)

	// Verify state filtering by controller
	stateMetrics := findDataPointsByName(dataPoints, "machines_by_state")
	ctrl1StateMetrics := findMetricsWithTag(stateMetrics, "controller_dns_name", "ctx-ctrl-01.domain.com")
	assert.GreaterOrEqual(t, len(ctrl1StateMetrics), 1) // Should have registration state metrics for this controller
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
			Id:               1,
			FailureDate:      timestamp.Add(-10 * time.Minute),
			ConnectionFailureEnumValue: 1, // ClientConnectionFailure
			DesktopGroupId:   "dg1",
			DesktopGroupName: "Production Apps",
			UserName:         "user1",
		},
		{
			Id:               2,
			FailureDate:      timestamp.Add(-15 * time.Minute),
			ConnectionFailureEnumValue: 1, // ClientConnectionFailure
			DesktopGroupId:   "dg1",
			DesktopGroupName: "Production Apps",
			UserName:         "user2",
		},
		{
			Id:               3,
			FailureDate:      timestamp.Add(-20 * time.Minute),
			ConnectionFailureEnumValue: 6, // Configuration
			DesktopGroupId:   "dg2",
			DesktopGroupName: "VDI Desktops",
			UserName:         "user3",
		},
		{
			Id:               4,
			FailureDate:      timestamp.Add(-5 * time.Minute),
			ConnectionFailureEnumValue: 2, // MachineFailure
			DesktopGroupId:   "dg2",
			DesktopGroupName: "VDI Desktops",
			UserName:         "user1", // Same user, different failure type and DG
		},
	}

	desktopGroups := []DesktopGroup{
		{DesktopGroupId: "dg1", Name: "Production Apps"},
		{DesktopGroupId: "dg2", Name: "VDI Desktops"},
	}

	// Categories mapping from production data
	categories := []ConnectionFailureCategory{
		{ConnectionFailureEnumValue: 1, Category: 0},  // Client connection
		{ConnectionFailureEnumValue: 2, Category: 2},  // Machine failure
		{ConnectionFailureEnumValue: 6, Category: 1},  // Configuration
	}

	dataPoints := collector.calculateConnectionFailuresMetrics(timestamp, failures, desktopGroups, categories)

	// Verify global failure metrics
	totalFailuresMetric := findDataPointByName(dataPoints, "user_connection_failures_total")
	assert.NotNil(t, totalFailuresMetric)
	assert.Equal(t, float32(4.0), totalFailuresMetric.Value)

	uniqueUsersMetric := findDataPointByName(dataPoints, "user_connection_users_with_failures")
	assert.NotNil(t, uniqueUsersMetric)
	assert.Equal(t, float32(3.0), uniqueUsersMetric.Value) // 3 unique users: user1, user2, user3

	// Verify filtering by failure type
	typeMetrics := findDataPointsByName(dataPoints, "user_connection_failures_by_type")
	
	// Check client_connection failures (should be 2 globally)
	clientConnFailures := findMetricsWithTag(typeMetrics, "failure_type", "client_connection_failures")
	globalClientConnFailure := findMetricWithoutDeliveryGroupTag(clientConnFailures)
	assert.NotNil(t, globalClientConnFailure)
	assert.Equal(t, float32(2.0), globalClientConnFailure.Value)

	// Verify filtering by delivery group
	dgMetrics := findDataPointsByName(dataPoints, "user_connection_failures_by_delivery_group")
	
	dg1Failures := findMetricsWithTag(dgMetrics, "delivery_group_id", "dg1")
	assert.Len(t, dg1Failures, 1)
	assert.Equal(t, float32(2.0), dg1Failures[0].Value) // 2 failures in DG1

	dg2Failures := findMetricsWithTag(dgMetrics, "delivery_group_id", "dg2")
	assert.Len(t, dg2Failures, 1)
	assert.Equal(t, float32(2.0), dg2Failures[0].Value) // 2 failures in DG2

	// Verify unique users per delivery group
	usersWithFailuresMetrics := findDataPointsByName(dataPoints, "user_connection_users_with_failures")
	
	dg1Users := findMetricsWithTag(usersWithFailuresMetrics, "delivery_group_id", "dg1")
	assert.Len(t, dg1Users, 1)
	assert.Equal(t, float32(2.0), dg1Users[0].Value) // user1 and user2 in DG1

	dg2Users := findMetricsWithTag(usersWithFailuresMetrics, "delivery_group_id", "dg2")
	assert.Len(t, dg2Users, 1)
	assert.Equal(t, float32(2.0), dg2Users[0].Value) // user3 and user1 in DG2
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
		// Should have metric_type tag
		assert.True(t, hasTag(metric.Tags, "metric_type", "sessions"))
		// Should have session_filter tag
		assert.True(t, hasTag(metric.Tags, "session_filter", "connected"))
		
		// If has delivery group, should have both ID and name
		if hasTagKey(metric.Tags, "delivery_group_id") {
			assert.True(t, hasTagKey(metric.Tags, "delivery_group_name"))
		}
	}

	// Test machine metrics tags
	machineDataPoints := collector.calculateMachineMetrics(timestamp, machines, desktopGroups)
	controllerMetrics := findDataPointsByName(machineDataPoints, "machines_by_controller")
	
	for _, metric := range controllerMetrics {
		// Should have metric_type tag
		assert.True(t, hasTag(metric.Tags, "metric_type", "machines"))
		// Should have controller_dns_name tag
		assert.True(t, hasTagKey(metric.Tags, "controller_dns_name"))
		// Should have machine_state tag
		assert.True(t, hasTagKey(metric.Tags, "machine_state"))
	}

	// Test user connection failure metrics tags
	categories := []ConnectionFailureCategory{
		{ConnectionFailureEnumValue: 1, Category: 0},
		{ConnectionFailureEnumValue: 6, Category: 1},
	}
	failureDataPoints := collector.calculateConnectionFailuresMetrics(timestamp, failures, desktopGroups, categories)
	userFailureMetrics := findDataPointsByName(failureDataPoints, "user_connection_failures_by_type")
	
	for _, metric := range userFailureMetrics {
		// Should have metric_type tag
		assert.True(t, hasTag(metric.Tags, "metric_type", "user_connections"))
		// Should have failure_type tag
		assert.True(t, hasTagKey(metric.Tags, "failure_type"))
		// Should have time_window tag
		assert.True(t, hasTag(metric.Tags, "time_window", "1h"))
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

func findMetricsWithTag(metrics []datapoint.DataPoint, tagKey, tagValue string) []datapoint.DataPoint {
	var result []datapoint.DataPoint
	for _, metric := range metrics {
		if hasTag(metric.Tags, tagKey, tagValue) {
			result = append(result, metric)
		}
	}
	return result
}

func findMetricWithTags(metrics []datapoint.DataPoint, requiredTags map[string]string) *datapoint.DataPoint {
	for _, metric := range metrics {
		matches := true
		for key, value := range requiredTags {
			if !hasTag(metric.Tags, key, value) {
				matches = false
				break
			}
		}
		if matches {
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

func findTagValue(tags []tags.Tag, key string) string {
	for _, tag := range tags {
		if tag.Key == key {
			return tag.Value
		}
	}
	return ""
}