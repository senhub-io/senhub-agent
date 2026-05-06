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
			name: "valid configuration with director_url",
			config: map[string]interface{}{
				"director_url": "https://citrix-director.example.com",
				"auth": map[string]interface{}{
					"username": "test\\user",
					"password": "password",
				},
			},
			wantErr: false,
		},
		{
			name: "missing base_url and director_url",
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
	mockClient.On("GetLoadIndexes", mock.Anything).Return([]LoadIndex{
		{Id: 1, MachineId: "m1", Cpu: 5000, Memory: 3000, Disk: 1000, Network: 500, SessionCount: 2000, EffectiveLoadIndex: 4000},
		{Id: 2, MachineId: "m2", Cpu: 9000, Memory: 8500, Disk: 2000, Network: 1000, SessionCount: 7000, EffectiveLoadIndex: 8500},
	}, nil)

	// Test collection using the new specialized modules
	dataPoints, err := collector.CollectMetrics(context.Background(), timestamp)

	// Verify results
	assert.NoError(t, err)
	assert.NotEmpty(t, dataPoints)

	// Check that we have metrics from different categories
	var hasInfraMetrics, hasSessionMetrics, hasLogonMetrics, hasFailureMetrics, hasLoadMetrics bool
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
				case "load_index":
					hasLoadMetrics = true
				}
			}
		}
	}

	assert.True(t, hasInfraMetrics, "Should have infrastructure metrics")
	assert.True(t, hasSessionMetrics, "Should have session metrics")
	assert.True(t, hasLogonMetrics, "Should have logon breakdown metrics (even if zero)")
	assert.True(t, hasFailureMetrics, "Should have failure metrics")
	assert.True(t, hasLoadMetrics, "Should have load index metrics")

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

