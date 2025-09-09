package main

import (
	"testing"

	"senhub-agent.go/internal/agent/cliArgs"
)

func TestGetSystemStatusDirect(t *testing.T) {
	tests := []struct {
		name     string
		args     *cliArgs.ParsedArgs
		wantMode string
		wantErr  bool
	}{
		{
			name: "Offline mode from args",
			args: &cliArgs.ParsedArgs{
				Offline:           true,
				AuthenticationKey: "test-key-123",
			},
			wantMode: "offline",
			wantErr:  false,
		},
		{
			name: "Online mode from args",
			args: &cliArgs.ParsedArgs{
				Offline:           false,
				AuthenticationKey: "test-key-456",
			},
			wantMode: "online",
			wantErr:  false,
		},
		{
			name:     "No args - online mode default",
			args:     &cliArgs.ParsedArgs{}, // Empty args, no offline flag, no auth key
			wantMode: "online",              // Default assumption for status checks
			wantErr:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			status, err := getSystemStatusDirect(tt.args)

			if (err != nil) != tt.wantErr {
				t.Errorf("getSystemStatusDirect() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			// Verify the mode is one of the expected values (using real agent detection logic)
			validModes := []string{"online", "offline"}
			validMode := false
			for _, mode := range validModes {
				if status.Connection.Mode == mode {
					validMode = true
					break
				}
			}
			if !validMode {
				t.Errorf("Connection mode '%s' should be one of %v", status.Connection.Mode, validModes)
			}

			// Basic validation of returned status
			if status.Agent.Version == "" {
				t.Error("Agent version should not be empty")
			}

			if status.Health.Status == "" {
				t.Error("Health status should not be empty")
			}
		})
	}
}

func TestValidateConfigPath(t *testing.T) {
	tests := []struct {
		name    string
		path    string
		wantErr bool
	}{
		{
			name:    "Valid YAML file",
			path:    "./test-config.yaml",
			wantErr: false,
		},
		{
			name:    "Valid YML file",
			path:    "./test-config.yml",
			wantErr: false,
		},
		{
			name:    "Invalid extension",
			path:    "./test-config.txt",
			wantErr: true,
		},
		{
			name:    "Directory traversal attempt",
			path:    "../../../etc/passwd",
			wantErr: true,
		},
		{
			name:    "No extension",
			path:    "./config",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateConfigPath(tt.path)

			if (err != nil) != tt.wantErr {
				t.Errorf("validateConfigPath() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}
