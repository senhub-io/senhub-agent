package auto_update

import (
	"fmt"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"github.com/ybbus/httpretry"
	"senhub-agent.go/internal/agent/cliArgs"
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

	remoteConfig := newMockConfiguration("http://localhost:8080", "")

	au := NewAutoUpdate(AutoUpdateConfig{
		ConfigSource: remoteConfig,
		Logger:       baseLogger,
		DryRun:       false,
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
			remoteConfig := newMockConfiguration(
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

func TestAutoUpdate_DowngradeGuardKept(t *testing.T) {
	versionServer := testUtils.GetTestHTTPServerWithURLPath(
		NewAutoUpdateVersionRoutes([][2]string{
			{"1.0.0"},
			{"1.1.0"},
			{"latest", "1.1.0"},
		}),
	)
	defer versionServer.Server.Close()

	oldVersion := cliArgs.Version
	t.Cleanup(func() { cliArgs.Version = oldVersion })
	cliArgs.Version = "1.2.0"

	testCases := []struct {
		name           string
		operatorDriven bool
		requested      string
	}{
		{"daemon, explicit older", false, "1.1.0"},
		{"daemon, latest older", false, "latest"},
		{"operator, latest older", true, "latest"},
		{"operator, constraint older", true, "<1.2"},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			au := &autoUpdate{
				configSource:   newMockConfiguration("http://localhost:8000", `{ "agent": {} }`),
				logger:         createTestModuleLogger(),
				httpClient:     httpretry.NewDefaultClient(),
				operatorDriven: tc.operatorDriven,
			}
			updated, err := au.Update(tc.requested, versionServer.URL)
			if updated {
				t.Fatalf("expected updated to be false, got true")
			}
			if err != nil {
				t.Fatalf("expected err to be nil, got %v", err)
			}
		})
	}
}

func TestAutoUpdate_OperatorResolutionFailsLoudly(t *testing.T) {
	versionServer := testUtils.GetTestHTTPServerWithURLPath(
		NewAutoUpdateVersionRoutes([][2]string{
			{"1.0.0"},
			{"1.1.0"},
			{"latest", "1.1.0"},
		}),
	)
	defer versionServer.Server.Close()

	oldVersion := cliArgs.Version
	t.Cleanup(func() { cliArgs.Version = oldVersion })
	cliArgs.Version = "1.0.0"

	testCases := []struct {
		name        string
		requested   string
		errContains string
	}{
		{"unparseable word", "pas-une-version", "not a valid version"},
		{"unparseable rc", "0.7.0.rc.4", "not a valid version"},
		{"unparseable constraint", "9.x.zz", "not a valid version"},
		{"well-formed but absent prerelease", "0.0.1-nexiste-pas", "not found in the update registry"},
		{"well-formed but absent stable", "9.9.9", "not found in the update registry"},
		{"well-formed but absent beta", "0.7.0-beta.99", "cannot fetch version 0.7.0-beta.99"},
		{"equal version", "1.0.0", ""},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			au := &autoUpdate{
				configSource:   newMockConfiguration("http://localhost:8000", `{ "agent": {} }`),
				logger:         createTestModuleLogger(),
				httpClient:     httpretry.NewDefaultClient(),
				operatorDriven: true,
			}
			updated, err := au.Update(tc.requested, versionServer.URL)
			if updated {
				t.Fatalf("expected updated to be false, got true")
			}
			if tc.errContains == "" {
				if err != nil {
					t.Fatalf("expected err to be nil, got %v", err)
				}
				return
			}
			if err == nil {
				t.Fatalf("expected error containing %q, got nil", tc.errContains)
			}
			if !strings.Contains(err.Error(), tc.errContains) {
				t.Fatalf("expected error containing %q, got %v", tc.errContains, err)
			}
		})
	}

	t.Run("unreachable registry", func(t *testing.T) {
		unreachableServer := testUtils.GetTestHTTPServerWithURLPath(
			NewAutoUpdateVersionRoutes([][2]string{
				{"1.0.0"},
				{"1.1.0"},
			}),
		)
		closedURL := unreachableServer.URL
		unreachableServer.Server.Close()

		au := &autoUpdate{
			configSource:   newMockConfiguration("http://localhost:8000", `{ "agent": {} }`),
			logger:         createTestModuleLogger(),
			httpClient:     httpretry.NewDefaultClient(httpretry.WithMaxRetryCount(0)),
			operatorDriven: true,
		}
		updated, err := au.Update("1.1.0", closedURL)
		if updated {
			t.Fatalf("expected updated to be false, got true")
		}
		if err == nil {
			t.Fatalf("expected an error, got nil")
		}
		if !strings.Contains(err.Error(), "cannot reach the update registry") {
			t.Fatalf("expected error containing %q, got %v", "cannot reach the update registry", err)
		}
	})
}

