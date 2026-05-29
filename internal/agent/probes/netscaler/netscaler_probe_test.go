// Package netscaler provides monitoring capabilities for Citrix Netscaler (ADC) via NITRO API
package netscaler

import (
	"testing"

	"senhub-agent.go/probesdk/cliargs"
	"senhub-agent.go/probesdk/logger"
)

// TestNewNetscalerProbe tests probe creation
func TestNewNetscalerProbe(t *testing.T) {
	args := &cliArgs.ParsedArgs{
		Env:     "test",
		Verbose: false,
	}
	baseLogger := logger.NewLogger(args)

	config := map[string]interface{}{
		"base_url": "https://netscaler.example.com",
		"username": "nsroot",
		"password": "nsroot",
		"interval": float64(60),
	}

	probe, err := NewNetscalerProbe(config, baseLogger)
	if err != nil {
		t.Fatalf("Failed to create Netscaler probe: %v", err)
	}

	if probe == nil {
		t.Fatal("Expected non-nil probe")
	}
}

// TestNewNetscalerProbe_MissingBaseURL tests error when base_url is missing
func TestNewNetscalerProbe_MissingBaseURL(t *testing.T) {
	args := &cliArgs.ParsedArgs{
		Env:     "test",
		Verbose: false,
	}
	baseLogger := logger.NewLogger(args)

	config := map[string]interface{}{
		"username": "nsroot",
		"password": "nsroot",
	}

	_, err := NewNetscalerProbe(config, baseLogger)
	if err == nil {
		t.Error("Expected error for missing base_url")
	}
}

// TestNewNetscalerProbe_MissingCredentials tests error when credentials are missing
func TestNewNetscalerProbe_MissingCredentials(t *testing.T) {
	args := &cliArgs.ParsedArgs{
		Env:     "test",
		Verbose: false,
	}
	baseLogger := logger.NewLogger(args)

	tests := []struct {
		name   string
		config map[string]interface{}
	}{
		{
			name: "Missing username",
			config: map[string]interface{}{
				"base_url": "https://netscaler.example.com",
				"password": "nsroot",
			},
		},
		{
			name: "Missing password and api_key",
			config: map[string]interface{}{
				"base_url": "https://netscaler.example.com",
				"username": "nsroot",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := NewNetscalerProbe(tt.config, baseLogger)
			if err == nil {
				t.Error("Expected error for missing credentials")
			}
		})
	}
}

// TestNewNetscalerProbe_DefaultValues tests default values
func TestNewNetscalerProbe_DefaultValues(t *testing.T) {
	args := &cliArgs.ParsedArgs{
		Env:     "test",
		Verbose: false,
	}
	baseLogger := logger.NewLogger(args)

	// Minimum config
	config := map[string]interface{}{
		"base_url": "https://netscaler.example.com",
		"username": "nsroot",
		"password": "nsroot",
	}

	probe, err := NewNetscalerProbe(config, baseLogger)
	if err != nil {
		t.Fatalf("Failed to create probe: %v", err)
	}

	if probe == nil {
		t.Fatal("Expected non-nil probe")
	}

	// Note: We can't easily test the internal default values without exposing them
	// This test just verifies that the probe can be created without optional params
}

// TestNewNetscalerProbe_CustomPort tests custom port configuration
func TestNewNetscalerProbe_CustomPort(t *testing.T) {
	args := &cliArgs.ParsedArgs{
		Env:     "test",
		Verbose: false,
	}
	baseLogger := logger.NewLogger(args)

	config := map[string]interface{}{
		"base_url": "https://netscaler.example.com:8443",
		"username": "nsroot",
		"password": "nsroot",
	}

	probe, err := NewNetscalerProbe(config, baseLogger)
	if err != nil {
		t.Fatalf("Failed to create probe with custom port: %v", err)
	}

	if probe == nil {
		t.Fatal("Expected non-nil probe")
	}
}