func (m *MockCitrixClient) GetLoadIndexes(ctx context.Context) ([]LoadIndex, error) {
	args := m.Called(ctx)
	return args.Get(0).([]LoadIndex), args.Error(1)
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
	expected := []string{"senhub", "prtg", "http", "otlp"}

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
		mockClient.On("GetLoadIndexes", mock.Anything).Return([]LoadIndex{}, nil)

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

	t.Run("GetLoadIndexes delegates to original", func(t *testing.T) {
		expectedIndexes := []LoadIndex{{Id: 1, MachineId: "m1", EffectiveLoadIndex: 5000}}
		mockClient.On("GetLoadIndexes", mock.Anything).Return(expectedIndexes, nil).Once()
		indexes, err := filteredClient.GetLoadIndexes(ctx)
		assert.NoError(t, err)
		assert.Equal(t, expectedIndexes, indexes)
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

// TestCollectInfrastructureMetrics_PhantomFiltering verifies that machines without
// DnsName and MachineName are excluded from infrastructure metrics (phantom entries
// in Director SQL database that inflate machine counts).
func TestCollectInfrastructureMetrics_PhantomFiltering(t *testing.T) {
	baseLogger := &logger.Logger{}
	mockClient := &MockCitrixClient{}

	// Return a mix of valid machines and phantom entries
	mockClient.On("GetMachines", mock.Anything, mock.Anything).Return([]Machine{
		// Valid machines in a Delivery Group
		{MachineId: "m1", MachineName: "VDI-001", DnsName: "vdi-001.corp.local", DesktopGroupId: "dg1", RegistrationState: RegistrationStateRegistered, FaultState: FaultStateNone},
		{MachineId: "m2", MachineName: "VDI-002", DnsName: "vdi-002.corp.local", DesktopGroupId: "dg1", RegistrationState: RegistrationStateRegistered, FaultState: FaultStateNone},
		{MachineId: "m3", MachineName: "VDI-003", DnsName: "vdi-003.corp.local", DesktopGroupId: "dg1", RegistrationState: RegistrationStateUnregistered, FaultState: FaultStateNone},
		// Phantom: no DnsName, no MachineName (ghost entries in SQL)
		{MachineId: "m4", MachineName: "", DnsName: "", DesktopGroupId: "", RegistrationState: RegistrationStateUnregistered, FaultState: FaultStateNone},
		{MachineId: "m5", MachineName: "", DnsName: "", DesktopGroupId: "", RegistrationState: RegistrationStateUnregistered, FaultState: FaultStateNone},
		// Valid: has MachineName but no DnsName (should be kept)
		{MachineId: "m6", MachineName: "LEGACY-001", DnsName: "", DesktopGroupId: "dg2", RegistrationState: RegistrationStateRegistered, FaultState: FaultStateNone},
	}, nil)

	collector := NewMetricsCollector(mockClient, baseLogger)
	ctx := context.Background()

	metrics, err := collector.CollectInfrastructureMetrics(ctx, time.Now(), nil)
	assert.NoError(t, err)
	assert.NotNil(t, metrics)

	// Find the machines_total metric
	var totalMachines float32
	for _, m := range metrics {
		if m.Name == MetricMachinesTotal {
			for _, tag := range m.Tags {
				if tag.Key == "metric_type" && tag.Value == "infrastructure" {
					totalMachines = m.Value
				}
			}
		}
	}

	// Should be 4 (3 valid VDIs + 1 legacy), not 6 (which includes 2 phantoms)
	assert.Equal(t, float32(4), totalMachines, "Phantom machines (no DnsName AND no MachineName) should be excluded from total count")
}

// TestCollectInfrastructureMetrics_RegisteredCounts verifies registered/unregistered breakdown
func TestCollectInfrastructureMetrics_RegisteredCounts(t *testing.T) {
	baseLogger := &logger.Logger{}
	mockClient := &MockCitrixClient{}

	mockClient.On("GetMachines", mock.Anything, mock.Anything).Return([]Machine{
		{MachineId: "m1", MachineName: "VDI-001", DnsName: "vdi-001.local", DesktopGroupId: "dg1", RegistrationState: RegistrationStateRegistered, FaultState: FaultStateNone},
		{MachineId: "m2", MachineName: "VDI-002", DnsName: "vdi-002.local", DesktopGroupId: "dg1", RegistrationState: RegistrationStateRegistered, FaultState: FaultStateNone},
		{MachineId: "m3", MachineName: "VDI-003", DnsName: "vdi-003.local", DesktopGroupId: "dg1", RegistrationState: RegistrationStateUnregistered, FaultState: FaultStateNone},
		{MachineId: "m4", MachineName: "VDI-004", DnsName: "vdi-004.local", DesktopGroupId: "dg1", RegistrationState: RegistrationStateAgentError, FaultState: FaultStateUnregistered},
	}, nil)

	collector := NewMetricsCollector(mockClient, baseLogger)
	ctx := context.Background()

	metrics, err := collector.CollectInfrastructureMetrics(ctx, time.Now(), nil)
	assert.NoError(t, err)

	metricMap := make(map[string]float32)
	for _, m := range metrics {
		for _, tag := range m.Tags {
			if tag.Key == "metric_type" && tag.Value == "infrastructure" {
				metricMap[m.Name] = m.Value
			}
		}
	}

	assert.Equal(t, float32(4), metricMap[MetricMachinesTotal])
	assert.Equal(t, float32(2), metricMap[MetricMachinesRegistered])
	assert.Equal(t, float32(2), metricMap[MetricMachinesUnregistered]) // Unregistered + AgentError
}

// MockDeliveryControllerClient implements DeliveryControllerClient for testing
type MockDeliveryControllerClient struct {
	mock.Mock
}

func (m *MockDeliveryControllerClient) GetMe(ctx context.Context) (*DDCMeResponse, error) {
	args := m.Called(ctx)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*DDCMeResponse), args.Error(1)
}

func (m *MockDeliveryControllerClient) GetMachinesBySite(ctx context.Context, siteName string) ([]string, error) {
	args := m.Called(ctx, siteName)
	return args.Get(0).([]string), args.Error(1)
}

func (m *MockDeliveryControllerClient) GetMachinesDetailedBySite(ctx context.Context, siteName string) ([]DDCMachine, error) {
	args := m.Called(ctx, siteName)
	return args.Get(0).([]DDCMachine), args.Error(1)
}

func (m *MockDeliveryControllerClient) GetDeliveryGroupsBySite(ctx context.Context, siteName string) ([]DDCDeliveryGroup, error) {
	args := m.Called(ctx, siteName)
	return args.Get(0).([]DDCDeliveryGroup), args.Error(1)
}

func (m *MockDeliveryControllerClient) GetControllersBySite(ctx context.Context, siteName string) ([]DDCController, error) {
	args := m.Called(ctx, siteName)
	return args.Get(0).([]DDCController), args.Error(1)
}

func (m *MockDeliveryControllerClient) GetSessionsBySite(ctx context.Context, siteName string) ([]DDCSession, error) {
	args := m.Called(ctx, siteName)
	return args.Get(0).([]DDCSession), args.Error(1)
}

func (m *MockDeliveryControllerClient) GetSiteDetails(ctx context.Context, siteName string) (*DDCSiteDetails, error) {
	args := m.Called(ctx, siteName)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*DDCSiteDetails), args.Error(1)
}

func (m *MockDeliveryControllerClient) GetLicenseInfo(ctx context.Context, siteName string) (*DDCSiteLicenseInfo, error) {
	args := m.Called(ctx, siteName)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*DDCSiteLicenseInfo), args.Error(1)
}

