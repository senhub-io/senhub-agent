package auto_update

import (
	"os"
	"testing"

	"github.com/rs/zerolog"
	"senhub-agent.go/internal/agent/cliArgs"
	"senhub-agent.go/internal/agent/services/configuration"
)

func TestAutoUpdate_GetName(t *testing.T) {
	logger := zerolog.New(os.Stderr)

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
	// Set a version
	cliArgs.Version = "1.0.0"
	testCases := []struct {
		name            string
		currentVersion  string
		expectedVersion string
		expectedResult  bool
	}{
		{
			name:            "Should update",
			currentVersion:  "1.0.0",
			expectedVersion: "1.0.1",
			expectedResult:  true,
		},
		{
			name:            "Should not update",
			currentVersion:  "1.0.0",
			expectedVersion: "1.0.0",
			expectedResult:  false,
		},
		{
			name:            "Should revert",
			currentVersion:  "1.0.0",
			expectedVersion: "0.9.2",
			expectedResult:  true,
		},
		{
			name:            "Should not update with empty version",
			currentVersion:  "1.0.0",
			expectedVersion: "",
			expectedResult:  false,
		},
		{
			name:            "Should update with constraint",
			currentVersion:  "1.0.0",
			expectedVersion: ">1.0.1",
			expectedResult:  true,
		},
		{
			name:            "Should NOT update with invalid constraint",
			currentVersion:  "1.0.0",
			expectedVersion: ">1.0.1!3%6&",
			expectedResult:  false,
		},
		{
			name:            "Should NOT update with complex constraint",
			currentVersion:  "1.0.1",
			expectedVersion: ">=1.0.1 <1.0.3",
			expectedResult:  false,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			logger := zerolog.New(os.Stderr)
			var configString string
			if tc.expectedVersion == "" {
				configString = `{ "agent": {} }`
			} else {
				configString = `{ "agent": { "version": "` + tc.expectedVersion + `" } }`
			}
			remoteConfig := configuration.NewMockRemoteConfiguration(
				"http://localhost:8000", configString)

			au := &autoUpdate{
				remoteConfig,
				&logger,
			}

			cliArgs.Version = tc.currentVersion

			shouldUpdate := au.shouldUpdate()
			if shouldUpdate != tc.expectedResult {
				t.Errorf("Expected %t, got %t", tc.expectedResult, shouldUpdate)
			}
		})
	}
}
