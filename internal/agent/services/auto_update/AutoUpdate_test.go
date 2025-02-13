package auto_update

import (
	"testing"

	"github.com/rs/zerolog"
	"github.com/ybbus/httpretry"
	"senhub-agent.go/internal/agent/cliArgs"
	"senhub-agent.go/internal/agent/services/configuration"
	"senhub-agent.go/internal/testUtils"
)

func TestAutoUpdate_GetName(t *testing.T) {
	logger := zerolog.New( /*os.Stderr*/ nil)

	remoteConfig := configuration.NewMockRemoteConfiguration("http://localhost:8080", "")

	au := NewAutoUpdate(AutoUpdateConfig{
		remoteConfig,
		&logger,
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
			logger := zerolog.New( /*os.Stderr*/ nil)
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
				remoteConfig,
				&logger,
				httpClient,
			}

			cliArgs.Version = tc.currentVersion

			expectedVersion := au.getExpectedVersion()
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
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			logger := zerolog.New( /*os.Stderr*/ nil)
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
				remoteConfig,
				&logger,
				httpClient,
			}

			cliArgs.Version = tc.currentVersion

			expectedVersion := au.getExpectedVersion()
			if expectedVersion != tc.expectedResult {
				t.Errorf("Expected %s, got %s", tc.expectedResult, expectedVersion)
			}
		})
	}
}