func (m *MockDeliveryControllerClient) TestConnectivity(ctx context.Context) error {
	args := m.Called(ctx)
	return args.Error(0)
}

// === License Metrics Tests ===

func TestCollectLicenseMetrics_NilDDCAndEmptyLicenseURL(t *testing.T) {
	baseLogger := &logger.Logger{}
	mockClient := &MockCitrixClient{}

	collector := NewMetricsCollector(mockClient, baseLogger)
	// ddcClient is nil by default, licenseServerURL is empty

	metrics, err := collector.CollectLicenseMetrics(context.Background(), time.Now())
	assert.NoError(t, err)
	assert.Nil(t, metrics, "Should return nil when no DDC client and no license server URL")
}

func TestCollectLicenseMetrics_DDCReturnsValidData(t *testing.T) {
	baseLogger := &logger.Logger{}
	mockClient := &MockCitrixClient{}
	mockDDC := &MockDeliveryControllerClient{}

	collector := NewMetricsCollector(mockClient, baseLogger)
	collector.ddcClient = mockDDC
	collector.siteFilter = "TestSite"

	mockDDC.On("GetLicenseInfo", mock.Anything, "TestSite").Return(&DDCSiteLicenseInfo{
		LicensedSessionsActive:         42,
		PeakConcurrentLicenseUsers:     100,
		TotalUniqueLicenseUsers:        200,
		LicenseGraceSessionsRemaining: 500,
		LicensingGracePeriodActive:     false,
		LicensingGraceHoursLeft:        0,
	}, nil)

	metrics, err := collector.CollectLicenseMetrics(context.Background(), time.Now())
	assert.NoError(t, err)
	assert.Len(t, metrics, 6, "Should return 6 license metrics")

	// Verify metric names and values
	metricMap := make(map[string]float32)
	for _, m := range metrics {
		metricMap[m.Name] = m.Value
	}
	assert.Equal(t, float32(42), metricMap[MetricLicenseSessionsActive])
	assert.Equal(t, float32(100), metricMap[MetricLicensePeakConcurrent])
	assert.Equal(t, float32(200), metricMap[MetricLicenseUniqueUsers])
	assert.Equal(t, float32(500), metricMap[MetricLicenseGraceSessionsLeft])
	assert.Equal(t, float32(0), metricMap[MetricLicenseGracePeriodActive])
	assert.Equal(t, float32(0), metricMap[MetricLicenseGraceHoursLeft])

	mockDDC.AssertExpectations(t)
}

func TestCollectLicenseMetrics_DDCReturnsAllZeros(t *testing.T) {
	baseLogger := &logger.Logger{}
	mockClient := &MockCitrixClient{}
	mockDDC := &MockDeliveryControllerClient{}

	collector := NewMetricsCollector(mockClient, baseLogger)
	collector.ddcClient = mockDDC
	collector.siteFilter = "TestSite"

	// All zeros means CVAD version doesn't support these fields - should fall back
	mockDDC.On("GetLicenseInfo", mock.Anything, "TestSite").Return(&DDCSiteLicenseInfo{
		LicensedSessionsActive:         0,
		PeakConcurrentLicenseUsers:     0,
		TotalUniqueLicenseUsers:        0,
		LicenseGraceSessionsRemaining: 0,
		LicensingGracePeriodActive:     false,
		LicensingGraceHoursLeft:        0,
	}, nil)

	// No license server URL either, so should return nil
	metrics, err := collector.CollectLicenseMetrics(context.Background(), time.Now())
	assert.NoError(t, err)
	assert.Nil(t, metrics, "Should return nil when DDC returns all zeros and no license server is configured")

	mockDDC.AssertExpectations(t)
}

