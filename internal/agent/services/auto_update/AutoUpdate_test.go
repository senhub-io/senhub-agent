package auto_update

import (
	"encoding/json"
	"fmt"
	"net/url"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"github.com/ybbus/httpretry"
	"senhub-agent.go/internal/agent/cliArgs"
	"senhub-agent.go/internal/agent/services/configuration"
	"senhub-agent.go/internal/agent/services/logger"
	"senhub-agent.go/internal/testUtils"
)

// Helper function to create test logger
func createTestLogger() *logger.Logger {
	l := zerolog.New( /*os.Stderr*/ nil)
	return &l
}

// Helper function to create test module logger
func createTestModuleLogger() *logger.ModuleLogger {
	return logger.NewModuleLogger(createTestLogger(), "auto_update.test")
}

func TestAutoUpdate_GetName(t *testing.T) {
	baseLogger := createTestLogger()

	remoteConfig := configuration.NewMockRemoteConfiguration("http://localhost:8080", "")

	au := NewAutoUpdate(AutoUpdateConfig{
		remoteConfig,
		baseLogger,
		false,
	})

	if au.GetName() != "AutoUpdate" {
		t.Errorf("Expected AutoUpdate, got %s", au.GetName())
	}
}

func TestAutoUpdate_ShouldUpdate(t *testing.T) {
	versionServer := testUtils.GetTestHTTPServerWithURLPath(
		NewAutoUpdateVersionRoutes([][2]string{
			{"0.9.2"},
			{"1.0.0"},
			{"1.0.1"},
			{"1.0.2"},
			{"1.1.0"},
			{"latest", "1.1.0"},
		}),
	)
	defer versionServer.Server.Close()

	testCases := []struct {
		name            string
		currentVersion  string
		expectedVersion string
		expectedResult  string
	}{
		{
			name:            "Should update",
			currentVersion:  "1.0.0",
			expectedVersion: "1.0.1",
			expectedResult:  "1.0.1",
		},
		{
			name:            "Should not update",
			currentVersion:  "1.0.0",
			expectedVersion: "1.0.0",
			expectedResult:  "1.0.0",
		},
		{
			name:            "Should revert",
			currentVersion:  "1.0.0",
			expectedVersion: "0.9.2",
			expectedResult:  "0.9.2",
		},
		{
			name:            "Should not update with empty version",
			currentVersion:  "1.0.0",
			expectedVersion: "",
			expectedResult:  "1.0.0",
		},
		{
			name:            "Should update with constraint",
			currentVersion:  "1.0.0",
			expectedVersion: ">1.0.1",
			expectedResult:  "1.1.0",
		},
		{
			name:            "Should NOT update with invalid constraint",
			currentVersion:  "1.0.0",
			expectedVersion: ">1.0.1!3%6&",
			expectedResult:  "1.0.0",
		},
		{
			name:            "Should NOT update with complex constraint",
			currentVersion:  "1.0.1",
			expectedVersion: ">=1.0.1, <1.0.3",
			expectedResult:  "1.0.1",
		},
		{
			name:            "Should update with complex constraint",
			currentVersion:  "1.0.0",
			expectedVersion: ">=1.0.1, <1.0.3",
			expectedResult:  "1.0.2",
		},
		{
			name:            "Should not update if latest is already installed",
			currentVersion:  "1.1.0",
			expectedVersion: "latest",
			expectedResult:  "1.1.0",
		},
		{
			name:            "Should update if latest is not installed",
			currentVersion:  "1.0.0",
			expectedVersion: "latest",
			expectedResult:  "1.1.0",
		},
		{
			name:            "Should NOT update with invalid version",
			currentVersion:  "1.0.0",
			expectedVersion: "beta",
			expectedResult:  "1.0.0",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			var configString string
			if tc.expectedVersion == "" {
				configString = `{ "agent": {} }`
			} else {
				configString = `{
					"agent": {
						"version": "` + tc.expectedVersion + `",
						"registry_url": "` + versionServer.URL + `"
					}
				}`
			}
			remoteConfig := configuration.NewMockRemoteConfiguration(
				"http://localhost:8000", configString)

			httpClient := httpretry.NewDefaultClient()
			au := &autoUpdate{
				configSource: remoteConfig,
				logger:       createTestModuleLogger(),
				httpClient:   httpClient,
			}

			cliArgs.Version = tc.currentVersion

			expectedVersion := au.getExpectedVersionFromConfig()
			if expectedVersion != tc.expectedResult {
				t.Errorf("Expected %s, got %s", tc.expectedResult, expectedVersion)
			}
		})
	}
}

