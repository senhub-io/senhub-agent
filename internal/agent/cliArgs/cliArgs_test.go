package cliArgs

import (
	"reflect"
	"testing"
)

func TestParsedArgsFromStartArgs(t *testing.T) {
	tests := []struct {
		name        string
		args        *StartSubcommandArgs
		environment string
		expected    *ParsedArgs
		description string
	}{
		{
			name: "Basic start args with authentication key",
			args: &StartSubcommandArgs{
				AuthenticationKey: "test-key-123",
				ServerUrl:         "https://example.com",
				Verbose:           true,
			},
			environment: "production",
			expected: &ParsedArgs{
				AuthenticationKey: "test-key-123",
				ServerUrl:         "https://example.com",
				Verbose:           true,
				DebugModules:      nil,
				Env:               "production",
				Version:           Version,
				CommitHash:        CommitHash,
				ConfigPath:        "./agent-config.yaml", // Default
				HttpsPort:         8443,                  // Default
				HttpsHosts:        []string{"localhost", "127.0.0.1"},
				MinTlsVersion:     "1.2", // Default
			},
			description: "Should parse basic start args with defaults",
		},
		{
			name: "Offline mode with custom config path",
			args: &StartSubcommandArgs{
				Offline:    true,
				ConfigPath: "/custom/path/config.yaml",
			},
			environment: "production",
			expected: &ParsedArgs{
				Offline:       true,
				ConfigPath:    "/custom/path/config.yaml",
				ServerUrl:     "", // No server URL in offline mode
				Env:           "production",
				Version:       Version,
				CommitHash:    CommitHash,
				HttpsPort:     8443,
				HttpsHosts:    []string{"localhost", "127.0.0.1"},
				MinTlsVersion: "1.2",
			},
			description: "Should handle offline mode correctly",
		},
		{
			name: "HTTPS configuration with custom settings",
			args: &StartSubcommandArgs{
				EnableHttps:   true,
				HttpsPort:     9443,
				HttpsHosts:    "example.com,*.example.com,192.168.1.1",
				CertFile:      "/path/to/cert.pem",
				KeyFile:       "/path/to/key.pem",
				MinTlsVersion: "1.3",
			},
			environment: "production",
			expected: &ParsedArgs{
				EnableHttps:   true,
				HttpsPort:     9443,
				HttpsHosts:    []string{"example.com", "*.example.com", "192.168.1.1"},
				CertFile:      "/path/to/cert.pem",
				KeyFile:       "/path/to/key.pem",
				MinTlsVersion: "1.3",
				ServerUrl:     "", // Will be set to default by function logic
				ConfigPath:    "./agent-config.yaml",
				Env:           "production",
				Version:       Version,
				CommitHash:    CommitHash,
			},
			description: "Should parse HTTPS configuration correctly",
		},
		{
			name: "Debug modules parsing",
			args: &StartSubcommandArgs{
				DebugModules: "strategy.http,cache,probe.redfish",
				Verbose:      true,
			},
			environment: "development",
			expected: &ParsedArgs{
				DebugModules:  []string{"strategy.http", "cache", "probe.redfish"},
				Verbose:       true,
				ServerUrl:     "", // Will use default
				Env:           "development",
				Version:       Version,
				CommitHash:    CommitHash,
				ConfigPath:    "./agent-config.yaml",
				HttpsPort:     8443,
				HttpsHosts:    []string{"localhost", "127.0.0.1"},
				MinTlsVersion: "1.2",
			},
			description: "Should parse comma-separated debug modules",
		},
		{
			name: "Debug modules with whitespace",
			args: &StartSubcommandArgs{
				DebugModules: " strategy.http , cache  ,  probe.redfish ",
			},
			environment: "production",
			expected: &ParsedArgs{
				Verbose:       true, // --debug-modules implies --verbose
				DebugModules:  []string{"strategy.http", "cache", "probe.redfish"},
				ServerUrl:     "",
				Env:           "production",
				Version:       Version,
				CommitHash:    CommitHash,
				ConfigPath:    "./agent-config.yaml",
				HttpsPort:     8443,
				HttpsHosts:    []string{"localhost", "127.0.0.1"},
				MinTlsVersion: "1.2",
			},
			description: "Should trim whitespace from debug modules and imply verbose",
		},
		{
			name: "Filter flag sets debug modules and implies verbose",
			args: &StartSubcommandArgs{
				Filter: "probe.veeam,strategy.http",
			},
			environment: "production",
			expected: &ParsedArgs{
				Verbose:       true,
				DebugModules:  []string{"probe.veeam", "strategy.http"},
				ServerUrl:     "",
				Env:           "production",
				Version:       Version,
				CommitHash:    CommitHash,
				ConfigPath:    "./agent-config.yaml",
				HttpsPort:     8443,
				HttpsHosts:    []string{"localhost", "127.0.0.1"},
				MinTlsVersion: "1.2",
			},
			description: "Filter flag should populate DebugModules and set Verbose true",
		},
		{
			name: "Filter takes precedence over debug-modules",
			args: &StartSubcommandArgs{
				Filter:       "probe.veeam",
				DebugModules: "strategy.http",
			},
			environment: "production",
			expected: &ParsedArgs{
				Verbose:       true,
				DebugModules:  []string{"probe.veeam"},
				ServerUrl:     "",
				Env:           "production",
				Version:       Version,
				CommitHash:    CommitHash,
				ConfigPath:    "./agent-config.yaml",
				HttpsPort:     8443,
				HttpsHosts:    []string{"localhost", "127.0.0.1"},
				MinTlsVersion: "1.2",
			},
			description: "Filter should win over deprecated debug-modules",
		},
		{
			name: "Debug log shipper configuration",
			args: &StartSubcommandArgs{
				DebugLogShipperUrl: "https://logs.example.com/ingest",
				DebugLogShipperTags: map[string]string{
					"environment": "production",
					"service":     "senhub-agent",
				},
				DebugLogShipperBuffer: 1000,
			},
			environment: "production",
			expected: &ParsedArgs{
				DebugLogShipperUrl: "https://logs.example.com/ingest",
				DebugLogShipperTags: map[string]string{
					"environment": "production",
					"service":     "senhub-agent",
				},
				DebugLogShipperBuffer: 1000,
				ServerUrl:             "",
				Env:                   "production",
				Version:               Version,
				CommitHash:            CommitHash,
				ConfigPath:            "./agent-config.yaml",
				HttpsPort:             8443,
				HttpsHosts:            []string{"localhost", "127.0.0.1"},
				MinTlsVersion:         "1.2",
			},
			description: "Should parse debug log shipper configuration",
		},
		{
			name:        "Default values when nothing specified",
			args:        &StartSubcommandArgs{},
			environment: "production",
			expected: &ParsedArgs{
				ServerUrl:     "", // Will use default from function
				ConfigPath:    "./agent-config.yaml",
				HttpsPort:     8443,
				HttpsHosts:    []string{"localhost", "127.0.0.1"},
				MinTlsVersion: "1.2",
				Env:           "production",
				Version:       Version,
				CommitHash:    CommitHash,
			},
			description: "Should apply all default values",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := parsedArgsFromStartArgs(tt.args, tt.environment)

			// Check each field
			if result.AuthenticationKey != tt.expected.AuthenticationKey {
				t.Errorf("AuthenticationKey: got %s, want %s", result.AuthenticationKey, tt.expected.AuthenticationKey)
			}
			if result.Verbose != tt.expected.Verbose {
				t.Errorf("Verbose: got %v, want %v", result.Verbose, tt.expected.Verbose)
			}
			if result.Offline != tt.expected.Offline {
				t.Errorf("Offline: got %v, want %v", result.Offline, tt.expected.Offline)
			}
			if result.ConfigPath != tt.expected.ConfigPath {
				t.Errorf("ConfigPath: got %s, want %s", result.ConfigPath, tt.expected.ConfigPath)
			}
			if result.EnableHttps != tt.expected.EnableHttps {
				t.Errorf("EnableHttps: got %v, want %v", result.EnableHttps, tt.expected.EnableHttps)
			}
			if result.HttpsPort != tt.expected.HttpsPort {
				t.Errorf("HttpsPort: got %d, want %d", result.HttpsPort, tt.expected.HttpsPort)
			}
			if !reflect.DeepEqual(result.HttpsHosts, tt.expected.HttpsHosts) {
				t.Errorf("HttpsHosts: got %v, want %v", result.HttpsHosts, tt.expected.HttpsHosts)
			}
			if result.CertFile != tt.expected.CertFile {
				t.Errorf("CertFile: got %s, want %s", result.CertFile, tt.expected.CertFile)
			}
			if result.KeyFile != tt.expected.KeyFile {
				t.Errorf("KeyFile: got %s, want %s", result.KeyFile, tt.expected.KeyFile)
			}
			if result.MinTlsVersion != tt.expected.MinTlsVersion {
				t.Errorf("MinTlsVersion: got %s, want %s", result.MinTlsVersion, tt.expected.MinTlsVersion)
			}
			if !reflect.DeepEqual(result.DebugModules, tt.expected.DebugModules) {
				t.Errorf("DebugModules: got %v, want %v", result.DebugModules, tt.expected.DebugModules)
			}
			if result.Env != tt.expected.Env {
				t.Errorf("Env: got %s, want %s", result.Env, tt.expected.Env)
			}
			if result.DebugLogShipperUrl != tt.expected.DebugLogShipperUrl {
				t.Errorf("DebugLogShipperUrl: got %s, want %s", result.DebugLogShipperUrl, tt.expected.DebugLogShipperUrl)
			}
			if result.DebugLogShipperBuffer != tt.expected.DebugLogShipperBuffer {
				t.Errorf("DebugLogShipperBuffer: got %d, want %d", result.DebugLogShipperBuffer, tt.expected.DebugLogShipperBuffer)
			}
		})
	}
}