func TestCollectLicenseMetrics_DDCError_NoFallback(t *testing.T) {
	baseLogger := &logger.Logger{}
	mockClient := &MockCitrixClient{}
	mockDDC := &MockDeliveryControllerClient{}

	collector := NewMetricsCollector(mockClient, baseLogger)
	collector.ddcClient = mockDDC
	collector.siteFilter = "TestSite"
	// No license server URL

	mockDDC.On("GetLicenseInfo", mock.Anything, "TestSite").Return(nil, assert.AnError)

	metrics, err := collector.CollectLicenseMetrics(context.Background(), time.Now())
	assert.NoError(t, err)
	assert.Nil(t, metrics, "Should return nil when DDC errors and no license server is configured")

	mockDDC.AssertExpectations(t)
}

// === collectLicenseFromDDC Tests ===

func TestCollectLicenseFromDDC_ValidData(t *testing.T) {
	baseLogger := &logger.Logger{}
	mockClient := &MockCitrixClient{}
	mockDDC := &MockDeliveryControllerClient{}

	collector := NewMetricsCollector(mockClient, baseLogger)
	collector.ddcClient = mockDDC
	collector.siteFilter = "ProdSite"

	mockDDC.On("GetLicenseInfo", mock.Anything, "ProdSite").Return(&DDCSiteLicenseInfo{
		LicensedSessionsActive:         10,
		PeakConcurrentLicenseUsers:     25,
		TotalUniqueLicenseUsers:        50,
		LicenseGraceSessionsRemaining: 100,
		LicensingGracePeriodActive:     true,
		LicensingGraceHoursLeft:        168,
	}, nil)

	timestamp := time.Now()
	metrics, err := collector.collectLicenseFromDDC(context.Background(), timestamp)
	assert.NoError(t, err)
	assert.Len(t, metrics, 6)

	metricMap := make(map[string]float32)
	for _, m := range metrics {
		metricMap[m.Name] = m.Value
		// Verify all metrics have the licensing tag
		assert.Len(t, m.Tags, 1)
		assert.Equal(t, "metric_type", m.Tags[0].Key)
		assert.Equal(t, "licensing", m.Tags[0].Value)
		// Verify timestamp
		assert.Equal(t, timestamp, m.Timestamp)
	}

	assert.Equal(t, float32(10), metricMap[MetricLicenseSessionsActive])
	assert.Equal(t, float32(25), metricMap[MetricLicensePeakConcurrent])
	assert.Equal(t, float32(50), metricMap[MetricLicenseUniqueUsers])
	assert.Equal(t, float32(100), metricMap[MetricLicenseGraceSessionsLeft])
	assert.Equal(t, float32(1), metricMap[MetricLicenseGracePeriodActive], "Grace period active should be 1")
	assert.Equal(t, float32(168), metricMap[MetricLicenseGraceHoursLeft])

	mockDDC.AssertExpectations(t)
}

func TestCollectLicenseFromDDC_AllZeroValues(t *testing.T) {
	baseLogger := &logger.Logger{}
	mockClient := &MockCitrixClient{}
	mockDDC := &MockDeliveryControllerClient{}

	collector := NewMetricsCollector(mockClient, baseLogger)
	collector.ddcClient = mockDDC
	collector.siteFilter = "TestSite"

	mockDDC.On("GetLicenseInfo", mock.Anything, "TestSite").Return(&DDCSiteLicenseInfo{}, nil)

	metrics, err := collector.collectLicenseFromDDC(context.Background(), time.Now())
	assert.NoError(t, err)
	assert.Nil(t, metrics, "Should return nil when all license values are zero")

	mockDDC.AssertExpectations(t)
}

// === buildLicenseMetrics Tests ===