func TestAutoUpdate_getExpectedVersion_WithFailingServer(t *testing.T) {
	versionServer := testUtils.GetTestHTTPServerWithURLPath([]testUtils.TestHTTPServerURLConf{
		NewAutoUpdateVersionMetadataRoute("0.9.2"),
		NewAutoUpdateVersionMetadataRoute("1.0.0"),
		NewAutoUpdateVersionMetadataRoute("1.0.1"),
		NewAutoUpdateVersionMetadataRoute("1.0.2"),
		NewAutoUpdateVersionMetadataRoute("1.1.0"),
		NewAutoUpdateVersionMetadataRoute("latest", "1.1.0"),
	})
	defer versionServer.Server.Close()

	testCases := []struct {
		name            string
		currentVersion  string
		expectedVersion string
		expectedResult  string
	}{
		{
			name:            "Should not update range with no list on server",
			currentVersion:  "1.0.0",
			expectedVersion: ">1.0.1",
			expectedResult:  "1.0.0",
		},
		{
			name:            "Should update if list is failing but version exists",
			currentVersion:  "1.0.0",
			expectedVersion: "1.0.1",
			expectedResult:  "1.0.1",
		},
		{
			name:            "Should not update with latest when list fails",
			currentVersion:  "1.0.0",
			expectedVersion: "latest",
			expectedResult:  "1.0.0",
		},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			configString := `{
				"agent": {
					"version": "` + tc.expectedVersion + `",
					"registry_url": "` + versionServer.URL + `"
				}
			}`
			remoteConfig := configuration.NewMockRemoteConfiguration(
				"http://localhost:8000", configString)

			httpClient := httpretry.NewDefaultClient()
			au := &autoUpdate{
				configSource: remoteConfig,
				logger:       createTestModuleLogger(),
				httpClient:   httpClient,
			}

			cliArgs.Version = tc.currentVersion

			expectedVersion := au.getExpectedVersionFromConfig()
			if expectedVersion != tc.expectedResult {
				t.Errorf("Expected %s, got %s", tc.expectedResult, expectedVersion)
			}
		})
	}
}

func TestIsBetaVersion(t *testing.T) {
	testCases := []struct {
		name     string
		version  string
		expected bool
	}{
		{
			name:     "Regular version",
			version:  "1.0.0",
			expected: false,
		},
		{
			name:     "Beta version",
			version:  "1.0.0-beta",
			expected: true,
		},
		{
			name:     "Short version",
			version:  "1.0",
			expected: false,
		},
		{
			name:     "Empty string",
			version:  "",
			expected: false,
		},
		{
			name:     "Other suffix",
			version:  "1.0.0-alpha",
			expected: false,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			result := IsBetaVersion(tc.version)
			if result != tc.expected {
				t.Errorf("Expected %v, got %v", tc.expected, result)
			}
		})
	}
}