// TestNewNetscalerProbe_InsecureSkipVerify tests SSL verification skip
func TestNewNetscalerProbe_InsecureSkipVerify(t *testing.T) {
	args := &cliArgs.ParsedArgs{
		Env:     "test",
		Verbose: false,
	}
	baseLogger := logger.NewLogger(args)

	config := map[string]interface{}{
		"base_url":             "https://netscaler.example.com",
		"username":             "nsroot",
		"password":             "nsroot",
		"insecure_skip_verify": true,
	}

	probe, err := NewNetscalerProbe(config, baseLogger)
	if err != nil {
		t.Fatalf("Failed to create probe with insecure_skip_verify: %v", err)
	}

	if probe == nil {
		t.Fatal("Expected non-nil probe")
	}
}

// TestNetscalerProbe_ConfigValidation tests configuration validation
func TestNetscalerProbe_ConfigValidation(t *testing.T) {
	args := &cliArgs.ParsedArgs{
		Env:     "test",
		Verbose: false,
	}
	baseLogger := logger.NewLogger(args)

	tests := []struct {
		name        string
		config      map[string]interface{}
		shouldError bool
		description string
	}{
		{
			name: "Valid minimal config",
			config: map[string]interface{}{
				"base_url": "https://netscaler.example.com",
				"username": "nsroot",
				"password": "nsroot",
			},
			shouldError: false,
			description: "Should accept minimal valid configuration",
		},
		{
			name: "Valid full config",
			config: map[string]interface{}{
				"base_url":             "https://netscaler.example.com",
				"username":             "nsroot",
				"password":             "nsroot",
				"interval":             float64(60),
				"insecure_skip_verify": true,
			},
			shouldError: false,
			description: "Should accept full configuration",
		},
		{
			name: "Valid config with API key",
			config: map[string]interface{}{
				"base_url": "https://netscaler.example.com",
				"username": "nsroot",
				"api_key":  "test-api-key",
			},
			shouldError: false,
			description: "Should accept API key instead of password",
		},
		{
			name: "Missing base_url",
			config: map[string]interface{}{
				"username": "nsroot",
				"password": "nsroot",
			},
			shouldError: true,
			description: "Should reject config without base_url",
		},
		{
			name: "Empty base_url",
			config: map[string]interface{}{
				"base_url": "",
				"username": "nsroot",
				"password": "nsroot",
			},
			shouldError: true,
			description: "Should reject empty base_url",
		},
		{
			name: "Missing username",
			config: map[string]interface{}{
				"base_url": "https://netscaler.example.com",
				"password": "nsroot",
			},
			shouldError: true,
			description: "Should reject config without username",
		},
		{
			name: "Missing password and api_key",
			config: map[string]interface{}{
				"base_url": "https://netscaler.example.com",
				"username": "nsroot",
			},
			shouldError: true,
			description: "Should reject config without password or API key",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := NewNetscalerProbe(tt.config, baseLogger)

			if tt.shouldError && err == nil {
				t.Errorf("Expected error: %s", tt.description)
			}

			if !tt.shouldError && err != nil {
				t.Errorf("Unexpected error: %v. %s", err, tt.description)
			}
		})
	}
}

// Note: HA stats collection tests require mocking the NITRO API client.
// These tests are omitted due to the complexity of mocking the netscaler.NitroClient
// interface and the need for extensive test infrastructure.
//
// For comprehensive HA stats testing, use integration tests with a real or
// simulated NetScaler instance. Key scenarios to test:
// 1. HA cluster with 2 nodes (PRIMARY + SECONDARY) - both UP
// 2. HA cluster with one node DOWN
// 3. Standalone NetScaler (no HA configured)
// 4. HA cluster with sync failures
// 5. Missing stats or config data
//
// The collectHAStats function (netscaler_collectors.go:533) handles these scenarios:
// - Returns error if HA not configured (expected for standalone)
// - Uses real stats for local node (connected node)
// - Uses config approximations for remote node (other HA node)
// - Handles missing fields gracefully with default values
//
// Manual test coverage: 2025-01-16
// - ✓ HA cluster with 2 nodes (node 0 PRIMARY, node 1 SECONDARY)
// - ✓ Both nodes UP, sync_status SUCCESS
// - ✓ Metrics include ha_node_id, ha_node_ip, is_local_node tags
// - ✓ Channel names use IP addresses ({ha_node_ip})
// - ✓ PRTG lookups work correctly (PRIMARY/SECONDARY/UP/SUCCESS)
