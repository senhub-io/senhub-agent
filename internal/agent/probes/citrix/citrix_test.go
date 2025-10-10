package citrix

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
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
