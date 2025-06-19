package agent

import (
	"os"
	"path/filepath"
	"testing"

	"senhub-agent.go/internal/agent/cliArgs"
	"senhub-agent.go/internal/agent/services/logger"
)

func TestDetectAgentMode(t *testing.T) {
	tests := []struct {
		name           string
		configContent  string
		cliArgs        *cliArgs.ParsedArgs
		expectedMode   bool // true = offline, false = online
		expectAuthKey  string
		description    string
	}{
		{
			name: "Offline mode from config file",
			configContent: `# Agent configuration
agent:
  key: "offline-test-key-123456789"
  mode: offline
  generated: false

storage:
  - name: http
    params:
      port: 8080
      endpoints: ["prtg", "web"]

probes:
  - name: cpu
    params:
      interval: 30
`,
			cliArgs: &cliArgs.ParsedArgs{
				AuthenticationKey: "",
				ConfigPath:        "",
				Offline:           false,
			},
			expectedMode:  true,
			expectAuthKey: "offline-test-key-123456789",
			description:   "Should detect offline mode from config file and extract authentication key",
		},
		{
			name: "Online mode from config file",
			configContent: `# Agent configuration (replicated from SenHub server)
agent:
  key: "online-test-key-987654321"
  mode: online
  generated: false

storage:
  - name: senhub
    params: {}

probes:
  - name: cpu
    params: {}
`,
			cliArgs: &cliArgs.ParsedArgs{
				AuthenticationKey: "",
				ConfigPath:        "",
				Offline:           false,
			},
			expectedMode:  false,
			expectAuthKey: "online-test-key-987654321",
			description:   "Should detect online mode from config file and extract authentication key",
		},
		{
			name: "CLI offline flag takes precedence",
			configContent: `# Agent configuration
agent:
  key: "config-file-key"
  mode: online
  generated: false
`,
			cliArgs: &cliArgs.ParsedArgs{
				AuthenticationKey: "cli-key-123",
				ConfigPath:        "",
				Offline:           true, // Explicit CLI offline flag
			},
			expectedMode:  true,
			expectAuthKey: "cli-key-123", // CLI key should be preserved
			description:   "CLI offline flag should take precedence over config file mode",
		},
		{
			name: "No config file but has CLI key - online mode",
			configContent: "", // No config file
			cliArgs: &cliArgs.ParsedArgs{
				AuthenticationKey: "cli-key-provided",
				ConfigPath:        "",
				Offline:           false,
			},
			expectedMode:  false, // Should use online mode with CLI key
			expectAuthKey: "cli-key-provided",
			description:   "With CLI key but no config file, should use online mode",
		},
		{
			name: "Config file with CLI key mismatch",
			configContent: `# Agent configuration
agent:
  key: "config-file-key-555"
  mode: offline
  generated: true
`,
			cliArgs: &cliArgs.ParsedArgs{
				AuthenticationKey: "cli-key-different",
				ConfigPath:        "",
				Offline:           false,
			},
			expectedMode:  true,
			expectAuthKey: "cli-key-different", // CLI key is preserved (validation logic is environment-dependent)
			description:   "Should respect CLI key when provided, config file determines mode",
		},
		{
			name: "No CLI key, use config file key",
			configContent: `# Agent configuration
agent:
  key: "config-only-key-555666777"
  mode: offline
  generated: true

storage:
  - name: http
    params:
      port: 8080
`,
			cliArgs: &cliArgs.ParsedArgs{
				AuthenticationKey: "", // No CLI key
				ConfigPath:        "",
				Offline:           false,
			},
			expectedMode:  true,
			expectAuthKey: "config-only-key-555666777",
			description:   "Should use authentication key from config file when no CLI key provided",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create temporary config file if content provided
			var configPath string
			if tt.configContent != "" {
				tempDir := t.TempDir()
				configPath = filepath.Join(tempDir, "agent-config.yaml")
				err := os.WriteFile(configPath, []byte(tt.configContent), 0644)
				if err != nil {
					t.Fatalf("Failed to create test config file: %v", err)
				}
				tt.cliArgs.ConfigPath = configPath
			}

			// Store original values to check modifications
			originalAuthKey := tt.cliArgs.AuthenticationKey

			// Call the function under test
			result := DetectAgentMode(tt.cliArgs)

			// Verify mode detection
			if result != tt.expectedMode {
				t.Errorf("DetectAgentMode() = %v, expected %v. %s", result, tt.expectedMode, tt.description)
			}

			// Verify authentication key handling
			if tt.expectAuthKey != "" && tt.cliArgs.AuthenticationKey != tt.expectAuthKey {
				t.Errorf("Expected authentication key '%s', got '%s'. %s", 
					tt.expectAuthKey, tt.cliArgs.AuthenticationKey, tt.description)
			}

			// Verify that CLI args are properly modified
			if tt.configContent != "" && tt.expectAuthKey != "" {
				if originalAuthKey == "" && tt.cliArgs.AuthenticationKey == "" {
					t.Error("Authentication key should have been set from config file")
				}
			}

			// Verify offline flag is set correctly
			expectedOfflineFlag := tt.expectedMode
			if tt.cliArgs.Offline != expectedOfflineFlag {
				t.Errorf("Expected Offline flag to be %v, got %v", expectedOfflineFlag, tt.cliArgs.Offline)
			}
		})
	}
}