func TestBuildLicenseMetrics(t *testing.T) {
	baseLogger := &logger.Logger{}
	mockClient := &MockCitrixClient{}
	collector := NewMetricsCollector(mockClient, baseLogger)

	timestamp := time.Date(2025, 6, 15, 10, 30, 0, 0, time.UTC)

	metrics := collector.buildLicenseMetrics(timestamp, 15, 30, 45, 200, true, 72)

	assert.Len(t, metrics, 6, "buildLicenseMetrics should return exactly 6 datapoints")

	expectedMetrics := map[string]float32{
		MetricLicenseSessionsActive:    15,
		MetricLicensePeakConcurrent:    30,
		MetricLicenseUniqueUsers:       45,
		MetricLicenseGraceSessionsLeft: 200,
		MetricLicenseGracePeriodActive: 1, // true -> 1
		MetricLicenseGraceHoursLeft:    72,
	}

	for _, m := range metrics {
		expected, ok := expectedMetrics[m.Name]
		assert.True(t, ok, "Unexpected metric name: %s", m.Name)
		assert.Equal(t, expected, m.Value, "Metric %s has wrong value", m.Name)
		assert.Equal(t, timestamp, m.Timestamp, "Metric %s has wrong timestamp", m.Name)
		assert.Len(t, m.Tags, 1)
		assert.Equal(t, "metric_type", m.Tags[0].Key)
		assert.Equal(t, "licensing", m.Tags[0].Value)
	}
}

func TestBuildLicenseMetrics_GracePeriodInactive(t *testing.T) {
	baseLogger := &logger.Logger{}
	mockClient := &MockCitrixClient{}
	collector := NewMetricsCollector(mockClient, baseLogger)

	metrics := collector.buildLicenseMetrics(time.Now(), 0, 0, 0, 0, false, 0)

	metricMap := make(map[string]float32)
	for _, m := range metrics {
		metricMap[m.Name] = m.Value
	}

	assert.Equal(t, float32(0), metricMap[MetricLicenseGracePeriodActive], "Grace period inactive should be 0")
}

// === truncateString Tests ===

func TestTruncateString(t *testing.T) {
	t.Run("short string unchanged", func(t *testing.T) {
		result := truncateString("hello", 10)
		assert.Equal(t, "hello", result)
	})

	t.Run("exact length unchanged", func(t *testing.T) {
		result := truncateString("hello", 5)
		assert.Equal(t, "hello", result)
	})

	t.Run("long string truncated with ellipsis", func(t *testing.T) {
		result := truncateString("hello world, this is a long string", 10)
		assert.Equal(t, "hello worl...", result)
		assert.Len(t, result, 13) // 10 + "..."
	})

	t.Run("empty string unchanged", func(t *testing.T) {
		result := truncateString("", 10)
		assert.Equal(t, "", result)
	})
}

// === Load Metrics Tests ===

func TestCollectLoadMetrics_ValidData(t *testing.T) {
	baseLogger := &logger.Logger{}
	mockClient := &MockCitrixClient{}

	mockClient.On("GetLoadIndexes", mock.Anything).Return([]LoadIndex{
		{Id: 1, MachineId: "m1", Cpu: 5000, Memory: 3000, Disk: 1000, Network: 500, SessionCount: 2000, EffectiveLoadIndex: 4000},
		{Id: 2, MachineId: "m2", Cpu: 9000, Memory: 8500, Disk: 2000, Network: 1000, SessionCount: 7000, EffectiveLoadIndex: 8500},
	}, nil)

	collector := NewMetricsCollector(mockClient, baseLogger)
	timestamp := time.Now()

	metrics, err := collector.CollectLoadMetrics(context.Background(), timestamp)
	assert.NoError(t, err)
	assert.Len(t, metrics, 7, "Should return 7 load index metrics")

	metricMap := make(map[string]float32)
	for _, m := range metrics {
		metricMap[m.Name] = m.Value
		assert.Equal(t, "load_index", m.Tags[0].Value)
	}

	// Average of (5000+9000)/2 = 7000, /100 = 70.0
	assert.Equal(t, float32(70), metricMap[MetricLoadIndexCpu])
	// Average of (3000+8500)/2 = 5750, /100 = 57.5
	assert.Equal(t, float32(57.5), metricMap[MetricLoadIndexMemory])
	// m2 has EffectiveLoadIndex 8500 >= LoadIndexOverloaded (8000), so 1 overloaded
	assert.Equal(t, float32(1), metricMap[MetricLoadOverloaded])

	mockClient.AssertExpectations(t)
}

