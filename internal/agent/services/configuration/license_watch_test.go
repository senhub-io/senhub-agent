package configuration

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"senhub-agent.go/internal/agent/cliArgs"
)

func TestTopLevelEventWanted(t *testing.T) {
	tests := []struct {
		name       string
		configPath string
		eventName  string
		want       bool
	}{
		{
			name:       "main config file is wanted",
			configPath: "/etc/senhub-agent/agent.yaml",
			eventName:  "/etc/senhub-agent/agent.yaml",
			want:       true,
		},
		{
			name:       "license sidecar is wanted",
			configPath: "/etc/senhub-agent/agent.yaml",
			eventName:  "/etc/senhub-agent/license.jwt",
			want:       true,
		},
		{
			name:       "revocations sidecar is wanted",
			configPath: "/etc/senhub-agent/agent.yaml",
			eventName:  "/etc/senhub-agent/license.revocations.jwt",
			want:       true,
		},
		{
			name:       "unrelated file beside config is not wanted",
			configPath: "/etc/senhub-agent/agent.yaml",
			eventName:  "/etc/senhub-agent/notes.txt",
			want:       false,
		},
		{
			name:       "backup of config is not wanted",
			configPath: "/etc/senhub-agent/agent.yaml",
			eventName:  "/etc/senhub-agent/agent.yaml.bak",
			want:       false,
		},
		{
			name:       "fragment event is left to shouldIgnoreEvent",
			configPath: "/etc/senhub-agent/agent.yaml",
			eventName:  "/etc/senhub-agent/probes.d/10-x.yaml",
			want:       true,
		},
		{
			name:       "custom config path: own basename is wanted",
			configPath: "/opt/x/custom.yaml",
			eventName:  "/opt/x/custom.yaml",
			want:       true,
		},
		{
			name:       "custom config path: agent.yaml in same dir is not wanted",
			configPath: "/opt/x/custom.yaml",
			eventName:  "/opt/x/agent.yaml",
			want:       false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := topLevelEventWanted(tt.configPath, tt.eventName); got != tt.want {
				t.Errorf("topLevelEventWanted(%q, %q) = %v, want %v",
					tt.configPath, tt.eventName, got, tt.want)
			}
		})
	}
}

func TestLicenseSidecarAppliesWhileRunning(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "agent.yaml")

	lc := NewLocalConfiguration(&cliArgs.ParsedArgs{ConfigPath: configPath}, createTestLocalLogger())

	runCtx, cancelRun := context.WithCancel(context.Background())
	defer cancelRun()
	if err := lc.Start(runCtx); err != nil {
		t.Fatalf("Start: %v", err)
	}
	t.Cleanup(func() { _ = lc.Shutdown(context.Background()) })

	if err := WriteLicenseSidecar(configPath, "header.payload.sig-a"); err != nil {
		t.Fatalf("WriteLicenseSidecar: %v", err)
	}
	waitForLicense(t, lc, "header.payload.sig-a")

	if err := RemoveLicenseSidecar(configPath); err != nil {
		t.Fatalf("RemoveLicenseSidecar: %v", err)
	}
	waitForLicense(t, lc, "")

	if err := WriteRevocationsSidecar(configPath, "header.payload.sig-r"); err != nil {
		t.Fatalf("WriteRevocationsSidecar: %v", err)
	}
	waitForRevocations(t, lc, "header.payload.sig-r")
}

func waitForLicense(t *testing.T, lc *LocalConfiguration, want string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if lc.GetConfiguration().Agent.License == want {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for Agent.License to become %q, got %q", want, lc.GetConfiguration().Agent.License)
}

func waitForRevocations(t *testing.T, lc *LocalConfiguration, want string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if lc.GetConfiguration().Agent.LicenseRevocations == want {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for Agent.LicenseRevocations to become %q, got %q", want, lc.GetConfiguration().Agent.LicenseRevocations)
}