func TestParsedArgsFromUpdateArgs(t *testing.T) {
	tests := []struct {
		name        string
		args        *UpdateSubcommandArgs
		environment string
		expected    *ParsedArgs
		description string
	}{
		{
			name: "Basic update args",
			args: &UpdateSubcommandArgs{
				Version:           "1.2.3",
				AuthenticationKey: "update-key-456",
				ServerUrl:         "https://update.example.com",
				Verbose:           true,
			},
			environment: "production",
			expected: &ParsedArgs{
				WantedVersion:     "1.2.3",
				AuthenticationKey: "update-key-456",
				ServerUrl:         "https://update.example.com",
				Verbose:           true,
				Env:               "production",
				Version:           Version,
				CommitHash:        CommitHash,
			},
			description: "Should parse basic update args",
		},
		{
			name: "Update with registry URL",
			args: &UpdateSubcommandArgs{
				Version:     "2.0.0-beta",
				RegistryUrl: "https://registry.example.com",
			},
			environment: "development",
			expected: &ParsedArgs{
				WantedVersion:     "2.0.0-beta",
				UpdateRegistryUrl: "https://registry.example.com",
				ServerUrl:         "", // Will use default
				Env:               "development",
				Version:           Version,
				CommitHash:        CommitHash,
			},
			description: "Should parse registry URL",
		},
		{
			name: "Update with dry-run",
			args: &UpdateSubcommandArgs{
				Version: "3.0.0",
				DryRun:  true,
			},
			environment: "production",
			expected: &ParsedArgs{
				WantedVersion: "3.0.0",
				DryRun:        true,
				ServerUrl:     "",
				Env:           "production",
				Version:       Version,
				CommitHash:    CommitHash,
			},
			description: "Should handle dry-run flag",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := parsedArgsFromUpdateArgs(tt.args, tt.environment)

			if result.WantedVersion != tt.expected.WantedVersion {
				t.Errorf("WantedVersion: got %s, want %s", result.WantedVersion, tt.expected.WantedVersion)
			}
			if result.AuthenticationKey != tt.expected.AuthenticationKey {
				t.Errorf("AuthenticationKey: got %s, want %s", result.AuthenticationKey, tt.expected.AuthenticationKey)
			}
			if result.UpdateRegistryUrl != tt.expected.UpdateRegistryUrl {
				t.Errorf("UpdateRegistryUrl: got %s, want %s", result.UpdateRegistryUrl, tt.expected.UpdateRegistryUrl)
			}
			if result.Verbose != tt.expected.Verbose {
				t.Errorf("Verbose: got %v, want %v", result.Verbose, tt.expected.Verbose)
			}
			if result.DryRun != tt.expected.DryRun {
				t.Errorf("DryRun: got %v, want %v", result.DryRun, tt.expected.DryRun)
			}
			if result.Env != tt.expected.Env {
				t.Errorf("Env: got %s, want %s", result.Env, tt.expected.Env)
			}
		})
	}
}