func TestCollectLoadMetrics_EmptyData(t *testing.T) {
	baseLogger := &logger.Logger{}
	mockClient := &MockCitrixClient{}

	mockClient.On("GetLoadIndexes", mock.Anything).Return([]LoadIndex{}, nil)

	collector := NewMetricsCollector(mockClient, baseLogger)

	metrics, err := collector.CollectLoadMetrics(context.Background(), time.Now())
	assert.NoError(t, err)
	assert.Len(t, metrics, 7, "Should return 7 zero-value load metrics")

	for _, m := range metrics {
		assert.Equal(t, float32(0), m.Value, "Metric %s should be 0", m.Name)
	}

	mockClient.AssertExpectations(t)
}

func TestCollectLoadMetrics_EndpointError(t *testing.T) {
	baseLogger := &logger.Logger{}
	mockClient := &MockCitrixClient{}

	mockClient.On("GetLoadIndexes", mock.Anything).Return([]LoadIndex{}, assert.AnError)

	collector := NewMetricsCollector(mockClient, baseLogger)

	metrics, err := collector.CollectLoadMetrics(context.Background(), time.Now())
	assert.NoError(t, err, "Should not error on endpoint failure")
	assert.Nil(t, metrics, "Should return nil when endpoint is unavailable")

	mockClient.AssertExpectations(t)
}

// === Per-Component Config Format Tests ===

func TestNewCitrixProbe_NewFormat_DirectorOnly(t *testing.T) {
	baseLogger := &logger.Logger{}

	config := map[string]interface{}{
		"interval": 120,
		"timeout":  30,
		"director": map[string]interface{}{
			"url":        "https://director.example.com",
			"verify_ssl": false,
			"auth": map[string]interface{}{
				"username": "DOMAIN\\svc_director",
				"password": "pass_director",
			},
		},
	}

	probe, err := NewCitrixProbe(config, baseLogger)
	assert.NoError(t, err)
	assert.NotNil(t, probe)

	cp := probe.(*citrixProbe)
	assert.NotNil(t, cp.directorConfig)
	assert.Equal(t, "https://director.example.com", cp.directorConfig.URL)
	assert.Equal(t, false, cp.directorConfig.VerifySSL)
	assert.Equal(t, "ntlm", cp.directorConfig.Auth.Method)
	assert.Equal(t, "DOMAIN\\svc_director", cp.directorConfig.Auth.Username)
	assert.Equal(t, "pass_director", cp.directorConfig.Auth.Password)
	assert.Nil(t, cp.ddcConfig)
	assert.Nil(t, cp.licenseConfig)
}

func TestNewCitrixProbe_NewFormat_DirectorMissingAuth(t *testing.T) {
	baseLogger := &logger.Logger{}

	config := map[string]interface{}{
		"director": map[string]interface{}{
			"url": "https://director.example.com",
		},
	}

	_, err := NewCitrixProbe(config, baseLogger)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "director.auth.username")
}

func TestNewCitrixProbe_NewFormat_DirectorMissingURL(t *testing.T) {
	baseLogger := &logger.Logger{}

	config := map[string]interface{}{
		"director": map[string]interface{}{
			"auth": map[string]interface{}{
				"username": "user",
				"password": "pass",
			},
		},
	}

	_, err := NewCitrixProbe(config, baseLogger)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "director.url")
}

