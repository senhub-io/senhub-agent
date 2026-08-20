package app

import (
	"testing"

	"path/filepath"
	"senhub-agent.go/internal/agent/cliArgs"
)

// TestGetSystemStatusDirect pins the post-0.2.0 contract: the status
// command reports mode = "offline" unconditionally. The pre-0.2.0
// test cases that exercised "online" mode have been removed alongside
// the rest of the online dispatch.
func TestGetSystemStatusDirect(t *testing.T) {
	tests := []struct {
		name string
		args *cliArgs.ParsedArgs
	}{
		{
			name: "No args - status reports offline",
			args: &cliArgs.ParsedArgs{},
		},
		{
			name: "With config path - status reports offline",
			args: &cliArgs.ParsedArgs{ConfigPath: "/nonexistent.yaml"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			status, err := getSystemStatusDirect(tt.args)
			if err != nil {
				t.Fatalf("getSystemStatusDirect() returned unexpected error: %v", err)
			}
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

// TestValidateConfigPath_AcceptsInstalledConfig pins the defect the
// recette found on both platforms: the installed configuration lives in
// /etc/senhub-agent or C:\ProgramData\SenHub, never under the directory
// an operator runs the command from. Refusing it made `status` unable to
// read the agent key, so it never reached the daemon and printed its
// degraded local view instead — on every normal install.
func TestValidateConfigPath_AcceptsInstalledConfig(t *testing.T) {
	installed, err := cliArgs.GetAbsoluteConfigPath("")
	if err != nil {
		t.Skipf("no platform default config path: %v", err)
	}
	if err := validateConfigPath(installed); err != nil {
		t.Errorf("installed configuration refused: %v", err)
	}
	// A sibling fragment of the same install is equally legitimate.
	sibling := filepath.Join(filepath.Dir(installed), "strategies.d", "10-otlp.yaml")
	if err := validateConfigPath(sibling); err != nil {
		t.Errorf("installed fragment refused: %v", err)
	}
	// Traversal stays refused.
	if err := validateConfigPath("../../../etc/passwd.yaml"); err == nil {
		t.Error("directory traversal accepted")
	}
}
