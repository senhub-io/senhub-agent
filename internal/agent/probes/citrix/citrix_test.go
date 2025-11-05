package citrix

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"senhub-agent.go/internal/agent/probes/types"
	"senhub-agent.go/internal/agent/services/logger"
)

// Keep only essential tests for public interface and core functionality

func TestNewCitrixProbe(t *testing.T) {
	baseLogger := &logger.Logger{}

	tests := []struct {
		name    string
		config  map[string]interface{}
		wantErr bool
	}{
		{
			name: "valid configuration with NTLM",
			config: map[string]interface{}{
				"base_url": "https://citrix.example.com",
				"auth": map[string]interface{}{
					"method":   "ntlm",
					"username": "test\\user",
					"password": "password",
				},
			},
			wantErr: false,
		},
		{
			name: "valid configuration with Basic auth",
			config: map[string]interface{}{
				"base_url": "https://citrix.example.com",
				"auth": map[string]interface{}{
					"method":   "basic",
					"username": "testuser",
					"password": "password",
				},
			},
			wantErr: false,
		},
		{
			name: "missing base_url",
			config: map[string]interface{}{
				"auth": map[string]interface{}{
					"username": "test\\user",
					"password": "password",
				},
			},
			wantErr: true,
		},
		{
			name: "missing auth configuration",
			config: map[string]interface{}{
				"base_url": "https://citrix.example.com",
			},
			wantErr: true,
		},
		{
			name: "missing username",
			config: map[string]interface{}{
				"base_url": "https://citrix.example.com",
				"auth": map[string]interface{}{
					"password": "password",
				},
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := NewCitrixProbe(tt.config, baseLogger)
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestCitrixProbe_GetInterval(t *testing.T) {
	baseLogger := &logger.Logger{}
	config := map[string]interface{}{
		"base_url": "https://citrix.example.com",
		"interval": 120,
		"auth": map[string]interface{}{
			"method":   "ntlm",
			"username": "test\\user",
			"password": "password",
		},
	}

	probe, err := NewCitrixProbe(config, baseLogger)
	assert.NoError(t, err)
	assert.Equal(t, time.Duration(120)*time.Second, probe.GetInterval())
}

func TestDesktopGroup_GetEffectiveId(t *testing.T) {
	tests := []struct {
		name     string
		dg       DesktopGroup
		expected string
	}{
		{
			name: "DesktopGroupId takes precedence (real Citrix server)",
			dg: DesktopGroup{
				Id:             "real-id-1234",
				DesktopGroupId: "preferred-id-5678",
				Name:           "Windows 10 Apps",
				DeliveryType:   1,
			},
			expected: "preferred-id-5678",
		},
		{
			name: "Id used when DesktopGroupId is empty (mock server)",
			dg: DesktopGroup{
				Id:             "fallback-id-9999",
				DesktopGroupId: "",
				Name:           "Test Delivery Group",
				DeliveryType:   1,
			},
			expected: "fallback-id-9999",
		},
		{
			name: "Id used when DesktopGroupId not set (mock server)",
			dg: DesktopGroup{
				Id:           "only-id-1111",
				Name:         "Legacy Delivery Group",
				DeliveryType: 1,
			},
			expected: "only-id-1111",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := tt.dg.GetEffectiveId()
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestNewCitrixClient_AuthenticationMethods(t *testing.T) {
	baseLogger := &logger.Logger{}

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
				BaseURL:    "https://citrix.example.com",
				AuthMethod: tt.authMethod,
				Username:   "test\\user",
				Password:   "password",
				VerifySSL:  false,
				Timeout:    30 * time.Second,
			}

			_, err := NewCitrixClient(config, baseLogger)
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestCitrixProbe_Collect(t *testing.T) {
	baseLogger := &logger.Logger{}
	mockClient := &MockCitrixClient{}

	// Create metrics collector with mock client
	collector := NewMetricsCollector(mockClient, baseLogger)

	timestamp := time.Now()

	// Mock data
	mockMachines := []Machine{
		{MachineId: "m1", MachineName: "machine1", RegistrationState: RegistrationStateRegistered},
		{MachineId: "m2", MachineName: "machine2", RegistrationState: RegistrationStateUnregistered},
	}
	mockSessions := []Session{
		{SessionKey: "s1", UserName: "user1", ConnectionState: SessionStateActive},
		{SessionKey: "s2", UserName: "user2", ConnectionState: SessionStateDisconnected},
	}
	mockFailures := []ConnectionFailureLog{
		{Id: 1, UserName: "user1", ConnectionFailureEnumValue: 1},
	}

	// Set up expectations for the new specialized modules
	mockClient.On("GetMachines", mock.Anything, mock.Anything).Return(mockMachines, nil)
	mockClient.On("GetSessionsByConnectionState", mock.Anything, mock.Anything).Return(mockSessions, nil)
	// GetSessions call removed - zombie session metrics no longer collected
	mockClient.On("GetConnections", mock.Anything, mock.Anything).Return([]Connection{}, nil).Maybe()
	mockClient.On("GetConnectionFailureCategories", mock.Anything).Return([]ConnectionFailureCategory{}, nil)
	mockClient.On("GetConnectionFailureLogs", mock.Anything, mock.Anything).Return(mockFailures, nil)

	// Test collection using the new specialized modules
	dataPoints, err := collector.CollectMetrics(context.Background(), timestamp)

	// Verify results
	assert.NoError(t, err)
	assert.NotEmpty(t, dataPoints)

	// Check that we have metrics from different categories
	var hasInfraMetrics, hasSessionMetrics, hasLogonMetrics, hasFailureMetrics bool
	for _, dp := range dataPoints {
		for _, tag := range dp.Tags {
			if tag.Key == "metric_type" {
				switch tag.Value {
				case "infrastructure":
					hasInfraMetrics = true
				case "sessions":
					hasSessionMetrics = true
				case "logon":
					hasLogonMetrics = true
				case "connection_failures":
					hasFailureMetrics = true
				}
			}
		}
	}

	assert.True(t, hasInfraMetrics, "Should have infrastructure metrics")
	assert.True(t, hasSessionMetrics, "Should have session metrics")
	assert.True(t, hasLogonMetrics, "Should have logon breakdown metrics (even if zero)")
	assert.True(t, hasFailureMetrics, "Should have failure metrics")

	// Verify mock expectations were met
	mockClient.AssertExpectations(t)
}

func TestNewCitrixProbe_DirectoryFirstApproach(t *testing.T) {
	baseLogger := &logger.Logger{}

	config := map[string]interface{}{
		"base_url": "https://director.company.com",
		"auth": map[string]interface{}{
			"method":   "ntlm",
			"username": "test\\user",
			"password": "password",
		},
		"delivery_controller": map[string]interface{}{
			"url":         "https://ddc.company.com",
			"site_filter": "TestSite",
			"fallback_urls": []string{
				"https://ddc-backup.company.com",
			},
		},
	}

	probe, err := NewCitrixProbe(config, baseLogger)
	assert.NoError(t, err)
	assert.NotNil(t, probe)

	// Verify that the probe was created successfully
	assert.Equal(t, time.Duration(120)*time.Second, probe.GetInterval()) // default interval
}

func TestNewCitrixProbe_DirectoryFirstValidation(t *testing.T) {
	baseLogger := &logger.Logger{}

	tests := []struct {
		name    string
		config  map[string]interface{}
		wantErr bool
	}{
		{
			name: "valid Directory-first configuration",
			config: map[string]interface{}{
				"base_url": "https://director.company.com",
				"auth": map[string]interface{}{
					"method":   "ntlm",
					"username": "test\\user",
					"password": "password",
				},
				"delivery_controller": map[string]interface{}{
					"url":         "https://ddc.company.com",
					"site_filter": "TestSite",
				},
			},
			wantErr: false,
		},
		{
			name: "configuration without delivery_controller (OData only)",
			config: map[string]interface{}{
				"base_url": "https://director.company.com",
				"auth": map[string]interface{}{
					"method":   "ntlm",
					"username": "test\\user",
					"password": "password",
				},
			},
			wantErr: false,
		},
		{
			name: "configuration with delivery_controller but no site_filter",
			config: map[string]interface{}{
				"base_url": "https://director.company.com",
				"auth": map[string]interface{}{
					"method":   "ntlm",
					"username": "test\\user",
					"password": "password",
				},
				"delivery_controller": map[string]interface{}{
					"url": "https://ddc.company.com",
				},
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := NewCitrixProbe(tt.config, baseLogger)
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

// Mock implementation for testing
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

func (m *MockCitrixClient) GetSessionsByConnectionState(ctx context.Context, connectionStates []int) ([]Session, error) {
	args := m.Called(ctx, connectionStates)
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

func (m *MockCitrixClient) GetConnectionFailureLogsWithExpand(ctx context.Context, sinceTime time.Time, expand []string) ([]ConnectionFailureLog, error) {
	args := m.Called(ctx, sinceTime, expand)
	return args.Get(0).([]ConnectionFailureLog), args.Error(1)
}

func (m *MockCitrixClient) GetConnectionFailureCategories(ctx context.Context) ([]ConnectionFailureCategory, error) {
	args := m.Called(ctx)
	return args.Get(0).([]ConnectionFailureCategory), args.Error(1)
}

func (m *MockCitrixClient) GetDeliveryGroupById(ctx context.Context, deliveryGroupId string) (*DesktopGroup, error) {
	args := m.Called(ctx, deliveryGroupId)
	return args.Get(0).(*DesktopGroup), args.Error(1)
}

func (m *MockCitrixClient) GetConnections(ctx context.Context, sinceTime time.Time) ([]Connection, error) {
	args := m.Called(ctx, sinceTime)
	return args.Get(0).([]Connection), args.Error(1)
}

func (m *MockCitrixClient) SetValidMachineDNS(dnsNames []string) {
	m.Called(dnsNames)
}

// Additional tests for better coverage

func TestCitrixProbe_GetName(t *testing.T) {
	baseLogger := &logger.Logger{}
	config := map[string]interface{}{
		"base_url": "https://citrix.example.com",
		"auth": map[string]interface{}{
			"username": "test\\user",
			"password": "password",
		},
	}

	probe, err := NewCitrixProbe(config, baseLogger)
	assert.NoError(t, err)

	// Test BaseProbe inheritance: SetName() and GetName()
	probe.(interface{ SetName(string) }).SetName("citrix")
	assert.Equal(t, "citrix", probe.GetName())

	// Test default behavior: GetName() returns empty string before SetName() is called
	probe2, _ := NewCitrixProbe(config, baseLogger)
	assert.Equal(t, "", probe2.GetName())
}

func TestCitrixProbe_ShouldStart(t *testing.T) {
	baseLogger := &logger.Logger{}
	config := map[string]interface{}{
		"base_url": "https://citrix.example.com",
		"auth": map[string]interface{}{
			"username": "test\\user",
			"password": "password",
		},
	}

	probe, err := NewCitrixProbe(config, baseLogger)
	assert.NoError(t, err)
	assert.True(t, probe.ShouldStart())
}

func TestCitrixProbe_GetTargetStrategies(t *testing.T) {
	baseLogger := &logger.Logger{}
	config := map[string]interface{}{
		"base_url": "https://citrix.example.com",
		"auth": map[string]interface{}{
			"username": "test\\user",
			"password": "password",
		},
	}

	probe, err := NewCitrixProbe(config, baseLogger)
	assert.NoError(t, err)

	citrixProbe := probe.(*citrixProbe)
	strategies := citrixProbe.GetTargetStrategies()
	expected := []string{"senhub", "prtg", "http"}

	assert.Equal(t, len(expected), len(strategies))
	for i, strategy := range expected {
		assert.Equal(t, strategy, strategies[i])
	}
}

func TestCitrixProbe_OnShutdown(t *testing.T) {
	baseLogger := &logger.Logger{}
	mockClient := &MockCitrixClient{}

	config := map[string]interface{}{
		"base_url": "https://citrix.example.com",
		"auth": map[string]interface{}{
			"username": "test\\user",
			"password": "password",
		},
	}

	probe, err := NewCitrixProbe(config, baseLogger)
	assert.NoError(t, err)

	citrixProbe := probe.(*citrixProbe)
	citrixProbe.client = mockClient

	tests := []struct {
		name            string
		disconnectError error
		wantErr         bool
	}{
		{
			name:            "successful shutdown",
			disconnectError: nil,
			wantErr:         false,
		},
		{
			name:            "disconnect error",
			disconnectError: assert.AnError,
			wantErr:         true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockClient.On("Disconnect", mock.Anything).Return(tt.disconnectError).Once()

			ctx := context.Background()
			err := probe.OnShutdown(ctx)

			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}

			mockClient.AssertExpectations(t)
		})
	}
}

func TestCitrixProbe_OnShutdown_NoClient(t *testing.T) {
	baseLogger := &logger.Logger{}
	config := map[string]interface{}{
		"base_url": "https://citrix.example.com",
		"auth": map[string]interface{}{
			"username": "test\\user",
			"password": "password",
		},
	}

	probe, err := NewCitrixProbe(config, baseLogger)
	assert.NoError(t, err)

	// Don't set client - should not error
	ctx := context.Background()
	err = probe.OnShutdown(ctx)
	assert.NoError(t, err)
}

func TestCitrixProbe_Collect_Errors(t *testing.T) {
	baseLogger := &logger.Logger{}
	config := map[string]interface{}{
		"base_url": "https://citrix.example.com",
		"auth": map[string]interface{}{
			"username": "test\\user",
			"password": "password",
		},
	}

	t.Run("client not initialized", func(t *testing.T) {
		probe, err := NewCitrixProbe(config, baseLogger)
		assert.NoError(t, err)

		// Don't initialize client
		_, err = probe.Collect()
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "client not initialized")
	})

	t.Run("metrics collector not initialized", func(t *testing.T) {
		probe, err := NewCitrixProbe(config, baseLogger)
		assert.NoError(t, err)

		citrixProbe := probe.(*citrixProbe)
		citrixProbe.client = &MockCitrixClient{} // Set client but not collector

		_, err = probe.Collect()
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "metrics collector not initialized")
	})

	t.Run("fault-tolerant collection with partial errors", func(t *testing.T) {
		mockClient := &MockCitrixClient{}
		probe, err := NewCitrixProbe(config, baseLogger)
		assert.NoError(t, err)

		citrixProbe := probe.(*citrixProbe)
		citrixProbe.client = mockClient
		citrixProbe.metricsCollector = NewMetricsCollector(mockClient, baseLogger)

		// Metrics collector is fault-tolerant - even if some metrics fail, it continues
		// Infrastructure metrics - fail
		mockClient.On("GetMachines", mock.Anything, mock.Anything).Return([]Machine{}, assert.AnError)
		// Session metrics - success with data
		mockClient.On("GetSessionsByConnectionState", mock.Anything, mock.Anything).Return([]Session{
			{SessionKey: "s1", UserName: "user1", ConnectionState: SessionStateActive},
		}, nil)
		// Logon metrics - success with no data
		mockClient.On("GetConnections", mock.Anything, mock.Anything).Return([]Connection{}, nil)
		// Failure metrics - success
		mockClient.On("GetConnectionFailureLogs", mock.Anything, mock.Anything).Return([]ConnectionFailureLog{}, nil)
		mockClient.On("GetConnectionFailureCategories", mock.Anything).Return([]ConnectionFailureCategory{}, nil)

		dataPoints, err := probe.Collect()
		// Should succeed even with partial errors (fault-tolerant)
		assert.NoError(t, err)
		assert.NotNil(t, dataPoints)
	})
}

func TestCitrixProbe_Collect_DebugMode(t *testing.T) {
	baseLogger := &logger.Logger{}
	mockClient := &MockCitrixClient{}

	config := map[string]interface{}{
		"base_url": "https://citrix.example.com",
		"auth": map[string]interface{}{
			"username": "test\\user",
			"password": "password",
		},
		"debug_identifiers": true, // Enable debug mode
	}

	probe, err := NewCitrixProbe(config, baseLogger)
	assert.NoError(t, err)

	citrixProbe := probe.(*citrixProbe)
	citrixProbe.client = mockClient
	citrixProbe.metricsCollector = NewMetricsCollector(mockClient, baseLogger)

	// Mock methods called by DebugIdentifierMapping
	mockClient.On("GetMachines", mock.Anything, mock.Anything).Return([]Machine{
		{MachineId: "m1", MachineName: "machine1", DnsName: "machine1.local"},
	}, nil)
	mockClient.On("GetSessions", mock.Anything, mock.Anything).Return([]Session{
		{SessionKey: "s1", UserName: "user1"},
	}, nil)

	// In debug mode, should return empty datapoints
	dataPoints, err := probe.Collect()
	assert.NoError(t, err)
	assert.Empty(t, dataPoints)
}

func TestNewCitrixProbe_ConfigurationValidation(t *testing.T) {
	baseLogger := &logger.Logger{}

	tests := []struct {
		name    string
		config  map[string]interface{}
		wantErr bool
		errMsg  string
	}{
		{
			name: "custom timeout",
			config: map[string]interface{}{
				"base_url": "https://citrix.example.com",
				"auth": map[string]interface{}{
					"username": "test\\user",
					"password": "password",
				},
				"timeout": 60,
			},
			wantErr: false,
		},
		{
			name: "custom retry configuration",
			config: map[string]interface{}{
				"base_url": "https://citrix.example.com",
				"auth": map[string]interface{}{
					"username": "test\\user",
					"password": "password",
				},
				"retry": map[string]interface{}{
					"max_attempts":   5,
					"backoff_factor": 1.5,
				},
			},
			wantErr: false,
		},
		{
			name: "custom interval",
			config: map[string]interface{}{
				"base_url": "https://citrix.example.com",
				"auth": map[string]interface{}{
					"username": "test\\user",
					"password": "password",
				},
				"interval": 300, // 5 minutes
			},
			wantErr: false,
		},
		{
			name: "TLS configuration",
			config: map[string]interface{}{
				"base_url": "https://citrix.example.com",
				"auth": map[string]interface{}{
					"username": "test\\user",
					"password": "password",
				},
				"tls": map[string]interface{}{
					"verify_ssl": true,
				},
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			probe, err := NewCitrixProbe(tt.config, baseLogger)
			if tt.wantErr {
				assert.Error(t, err)
				if tt.errMsg != "" {
					assert.Contains(t, err.Error(), tt.errMsg)
				}
			} else {
				assert.NoError(t, err)
				assert.NotNil(t, probe)
			}
		})
	}
}

func TestFilteredCitrixClient(t *testing.T) {
	baseLogger := &logger.Logger{}
	mockClient := &MockCitrixClient{}

	config := map[string]interface{}{
		"base_url": "https://citrix.example.com",
		"auth": map[string]interface{}{
			"username": "test\\user",
			"password": "password",
		},
	}

	probe, err := NewCitrixProbe(config, baseLogger)
	assert.NoError(t, err)

	citrixProbe := probe.(*citrixProbe)
	citrixProbe.client = mockClient

	filteredClient := &filteredCitrixClient{
		originalClient: mockClient,
		probe:          citrixProbe,
	}

	ctx := context.Background()
	sinceTime := time.Now().Add(-1 * time.Hour)

	t.Run("Connect delegates to original", func(t *testing.T) {
		mockClient.On("Connect", mock.Anything).Return(nil).Once()
		err := filteredClient.Connect(ctx)
		assert.NoError(t, err)
		mockClient.AssertExpectations(t)
	})

	t.Run("Disconnect delegates to original", func(t *testing.T) {
		mockClient.On("Disconnect", mock.Anything).Return(nil).Once()
		err := filteredClient.Disconnect(ctx)
		assert.NoError(t, err)
		mockClient.AssertExpectations(t)
	})

	t.Run("GetSessions delegates to original", func(t *testing.T) {
		expectedSessions := []Session{{SessionKey: "test"}}
		mockClient.On("GetSessions", mock.Anything, mock.Anything).Return(expectedSessions, nil).Once()
		sessions, err := filteredClient.GetSessions(ctx, sinceTime)
		assert.NoError(t, err)
		assert.Equal(t, expectedSessions, sessions)
		mockClient.AssertExpectations(t)
	})

	t.Run("GetSessionsByConnectionState delegates to original", func(t *testing.T) {
		expectedSessions := []Session{{SessionKey: "test"}}
		mockClient.On("GetSessionsByConnectionState", mock.Anything, mock.Anything).Return(expectedSessions, nil).Once()
		sessions, err := filteredClient.GetSessionsByConnectionState(ctx, []int{5})
		assert.NoError(t, err)
		assert.Equal(t, expectedSessions, sessions)
		mockClient.AssertExpectations(t)
	})

	t.Run("GetMachines uses probe filtering", func(t *testing.T) {
		expectedMachines := []Machine{{MachineId: "m1"}}
		mockClient.On("GetMachines", mock.Anything, mock.Anything).Return(expectedMachines, nil).Once()
		machines, err := filteredClient.GetMachines(ctx, sinceTime)
		assert.NoError(t, err)
		assert.Equal(t, expectedMachines, machines)
		mockClient.AssertExpectations(t)
	})

	t.Run("GetDesktopGroups delegates to original", func(t *testing.T) {
		expectedGroups := []DesktopGroup{{Id: "dg1"}}
		mockClient.On("GetDesktopGroups", mock.Anything).Return(expectedGroups, nil).Once()
		groups, err := filteredClient.GetDesktopGroups(ctx)
		assert.NoError(t, err)
		assert.Equal(t, expectedGroups, groups)
		mockClient.AssertExpectations(t)
	})

	t.Run("GetConnectionFailureLogs delegates to original", func(t *testing.T) {
		expectedLogs := []ConnectionFailureLog{{Id: 1}}
		mockClient.On("GetConnectionFailureLogs", mock.Anything, mock.Anything).Return(expectedLogs, nil).Once()
		logs, err := filteredClient.GetConnectionFailureLogs(ctx, sinceTime)
		assert.NoError(t, err)
		assert.Equal(t, expectedLogs, logs)
		mockClient.AssertExpectations(t)
	})

	t.Run("GetConnections delegates to original", func(t *testing.T) {
		expectedConns := []Connection{{Id: 1}}
		mockClient.On("GetConnections", mock.Anything, mock.Anything).Return(expectedConns, nil).Once()
		conns, err := filteredClient.GetConnections(ctx, sinceTime)
		assert.NoError(t, err)
		assert.Equal(t, expectedConns, conns)
		mockClient.AssertExpectations(t)
	})

	t.Run("SetValidMachineDNS delegates to original", func(t *testing.T) {
		dnsNames := []string{"machine1.local", "machine2.local"}
		mockClient.On("SetValidMachineDNS", dnsNames).Once()
		filteredClient.SetValidMachineDNS(dnsNames)
		mockClient.AssertExpectations(t)
	})
}

func TestCitrixProbe_GetMachinesForMetrics(t *testing.T) {
	baseLogger := &logger.Logger{}
	mockClient := &MockCitrixClient{}

	config := map[string]interface{}{
		"base_url": "https://citrix.example.com",
		"auth": map[string]interface{}{
			"username": "test\\user",
			"password": "password",
		},
	}

	probe, err := NewCitrixProbe(config, baseLogger)
	assert.NoError(t, err)

	citrixProbe := probe.(*citrixProbe)
	citrixProbe.client = mockClient

	ctx := context.Background()
	sinceTime := time.Now().Add(-1 * time.Hour)

	t.Run("without inventory service - fallback", func(t *testing.T) {
		expectedMachines := []Machine{{MachineId: "m1", MachineName: "machine1"}}
		mockClient.On("GetMachines", mock.Anything, mock.Anything).Return(expectedMachines, nil).Once()

		machines, err := citrixProbe.GetMachinesForMetrics(ctx, sinceTime)
		assert.NoError(t, err)
		assert.Equal(t, expectedMachines, machines)
		mockClient.AssertExpectations(t)
	})
}

func TestNewCitrixProbe_AdvancedConfiguration(t *testing.T) {
	baseLogger := &logger.Logger{}

	tests := []struct {
		name      string
		config    map[string]interface{}
		wantErr   bool
		checkFunc func(*testing.T, types.Probe)
	}{
		{
			name: "with fallback DDC URLs",
			config: map[string]interface{}{
				"base_url": "https://director.company.com",
				"auth": map[string]interface{}{
					"username": "test\\user",
					"password": "password",
				},
				"delivery_controller": map[string]interface{}{
					"url": "https://ddc1.company.com",
					"fallback_urls": []interface{}{
						"https://ddc2.company.com",
						"https://ddc3.company.com",
					},
					"site_filter": "PROD",
				},
			},
			wantErr: false,
			checkFunc: func(t *testing.T, p types.Probe) {
				citrixProbe := p.(*citrixProbe)
				assert.NotNil(t, citrixProbe.ddcConfig)
				assert.Equal(t, "PROD", citrixProbe.siteFilter)
				assert.Len(t, citrixProbe.ddcConfig.FallbackURLs, 2)
			},
		},
		{
			name: "with all retry configurations",
			config: map[string]interface{}{
				"base_url": "https://director.company.com",
				"auth": map[string]interface{}{
					"username": "test\\user",
					"password": "password",
				},
				"retry": map[string]interface{}{
					"max_attempts":   5,
					"backoff_factor": 3.0,
				},
				"timeout": 60,
			},
			wantErr: false,
			checkFunc: func(t *testing.T, p types.Probe) {
				citrixProbe := p.(*citrixProbe)
				assert.Equal(t, 5, citrixProbe.maxRetryAttempts)
				assert.Equal(t, 3.0, citrixProbe.retryBackoffFactor)
				assert.Equal(t, 60*time.Second, citrixProbe.timeout)
			},
		},
		{
			name: "missing password",
			config: map[string]interface{}{
				"base_url": "https://director.company.com",
				"auth": map[string]interface{}{
					"username": "test\\user",
				},
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			probe, err := NewCitrixProbe(tt.config, baseLogger)
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
				assert.NotNil(t, probe)
				if tt.checkFunc != nil {
					tt.checkFunc(t, probe)
				}
			}
		})
	}
}

func TestMetricsCollector_NewMetricsCollector(t *testing.T) {
	baseLogger := &logger.Logger{}
	mockClient := &MockCitrixClient{}

	t.Run("NewMetricsCollector creates collector", func(t *testing.T) {
		collector := NewMetricsCollector(mockClient, baseLogger)
		assert.NotNil(t, collector)
		assert.Equal(t, mockClient, collector.client)
	})

	t.Run("NewMetricsCollectorWithEnv creates collector with environment", func(t *testing.T) {
		collector := NewMetricsCollectorWithEnv(mockClient, "production", "https://citrix.example.com", baseLogger)
		assert.NotNil(t, collector)
		assert.Equal(t, mockClient, collector.client)
		assert.Equal(t, "production", collector.environment)
		assert.Equal(t, "https://citrix.example.com", collector.citrixURL)
	})
}
