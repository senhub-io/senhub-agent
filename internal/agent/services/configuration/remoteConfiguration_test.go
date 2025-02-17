package configuration

import (
	"os"
	"testing"

	"github.com/rs/zerolog"
	clientService "senhub-agent.go/internal/agent/services/server"
	"senhub-agent.go/internal/testUtils"
)

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
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			testServer := testUtils.GetTestHTTPServerWithURLPath([]testUtils.TestHTTPServerURLConf{
				NewConfigurationMockServerRoute(200, tc.config, false),
			})
			defer testServer.Server.Close()

			logger := zerolog.New(os.Stderr)
			httpClient := clientService.NewServer("authKey", testServer.URL, &logger)

			rc := &RemoteConfiguration{
				server:        httpClient,
				logger:        &logger,
				eventNotifier: NewEventNotifier(&logger),
			}

			_, err := rc.doFetchConfiguration()
			if (err == nil) != tc.expected {
				t.Errorf("Expected %v, got %v\n%v", tc.expected, err == nil, err)
			}
		})
	}
}
