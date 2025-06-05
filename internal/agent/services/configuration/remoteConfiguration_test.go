package configuration

import (
	"os"
	"testing"

	"github.com/rs/zerolog"
	"senhub-agent.go/internal/agent/services/logger"
	clientService "senhub-agent.go/internal/agent/services/server"
	"senhub-agent.go/internal/testUtils"
)

func TestValidateConfiguration(t *testing.T) {
	l := zerolog.New(os.Stderr)
	baseLogger := &l
	moduleLogger := logger.NewModuleLogger(baseLogger, "configuration.test")
	rc := &RemoteConfiguration{
		logger: moduleLogger,
	}

	testCases := []struct {
		name        string
		config      *RemoteConfigurationData
		expectError bool
	}{
		{
			name: "Valid config with duplicate probe names",
			config: &RemoteConfigurationData{
				StorageConfig: []StorageConfig{
					{Name: "senhub"},
				},
				Probes: []ProbeConfig{
					{Name: "ping_webapp", Params: map[string]interface{}{"url": "https://example1.com"}},
					{Name: "ping_webapp", Params: map[string]interface{}{"url": "https://example2.com"}},
				},
			},
			expectError: false,
		},
		{
			name: "Invalid config with empty probe name",
			config: &RemoteConfigurationData{
				StorageConfig: []StorageConfig{
					{Name: "senhub"},
				},
				Probes: []ProbeConfig{
					{Name: "", Params: map[string]interface{}{"url": "https://example.com"}},
				},
			},
			expectError: true,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			err := rc.validateConfiguration(tc.config)
			if (err != nil) != tc.expectError {
				t.Errorf("validateConfiguration() error = %v, expectError = %v", err, tc.expectError)
			}
		})
	}
}

func TestRemoteConfiguration_FetchCofiguration(t *testing.T) {
	testCases := []struct {
		name     string
		config   string
		expected bool
	}{
		{
			name:     "Valid config with all fields",
			config:   `{"storage_config":[],"probes":[], "agent": {}}`,
			expected: true,
		},
		{
			name:     "Empty config",
			config:   "",
			expected: false,
		},
		{
			name:     "Partial config",
			config:   `{"storage_config":[{"name":"senhub"}]}`,
			expected: true,
		},
		{
			name:     "Agent config",
			config:   `{"agent": { "registry_url": "http://localhost:8080", "version": "1.0.0", "update_check_interval": 3600 }}`,
			expected: true,
		},
		{
			name:     "Agent config: update_check_interval '1h'",
			config:   `{"agent": { "registry_url": "http://localhost:8080", "version": "1.0.0", "update_check_interval": "1h" }}`,
			expected: true,
		},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			testServer := testUtils.GetTestHTTPServerWithURLPath([]testUtils.TestHTTPServerURLConf{
				NewConfigurationMockServerRoute(200, tc.config, false),
			})
			defer testServer.Server.Close()

			l := zerolog.New(os.Stderr)
	baseLogger := &l
			moduleLogger := logger.NewModuleLogger(baseLogger, "configuration.test")
			httpClient := clientService.NewServer("authKey", testServer.URL, baseLogger)

			rc := &RemoteConfiguration{
				server:        httpClient,
				logger:        moduleLogger,
				eventNotifier: NewEventNotifier(baseLogger),
			}

			_, err := rc.doFetchConfiguration()
			if (err == nil) != tc.expected {
				t.Errorf("Expected %v, got %v\n%v", tc.expected, err == nil, err)
			}
		})
	}
}