func TestAutoUpdate_GetBinaryUrl(t *testing.T) {
	testCases := []struct {
		name           string
		registryUrl    string
		version        string
		os             string
		arch           string
		expectedResult string
	}{
		{
			name:           "Regular version",
			registryUrl:    "https://registry.example.com",
			version:        "1.0.0",
			os:             "linux",
			arch:           "amd64",
			expectedResult: "https://registry.example.com/download/1.0.0/senhub-agent-linux-amd64.zip",
		},
		{
			name:           "Beta version",
			registryUrl:    "https://registry.example.com",
			version:        "1.0.0-beta",
			os:             "linux",
			arch:           "amd64",
			expectedResult: "https://registry.example.com/download/1.0.0-beta/senhub-agent-linux-amd64.zip",
		},
		{
			name:           "Windows beta version",
			registryUrl:    "https://registry.example.com",
			version:        "1.0.0-beta",
			os:             "windows",
			arch:           "amd64",
			expectedResult: "https://registry.example.com/download/1.0.0-beta/senhub-agent-windows-amd64.zip",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			remoteConfig := configuration.NewMockRemoteConfiguration(
				"http://localhost:8000", "")

			httpClient := httpretry.NewDefaultClient()
			au := &autoUpdate{
				configSource: remoteConfig,
				logger:       createTestModuleLogger(),
				httpClient:   httpClient,
			}

			// Instead of trying to modify runtime.GOOS/GOARCH which is not possible in Go,
			// we'll manually construct the URL that would be generated
			binaryName := au.getBinaryNameForOptions(tc.os, tc.arch)

			// Get the formatted version
			formattedVersion := FormatVersionForUrl(tc.version)

			// Always use the same download path pattern, regardless of beta or not
			downloadPath := fmt.Sprintf(VERSION_BINARY_PATH, formattedVersion, binaryName)

			// Join with the registry URL
			result, err := url.JoinPath(tc.registryUrl, downloadPath)

			if err != nil {
				t.Errorf("Unexpected error: %v", err)
			}
			if result != tc.expectedResult {
				t.Errorf("Expected %s, got %s", tc.expectedResult, result)
			}
		})
	}
}

func TestAutoUpdate_GetBinaryName(t *testing.T) {
	testCases := []struct {
		name           string
		os             string
		arch           string
		expectedResult string
	}{
		{
			name:           "Linux amd64",
			os:             "linux",
			arch:           "amd64",
			expectedResult: "senhub-agent-linux-amd64.zip",
		},
		{
			name:           "Window amd64",
			os:             "windows",
			arch:           "amd64",
			expectedResult: "senhub-agent-windows-amd64.zip",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			remoteConfig := configuration.NewMockRemoteConfiguration(
				"http://localhost:8000", "")

			httpClient := httpretry.NewDefaultClient()
			au := &autoUpdate{
				configSource: remoteConfig,
				logger:       createTestModuleLogger(),
				httpClient:   httpClient,
			}
			result := au.getBinaryNameForOptions(
				tc.os,
				tc.arch,
			)
			if result != tc.expectedResult {
				t.Errorf("Expected %s, got %s", tc.expectedResult, result)
			}
		})
	}
}

func TestAutoUpdate_GetUpdateCheckInterval(t *testing.T) {
	testCases := []struct {
		name           string
		interval       any
		expectedResult time.Duration
	}{
		{
			name:           "1h",
			interval:       "1h",
			expectedResult: time.Hour,
		},
		{
			name:           "1m",
			interval:       "1m",
			expectedResult: time.Minute,
		},
		{
			name:           "number",
			interval:       3600,
			expectedResult: time.Hour,
		},
		{
			name:           "default",
			interval:       nil,
			expectedResult: time.Hour,
		},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			configInterval, _ := json.Marshal(tc.interval)
			remoteConfig := configuration.NewMockRemoteConfiguration(
				"http://localhost:8000", `
				{
					"agent": {
						"update_check_interval": `+string(configInterval)+`
					}
				}
				`)

			httpClient := httpretry.NewDefaultClient()
			au := &autoUpdate{
				configSource: remoteConfig,
				logger:       createTestModuleLogger(),
				httpClient:   httpClient,
			}
			result := au.GetUpdateCheckInterval()
			if result != tc.expectedResult {
				t.Errorf("Expected %v, got %v", tc.expectedResult, result)
			}
		})
	}
}