func TestLoadLocalConfigInfo(t *testing.T) {
	tests := []struct {
		name           string
		configContent  string
		expectedValid  bool
		expectedMode   string
		expectedKey    string
		description    string
	}{
		{
			name: "Valid offline configuration",
			configContent: `agent:
  key: "test-key-123"
  mode: offline
  generated: false`,
			expectedValid: true,
			expectedMode:  "offline",
			expectedKey:   "test-key-123",
			description:   "Should parse valid offline configuration",
		},
		{
			name: "Valid online configuration",
			configContent: `agent:
  key: "test-key-456"
  mode: online
  generated: false`,
			expectedValid: true,
			expectedMode:  "online",
			expectedKey:   "test-key-456",
			description:   "Should parse valid online configuration",
		},
		{
			name: "Invalid YAML",
			configContent: `agent:
  key: "test-key-789
  mode: offline
  invalid yaml structure`,
			expectedValid: false,
			expectedMode:  "",
			expectedKey:   "",
			description:   "Should handle invalid YAML gracefully",
		},
		{
			name: "Missing agent section",
			configContent: `storage:
  - name: http
probes:
  - name: cpu`,
			expectedValid: false, // No authentication key found
			expectedMode:  "offline", // Default mode when none specified
			expectedKey:   "",
			description:   "Should handle missing agent section gracefully",
		},
		{
			name: "Empty configuration",
			configContent: "",
			expectedValid: false,
			expectedMode:  "",
			expectedKey:   "",
			description:   "Should handle empty configuration",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create temporary config file
			tempDir := t.TempDir()
			configPath := filepath.Join(tempDir, "agent-config.yaml")
			
			if tt.configContent != "" {
				err := os.WriteFile(configPath, []byte(tt.configContent), 0644)
				if err != nil {
					t.Fatalf("Failed to create test config file: %v", err)
				}
			}

			// Create test args
			args := &cliArgs.ParsedArgs{
				ConfigPath: configPath,
			}

			// Create test logger
			tempLogger := logger.NewLogger(args)

			// Call the function under test
			result := loadLocalConfigInfo(args, tempLogger)

			// Verify results
			if result.IsValid != tt.expectedValid {
				t.Errorf("Expected IsValid=%v, got %v. %s", tt.expectedValid, result.IsValid, tt.description)
			}

			if result.Mode != tt.expectedMode {
				t.Errorf("Expected Mode='%s', got '%s'. %s", tt.expectedMode, result.Mode, tt.description)
			}

			if result.AuthenticationKey != tt.expectedKey {
				t.Errorf("Expected AuthenticationKey='%s', got '%s'. %s", tt.expectedKey, result.AuthenticationKey, tt.description)
			}

			if result.ConfigPath != configPath {
				t.Errorf("Expected ConfigPath='%s', got '%s'", configPath, result.ConfigPath)
			}
		})
	}
}

func TestDetectAgentModeBackwardCompatibility(t *testing.T) {
	t.Run("Legacy CLI-only usage", func(t *testing.T) {
		// Test that existing CLI-only deployments continue to work
		args := &cliArgs.ParsedArgs{
			AuthenticationKey: "legacy-cli-key-123",
			ConfigPath:        "", // No config file specified
			Offline:           false,
		}

		result := DetectAgentMode(args)

		// Should default to online mode for legacy CLI usage
		if result != false {
			t.Error("Legacy CLI-only usage should default to online mode")
		}

		// Should preserve the CLI authentication key
		if args.AuthenticationKey != "legacy-cli-key-123" {
			t.Error("CLI authentication key should be preserved in legacy mode")
		}
	})

	t.Run("Legacy offline CLI flag", func(t *testing.T) {
		// Test that explicit --offline flag still works
		args := &cliArgs.ParsedArgs{
			AuthenticationKey: "legacy-offline-key",
			ConfigPath:        "",
			Offline:           true, // Explicit offline flag
		}

		result := DetectAgentMode(args)

		// Should respect explicit offline flag
		if result != true {
			t.Error("Explicit --offline flag should be respected")
		}

		// Should preserve the CLI authentication key
		if args.AuthenticationKey != "legacy-offline-key" {
			t.Error("CLI authentication key should be preserved with explicit offline flag")
		}
	})
}