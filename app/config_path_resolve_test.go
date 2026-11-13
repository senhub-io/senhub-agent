package app

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestServiceConfigPathFromImagePath(t *testing.T) {
	tests := []struct {
		name      string
		imagePath string
		want      string
	}{
		{
			name:      "quoted exe with spaces and quoted value",
			imagePath: `"C:\Program Files\SenHub\senhub-agent.exe" --config-path "C:\SenHub\agent.yaml"`,
			want:      `C:\SenHub\agent.yaml`,
		},
		{
			name:      "equals form",
			imagePath: `C:\SenHub\senhub-agent.exe --config-path=C:\x\agent.yaml`,
			want:      `C:\x\agent.yaml`,
		},
		{
			name:      "no flag",
			imagePath: `"C:\SenHub\senhub-agent.exe" --run`,
			want:      "",
		},
		{
			name:      "flag without value",
			imagePath: `C:\SenHub\senhub-agent.exe --config-path`,
			want:      "",
		},
		{
			name:      "extra args after value",
			imagePath: `C:\SenHub\senhub-agent.exe --config-path C:\SenHub\agent.yaml --extra arg`,
			want:      `C:\SenHub\agent.yaml`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := serviceConfigPathFromImagePath(tt.imagePath); got != tt.want {
				t.Fatalf("serviceConfigPathFromImagePath(%q) = %q, want %q", tt.imagePath, got, tt.want)
			}
		})
	}
}

func TestResolveCLIConfigPathWithFlag(t *testing.T) {
	path, origin, err := resolveCLIConfigPath("agent.yaml")
	if err != nil {
		t.Fatalf("resolveCLIConfigPath returned error: %v", err)
	}
	if origin != "--config-path" {
		t.Fatalf("origin = %q, want %q", origin, "--config-path")
	}
	if !filepath.IsAbs(path) {
		t.Fatalf("path %q is not absolute", path)
	}
}

func TestDoctorWarnsWhenNoConfigurationWasFound(t *testing.T) {
	d := healthyDoctorDeps()
	d.defaultMissing = true
	checks := configCheckChecks(d, doctorConfig{})
	if len(checks) != 1 || checks[0].Level == "fail" || !strings.Contains(checks[0].Message, "no configuration found") {
		t.Fatalf("checks = %+v, want one warning saying where it looked", checks)
	}
}