func TestGetVersionInfo(t *testing.T) {
	// Set test values
	oldVersion := Version
	oldCommitHash := CommitHash
	oldBuildTime := BuildTime
	oldGoVersion := GoVersion
	oldEnv := Env
	oldProdURL := ProductionURL

	defer func() {
		Version = oldVersion
		CommitHash = oldCommitHash
		BuildTime = oldBuildTime
		GoVersion = oldGoVersion
		Env = oldEnv
		ProductionURL = oldProdURL
	}()

	Version = "1.0.0-test"
	CommitHash = "abc123"
	BuildTime = "2025-01-01T00:00:00Z"
	GoVersion = "go1.21.0"
	Env = "production"
	ProductionURL = "https://test-prod.example.com"

	info := GetVersionInfo()

	if info["version"] != "1.0.0-test" {
		t.Errorf("version: got %s, want 1.0.0-test", info["version"])
	}
	if info["commitHash"] != "abc123" {
		t.Errorf("commitHash: got %s, want abc123", info["commitHash"])
	}
	if info["buildTime"] != "2025-01-01T00:00:00Z" {
		t.Errorf("buildTime: got %s, want 2025-01-01T00:00:00Z", info["buildTime"])
	}
	if info["goVersion"] != "go1.21.0" {
		t.Errorf("goVersion: got %s, want go1.21.0", info["goVersion"])
	}
	if info["env"] != "production" {
		t.Errorf("env: got %s, want production", info["env"])
	}
	// defaultURL is set based on environment
	if _, exists := info["defaultURL"]; !exists {
		t.Error("defaultURL key should exist in version info")
	}
}

