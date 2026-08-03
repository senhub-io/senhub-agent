// Package http provides HTTP strategy for data storage
package http

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gorilla/mux"
	"senhub-agent.go/internal/agent/cliArgs"
	"senhub-agent.go/internal/agent/services/configuration"
	"senhub-agent.go/internal/agent/services/license"
	"senhub-agent.go/internal/agent/services/logger"
)

// Test helpers

func createTestLoggerForAPI() *logger.Logger {
	args := &cliArgs.ParsedArgs{
		Env:     "test",
		Verbose: false,
	}
	return logger.NewLogger(args)
}

func createTestAPIManager(agentConfig configuration.AgentConfiguration) *APIManager {
	baseLogger := createTestLoggerForAPI()
	moduleLogger := logger.NewModuleLogger(baseLogger, "test.api")

	// Create minimal HTTPSyncStrategy with agentConfig
	strategy := &HTTPSyncStrategy{
		agentConfig: agentConfig,
		logger:      moduleLogger,
		agentKey:    agentConfig.GetAuthenticationKey(),
	}

	// Create authentication manager
	strategy.authManager = NewAuthenticationManager(strategy.agentKey, agentConfig, moduleLogger)

	// Create API manager
	return NewAPIManager(strategy, moduleLogger)
}

// Mock configuration with license token
type mockConfigWithLicense struct {
	configuration.AgentConfiguration
	licenseToken string
}

func (m *mockConfigWithLicense) GetConfiguration() configuration.ConfigurationData {
	return configuration.ConfigurationData{
		Agent: configuration.AgentConfig{
			License: m.licenseToken,
		},
	}
}

func newMockConfigWithLicense(agentKey, licenseToken string) *mockConfigWithLicense {
	testLogger := createTestLoggerForAPI()
	baseConfig := configuration.NewAgentConfiguration(agentKey, "http://test-server", testLogger)
	return &mockConfigWithLicense{
		AgentConfiguration: baseConfig,
		licenseToken:       licenseToken,
	}
}

// Test cases

func TestAPIManager_HandleLicenseStatus_NoLicense(t *testing.T) {
	// Create mock configuration without license
	agentKey := "test-agent-key"
	agentConfig := newMockConfigWithLicense(agentKey, "")

	// Create API manager
	apiManager := createTestAPIManager(agentConfig)

	// Create test request
	req := httptest.NewRequest("GET", fmt.Sprintf("/api/%s/license/status", agentKey), nil)
	w := httptest.NewRecorder()

	// Set up mux vars for agentkey
	req = mux.SetURLVars(req, map[string]string{"agentkey": agentKey})

	// Call handler
	apiManager.HandleLicenseStatus(w, req)

	// Check response
	if w.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d", w.Code)
	}

	// Parse response
	var response struct {
		Status         string   `json:"status"`
		Tier           string   `json:"tier"`
		Message        string   `json:"message"`
		FreeTierProbes []string `json:"free_tier_probes"`
	}

	if err := json.NewDecoder(w.Body).Decode(&response); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}

	// Verify response
	if response.Status != "none" {
		t.Errorf("Expected status 'none', got '%s'", response.Status)
	}
	if response.Tier != "free" {
		t.Errorf("Expected tier 'free', got '%s'", response.Tier)
	}
	// free_tier_probes must be the full Free tier, not a hardcoded core subset.
	if want := len(license.GetFreeTierProbes()); len(response.FreeTierProbes) != want {
		t.Errorf("Expected %d free tier probes (the full free tier), got %d", want, len(response.FreeTierProbes))
	}
	if len(response.FreeTierProbes) <= 4 {
		t.Errorf("free_tier_probes looks like the legacy 4-probe stub (%v)", response.FreeTierProbes)
	}
	if response.Message != "No license configured - running in free tier mode" {
		t.Errorf("Unexpected message: %s", response.Message)
	}
}

func TestAPIManager_HandleLicenseStatus_InvalidLicense(t *testing.T) {
	// Create mock configuration with invalid license
	agentKey := "test-agent-key"
	agentConfig := newMockConfigWithLicense(agentKey, "invalid-license-token")

	// Create API manager
	apiManager := createTestAPIManager(agentConfig)

	// Create test request
	req := httptest.NewRequest("GET", fmt.Sprintf("/api/%s/license/status", agentKey), nil)
	w := httptest.NewRecorder()

	// Set up mux vars
	req = mux.SetURLVars(req, map[string]string{"agentkey": agentKey})

	// Call handler
	apiManager.HandleLicenseStatus(w, req)

	// Check response
	if w.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d", w.Code)
	}

	// Parse response
	var response struct {
		Status  string `json:"status"`
		Tier    string `json:"tier"`
		Message string `json:"message"`
	}

	if err := json.NewDecoder(w.Body).Decode(&response); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}

	// Verify response
	if response.Status != "invalid" {
		t.Errorf("Expected status 'invalid', got '%s'", response.Status)
	}
	if response.Tier != "free" {
		t.Errorf("Expected tier 'free', got '%s'", response.Tier)
	}
	if response.Message != "Invalid license token - running in free tier mode" {
		t.Errorf("Unexpected message: %s", response.Message)
	}
}

// Note: Tests for valid active licenses, grace period, and expired licenses
// are not included here because they would require access to the production
// RSA private key to generate properly signed JWT tokens.
//
// The license validation logic itself is thoroughly tested in
// internal/agent/services/license/license_test.go (95% coverage).
//
// These endpoint tests focus on:
// 1. No license configured (free tier)
// 2. Invalid/malformed license tokens
// 3. Authentication requirements
// 4. HTTP response format correctness
//
// For integration testing of valid licenses, use the full agent with
// real license tokens from the Sensor Factory.

func TestAPIManager_HandleLicenseStatus_UnauthorizedAccess(t *testing.T) {
	// Create mock configuration
	agentKey := "test-agent-key"
	agentConfig := newMockConfigWithLicense(agentKey, "")

	// Create API manager
	apiManager := createTestAPIManager(agentConfig)

	// Create test request with wrong agent key
	wrongKey := "wrong-agent-key"
	req := httptest.NewRequest("GET", fmt.Sprintf("/api/%s/license/status", wrongKey), nil)
	w := httptest.NewRecorder()

	// Set up mux vars with wrong key
	req = mux.SetURLVars(req, map[string]string{"agentkey": wrongKey})

	// Call handler
	apiManager.HandleLicenseStatus(w, req)

	// Check response (should be 401 Unauthorized)
	if w.Code != http.StatusUnauthorized {
		t.Errorf("Expected status 401 (Unauthorized), got %d", w.Code)
	}
}

func TestAPIManager_GetLicenseToken(t *testing.T) {
	tests := []struct {
		name          string
		licenseToken  string
		expectedToken string
	}{
		{
			name:          "With license token",
			licenseToken:  "test-license-token-123",
			expectedToken: "test-license-token-123",
		},
		{
			name:          "Empty license token",
			licenseToken:  "",
			expectedToken: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create mock configuration
			agentConfig := newMockConfigWithLicense("test-key", tt.licenseToken)

			// Create API manager
			apiManager := createTestAPIManager(agentConfig)

			// Get license token
			token := apiManager.getLicenseToken()

			// Verify
			if token != tt.expectedToken {
				t.Errorf("Expected token '%s', got '%s'", tt.expectedToken, token)
			}
		})
	}
}
