package configuration

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"senhub-agent.go/internal/agent/cliArgs"
)

func TestRemoteConfiguration_LocalReplication(t *testing.T) {
	// Create temporary directory for test
	tempDir := t.TempDir()
	configPath := filepath.Join(tempDir, "test-config.yaml")

	args := &cliArgs.ParsedArgs{
		AuthenticationKey: "test-key-12345678",
		ServerUrl:         "https://test.senhub.io",
		ConfigPath:        configPath,
	}

	// Create mock configuration with test data
	mockConfig := NewMockRemoteConfiguration("https://test.server", `{
		"storage": [
			{
				"name": "http",
				"params": {
					"port": 8080,
					"endpoints": ["prtg", "senhub"]
				}
			}
		],
		"probes": [
			{
				"name": "cpu",
				"params": {
					"interval": 30
				}
			}
		],
		"agent": {
			"registry_url": "https://eu-west-1.intake.senhub.io/releases",
			"version": "1.0.0",
			"update_check_interval": 3600
		},
		"cache": {
			"retention_minutes": 5
		}
	}`)

	// Set replication parameters
	mockConfig.SetReplicationParams(args)

	// Test replication
	err := mockConfig.TestReplication()
	if err != nil {
		t.Fatalf("Replication failed: %v", err)
	}

	// Verify file was created
	if _, err := os.Stat(configPath); os.IsNotExist(err) {
		t.Fatalf("Configuration file was not created: %s", configPath)
	}

	// Read and verify content
	content, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("Failed to read configuration file: %v", err)
	}

	contentStr := string(content)

	// Verify key elements
	if !strings.Contains(contentStr, "mode: online") {
		t.Error("Configuration should have mode: online")
	}

	if !strings.Contains(contentStr, args.AuthenticationKey) {
		t.Error("Configuration should contain the authentication key")
	}

	if !strings.Contains(contentStr, "name: http") {
		t.Error("Configuration should contain HTTP storage strategy")
	}

	if !strings.Contains(contentStr, "name: cpu") {
		t.Error("Configuration should contain CPU probe")
	}

	if !strings.Contains(contentStr, "type: cpu") {
		t.Error("Configuration should contain CPU probe type (v2 format)")
	}

	if !strings.Contains(contentStr, "# SenHub Agent Configuration (Replicated from Server)") {
		t.Error("Configuration should contain replication comment")
	}

	if !strings.Contains(contentStr, "generated: false") {
		t.Error("Configuration should indicate key was not generated")
	}

	t.Logf("✅ Configuration successfully replicated to: %s", configPath)
}

func TestRemoteConfiguration_ReplicationDisabledWithoutArgs(t *testing.T) {
	// Create mock without args (should disable replication)
	mockConfig := NewMockRemoteConfiguration("https://test.server", `{
		"storage": [],
		"probes": [],
		"agent": {}
	}`)

	// Try to replicate (should fail gracefully)
	err := mockConfig.TestReplication()
	if err == nil {
		t.Error("Replication should fail when no args are provided")
	}

	expectedError := "local replica path not configured"
	if !strings.Contains(err.Error(), expectedError) {
		t.Errorf("Expected error containing '%s', got: %v", expectedError, err)
	}
}

func TestRemoteConfiguration_MaskKey(t *testing.T) {
	tests := []struct {
		name     string
		key      string
		expected string
	}{
		{
			name:     "Normal key",
			key:      "abcd1234567890xyz",
			expected: "abcd***0xyz",
		},
		{
			name:     "Short key",
			key:      "abc123",
			expected: "***",
		},
		{
			name:     "Minimum length key",
			key:      "12345678",
			expected: "***",
		},
		{
			name:     "Long key",
			key:      "test-authentication-key-very-long",
			expected: "test***long",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := maskKey(tt.key)
			if result != tt.expected {
				t.Errorf("maskKey(%s) = %s, expected %s", tt.key, result, tt.expected)
			}
		})
	}
}