func TestNewCitrixProbe_NewFormat_AllComponents(t *testing.T) {
	baseLogger := &logger.Logger{}

	config := map[string]interface{}{
		"interval": 120,
		"timeout":  30,
		"director": map[string]interface{}{
			"url":        "https://director.example.com",
			"verify_ssl": false,
			"fallback_urls": []interface{}{
				"https://director2.example.com",
			},
			"auth": map[string]interface{}{
				"username": "DOMAIN\\svc_director",
				"password": "pass_director",
			},
		},
		"delivery_controller": map[string]interface{}{
			"url":        "https://ddc1.example.com",
			"verify_ssl": false,
			"fallback_urls": []interface{}{
				"https://ddc2.example.com",
			},
			"site_filter": "PROD",
			"auth": map[string]interface{}{
				"username": "DOMAIN\\svc_ddc",
				"password": "pass_ddc",
			},
		},
		"license_server": map[string]interface{}{
			"url":        "https://license:8083",
			"verify_ssl": false,
			"auth": map[string]interface{}{
				"username": "DOMAIN\\svc_lic",
				"password": "pass_lic",
			},
		},
	}

	probe, err := NewCitrixProbe(config, baseLogger)
	assert.NoError(t, err)
	assert.NotNil(t, probe)

	cp := probe.(*citrixProbe)

	// Director
	assert.NotNil(t, cp.directorConfig)
	assert.Equal(t, "https://director.example.com", cp.directorConfig.URL)
	assert.Equal(t, "ntlm", cp.directorConfig.Auth.Method)
	assert.Equal(t, "DOMAIN\\svc_director", cp.directorConfig.Auth.Username)
	assert.Len(t, cp.directorConfig.FallbackURLs, 1)

	// DDC
	assert.NotNil(t, cp.ddcConfig)
	assert.Equal(t, "https://ddc1.example.com", cp.ddcConfig.URL)
	assert.Equal(t, "basic", cp.ddcConfig.Auth.Method)
	assert.Equal(t, "DOMAIN\\svc_ddc", cp.ddcConfig.Auth.Username)
	assert.Equal(t, "pass_ddc", cp.ddcConfig.Auth.Password)
	assert.Equal(t, "PROD", cp.siteFilter)
	assert.Len(t, cp.ddcConfig.FallbackURLs, 1)

	// License Server
	assert.NotNil(t, cp.licenseConfig)
	assert.Equal(t, "https://license:8083", cp.licenseConfig.URL)
	assert.Equal(t, "basic", cp.licenseConfig.Auth.Method)
	assert.Equal(t, "DOMAIN\\svc_lic", cp.licenseConfig.Auth.Username)
	assert.Equal(t, "pass_lic", cp.licenseConfig.Auth.Password)
}

func TestNewCitrixProbe_NewFormat_DDCInheritsDirectorAuth(t *testing.T) {
	baseLogger := &logger.Logger{}

	config := map[string]interface{}{
		"director": map[string]interface{}{
			"url": "https://director.example.com",
			"auth": map[string]interface{}{
				"username": "DOMAIN\\shared_user",
				"password": "shared_pass",
			},
		},
		"delivery_controller": map[string]interface{}{
			"url":         "https://ddc.example.com",
			"site_filter": "SITE1",
			// No auth block - should inherit from director
		},
	}

	probe, err := NewCitrixProbe(config, baseLogger)
	assert.NoError(t, err)

	cp := probe.(*citrixProbe)
	assert.NotNil(t, cp.ddcConfig)
	assert.Equal(t, "basic", cp.ddcConfig.Auth.Method)
	assert.Equal(t, "DOMAIN\\shared_user", cp.ddcConfig.Auth.Username)
	assert.Equal(t, "shared_pass", cp.ddcConfig.Auth.Password)
}

func TestNewCitrixProbe_OldFormat_ProducesSameInternalState(t *testing.T) {
	baseLogger := &logger.Logger{}

	// Old format config
	oldConfig := map[string]interface{}{
		"base_url": "https://director.example.com",
		"interval": 120,
		"timeout":  30,
		"auth": map[string]interface{}{
			"username": "DOMAIN\\user",
			"password": "pass",
		},
		"tls": map[string]interface{}{
			"verify_ssl": false,
		},
		"delivery_controller": map[string]interface{}{
			"url":         "https://ddc.example.com",
			"site_filter": "PROD",
		},
	}

	probe, err := NewCitrixProbe(oldConfig, baseLogger)
	assert.NoError(t, err)

	cp := probe.(*citrixProbe)

	// Director config should be populated from flat fields
	assert.NotNil(t, cp.directorConfig)
	assert.Equal(t, "https://director.example.com", cp.directorConfig.URL)
	assert.Equal(t, "ntlm", cp.directorConfig.Auth.Method)
	assert.Equal(t, "DOMAIN\\user", cp.directorConfig.Auth.Username)
	assert.Equal(t, "pass", cp.directorConfig.Auth.Password)
	assert.Equal(t, false, cp.directorConfig.VerifySSL)

	// DDC config should use global auth
	assert.NotNil(t, cp.ddcConfig)
	assert.Equal(t, "https://ddc.example.com", cp.ddcConfig.URL)
	assert.Equal(t, "basic", cp.ddcConfig.Auth.Method)
	assert.Equal(t, "DOMAIN\\user", cp.ddcConfig.Auth.Username)
	assert.Equal(t, "PROD", cp.siteFilter)
}