func TestAutoUpdate_DaemonKeepsSilentFallback(t *testing.T) {
	versionServer := testUtils.GetTestHTTPServerWithURLPath(
		NewAutoUpdateVersionRoutes([][2]string{
			{"1.0.0"},
			{"1.1.0"},
			{"latest", "1.1.0"},
		}),
	)
	defer versionServer.Server.Close()

	oldVersion := cliArgs.Version
	t.Cleanup(func() { cliArgs.Version = oldVersion })
	cliArgs.Version = "1.0.0"

	au := &autoUpdate{
		configSource:   newMockConfiguration("http://localhost:8000", `{ "agent": {} }`),
		logger:         createTestModuleLogger(),
		httpClient:     httpretry.NewDefaultClient(),
		operatorDriven: false,
	}
	updated, err := au.Update("pas-une-version", versionServer.URL)
	if updated {
		t.Fatalf("expected updated to be false, got true")
	}
	if err != nil {
		t.Fatalf("expected err to be nil, got %v", err)
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
			remoteConfig := newMockConfiguration(
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

// TestAutoUpdate_getExpectedVersion_LatestIncludeBeta proves that the
// "latest" alias resolution honours include_beta: with it off, the newest
// stable wins (0.4.1); with it on, the newest beta wins (0.4.2-beta). This
// is the #591 regression: the tick path previously fetched only the stable
// channel and never saw the beta even when include_beta was set.
func TestAutoUpdate_getExpectedVersion_LatestIncludeBeta(t *testing.T) {
	versionServer := testUtils.GetTestHTTPServerWithURLPath([]testUtils.TestHTTPServerURLConf{
		NewAutoUpdateVersionMetadataRoute("0.4.1"),
		NewAutoUpdateVersionMetadataRoute("0.4.2-beta"),
		NewAutoUpdateVersionListRoute([][2]string{{"0.4.1"}}),
		NewAutoUpdateVersionListBetaRoute([][2]string{{"0.4.1"}, {"0.4.2-beta"}}),
	})
	defer versionServer.Server.Close()

	testCases := []struct {
		name           string
		includeBeta    bool
		expectedResult string
	}{
		{
			name:           "include_beta off resolves latest stable",
			includeBeta:    false,
			expectedResult: "0.4.1",
		},
		{
			name:           "include_beta on resolves latest beta",
			includeBeta:    true,
			expectedResult: "0.4.2-beta",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			configString := fmt.Sprintf(`{
				"agent": {
					"version": "latest",
					"registry_url": "%s",
					"include_beta": %t
				}
			}`, versionServer.URL, tc.includeBeta)
			remoteConfig := newMockConfiguration(
				"http://localhost:8000", configString)

			httpClient := httpretry.NewDefaultClient()
			au := &autoUpdate{
				configSource: remoteConfig,
				logger:       createTestModuleLogger(),
				httpClient:   httpClient,
			}

			// Pin current to a low version so resolution, not the downgrade
			// guard, is what's under test here.
			cliArgs.Version = "0.1.0"

			resolved := au.getExpectedVersionFromConfig()
			if resolved != tc.expectedResult {
				t.Errorf("Expected %s, got %s", tc.expectedResult, resolved)
			}
		})
	}
}

// TestGetExpectedVersion_LatestNilConfigSource pins m10: the CLI 'update latest'
// path builds the updater without a ConfigSource, so resolving 'latest' must not
// dereference a nil configSource. It falls back to stable-only and, when the
// registry is unreachable, returns the current version rather than panicking.
func TestGetExpectedVersion_LatestNilConfigSource(t *testing.T) {
	au := &autoUpdate{
		configSource: nil,
		logger:       createTestModuleLogger(),
		httpClient:   httpretry.NewDefaultClient(httpretry.WithMaxRetryCount(0)),
	}
	cliArgs.Version = "1.2.3"

	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("getExpectedVersion panicked with nil configSource: %v", r)
		}
	}()

	// An unroutable registry makes 'latest' resolution fail closed to current.
	got := au.getExpectedVersion("latest", "https://127.0.0.1:1/")
	if got != "1.2.3" {
		t.Errorf("getExpectedVersion(latest) = %q, want current 1.2.3", got)
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
			name:     "Numbered beta",
			version:  "0.6.2-beta.3",
			expected: true,
		},
		{
			name:     "Numbered beta, two digits",
			version:  "0.6.2-beta.12",
			expected: true,
		},
		{
			name:     "Earlier betaN form",
			version:  "0.6.1-beta2",
			expected: true,
		},
		{
			name:     "Beta with a dangling dot",
			version:  "0.6.2-beta.",
			expected: false,
		},
		{
			name:     "Beta number is not numeric",
			version:  "0.6.2-beta.x",
			expected: false,
		},
		{
			name:     "Dev build",
			version:  "0.6.2-dev.57.g1a2b3c4d",
			expected: false,
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
		{
			name:     "Release candidate",
			version:  "0.7.0-rc.1",
			expected: true,
		},
		{
			name:     "Release candidate, two digits",
			version:  "0.7.0-rc.12",
			expected: true,
		},
		{
			name:     "Release candidate with build metadata",
			version:  "0.7.0-rc.2+abc",
			expected: true,
		},
		{
			name:     "Unnumbered rc",
			version:  "0.7.0-rc",
			expected: false,
		},
		{
			name:     "Rc with a dangling dot",
			version:  "0.7.0-rc.",
			expected: false,
		},
		{
			name:     "Rc number is not numeric",
			version:  "0.7.0-rc.x",
			expected: false,
		},
		{
			name:     "Earlier rcN form is not accepted",
			version:  "0.7.0-rc2",
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
			remoteConfig := newMockConfiguration(
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
			remoteConfig := newMockConfiguration(
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

// The check runs every hour whatever an old configuration still carries:
// update_check_interval is not read.
func TestAutoUpdate_GetUpdateCheckInterval(t *testing.T) {
	au := &autoUpdate{
		configSource: newMockConfiguration("http://localhost:8000", `{"agent": {"update_check_interval": "1m"}}`),
		logger:       createTestModuleLogger(),
		httpClient:   httpretry.NewDefaultClient(),
	}
	if got := au.GetUpdateCheckInterval(); got != time.Hour {
		t.Errorf("GetUpdateCheckInterval() = %v, want 1h", got)
	}
}

func TestAutoUpdate_LatestFollowsIncludeBetaWithoutConfigSource(t *testing.T) {
	versionServer := testUtils.GetTestHTTPServerWithURLPath(NewAutoUpdateVersionRoutes([][2]string{{"1.0.0"}, {"1.1.0"}, {"1.2.0-beta.1"}, {"latest", "1.1.0"}}))
	defer versionServer.Server.Close()

	oldVersion := cliArgs.Version
	t.Cleanup(func() { cliArgs.Version = oldVersion })
	cliArgs.Version = "1.0.0"

	au := &autoUpdate{logger: createTestModuleLogger(), httpClient: httpretry.NewDefaultClient(), operatorDriven: true, includeBeta: true}
	got, err := au.resolveVersion("latest", versionServer.URL)
	if err != nil {
		t.Fatalf("resolveVersion(latest) with includeBeta=true returned error: %v", err)
	}
	if got != "1.2.0-beta.1" {
		t.Fatalf("resolveVersion(latest) with includeBeta=true = %q, want %q", got, "1.2.0-beta.1")
	}

	auStable := &autoUpdate{logger: createTestModuleLogger(), httpClient: httpretry.NewDefaultClient(), operatorDriven: true, includeBeta: false}
	gotStable, err := auStable.resolveVersion("latest", versionServer.URL)
	if err != nil {
		t.Fatalf("resolveVersion(latest) with includeBeta=false returned error: %v", err)
	}
	if gotStable != "1.1.0" {
		t.Fatalf("resolveVersion(latest) with includeBeta=false = %q, want %q", gotStable, "1.1.0")
	}
}

func TestAutoUpdate_ResolvedVersionNamesTheConcreteRelease(t *testing.T) {
	versionServer := testUtils.GetTestHTTPServerWithURLPath(NewAutoUpdateVersionRoutes([][2]string{{"1.0.0"}, {"1.1.0"}, {"latest", "1.1.0"}}))
	defer versionServer.Server.Close()

	oldVersion := cliArgs.Version
	cliArgs.Version = "1.0.0"
	t.Cleanup(func() { cliArgs.Version = oldVersion })

	au := &autoUpdate{
		configSource:   newMockConfiguration("http://localhost:8000", `{ "agent": {} }`),
		logger:         createTestModuleLogger(),
		httpClient:     httpretry.NewDefaultClient(),
		operatorDriven: true,
		dryRun:         true,
	}

	updated, err := au.Update("latest", versionServer.URL)
	if err != nil {
		t.Fatalf("Update returned error: %v", err)
	}
	if !updated {
		t.Fatal("expected Update to report an update")
	}
	if got := au.ResolvedVersion(); got != "1.1.0" {
		t.Fatalf("ResolvedVersion = %q, want %q", got, "1.1.0")
	}
}