func TestDefaultServerURL(t *testing.T) {
	// Save original values
	oldEnv := Env
	oldProdURL := ProductionURL
	oldDevURL := DevelopmentURL

	defer func() {
		Env = oldEnv
		ProductionURL = oldProdURL
		DevelopmentURL = oldDevURL
	}()

	tests := []struct {
		name           string
		env            string
		productionURL  string
		developmentURL string
		expectedURL    string
	}{
		{
			name:           "Production environment",
			env:            "production",
			productionURL:  "https://prod.example.com",
			developmentURL: "https://dev.example.com",
			expectedURL:    "https://prod.example.com",
		},
		{
			name:           "Development environment",
			env:            "development",
			productionURL:  "https://prod.example.com",
			developmentURL: "https://dev.example.com",
			expectedURL:    "https://dev.example.com",
		},
		{
			name:           "Unknown environment defaults to production",
			env:            "staging",
			productionURL:  "https://prod.example.com",
			developmentURL: "https://dev.example.com",
			expectedURL:    "https://prod.example.com",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			Env = tt.env
			ProductionURL = tt.productionURL
			DevelopmentURL = tt.developmentURL

			result := defaultServerURL()
			if result != tt.expectedURL {
				t.Errorf("got %s, want %s", result, tt.expectedURL)
			}
		})
	}
}

func TestHttpsHostsParsing(t *testing.T) {
	tests := []struct {
		name        string
		hostsString string
		expected    []string
	}{
		{
			name:        "Single host",
			hostsString: "example.com",
			expected:    []string{"example.com"},
		},
		{
			name:        "Multiple hosts",
			hostsString: "example.com,*.example.com,192.168.1.1",
			expected:    []string{"example.com", "*.example.com", "192.168.1.1"},
		},
		{
			name:        "Hosts with whitespace",
			hostsString: " example.com , *.example.com  ,  192.168.1.1 ",
			expected:    []string{"example.com", "*.example.com", "192.168.1.1"},
		},
		{
			name:        "Empty string uses defaults",
			hostsString: "",
			expected:    []string{"localhost", "127.0.0.1"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			args := &StartSubcommandArgs{
				HttpsHosts: tt.hostsString,
			}
			result := parsedArgsFromStartArgs(args, "production")

			if !reflect.DeepEqual(result.HttpsHosts, tt.expected) {
				t.Errorf("got %v, want %v", result.HttpsHosts, tt.expected)
			}
		})
	}
}

func TestDefaultValues(t *testing.T) {
	args := &StartSubcommandArgs{}
	result := parsedArgsFromStartArgs(args, "production")

	// Test all default values
	if result.ConfigPath != "./agent-config.yaml" {
		t.Errorf("ConfigPath default: got %s, want ./agent-config.yaml", result.ConfigPath)
	}
	if result.HttpsPort != 8443 {
		t.Errorf("HttpsPort default: got %d, want 8443", result.HttpsPort)
	}
	if !reflect.DeepEqual(result.HttpsHosts, []string{"localhost", "127.0.0.1"}) {
		t.Errorf("HttpsHosts default: got %v, want [localhost 127.0.0.1]", result.HttpsHosts)
	}
	if result.MinTlsVersion != "1.2" {
		t.Errorf("MinTlsVersion default: got %s, want 1.2", result.MinTlsVersion)
	}
}
