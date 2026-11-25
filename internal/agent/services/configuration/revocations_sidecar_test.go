package configuration

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/rs/zerolog"

	"senhub-agent.go/internal/agent/services/logger"
)

func TestRevocationsSidecarRoundTrip(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "agent.yaml")
	if err := os.WriteFile(configPath, []byte("agent:\n"), 0600); err != nil {
		t.Fatal(err)
	}

	got, err := ReadRevocationsSidecar(configPath)
	if err != nil {
		t.Fatalf("read absent sidecar: %v", err)
	}
	if got != "" {
		t.Fatalf("absent sidecar: got %q, want empty", got)
	}

	token := "abc.def.ghi"
	if err := WriteRevocationsSidecar(configPath, token); err != nil {
		t.Fatalf("write: %v", err)
	}
	got, err = ReadRevocationsSidecar(configPath)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if got != token {
		t.Fatalf("round trip: got %q, want %q", got, token)
	}

	// agent.yaml must never be touched by the revocations sidecar write.
	data, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "agent:\n" {
		t.Fatalf("agent.yaml was rewritten: %q", string(data))
	}

	if runtime.GOOS != "windows" {
		info, err := os.Stat(RevocationsSidecarPath(configPath))
		if err != nil {
			t.Fatal(err)
		}
		if info.Mode().Perm() != 0600 {
			t.Fatalf("mode: got %o, want 600", info.Mode().Perm())
		}
	}
}

// A licence or revocation list changed alone must notify the sensor, or a
// licence received at runtime waits for a restart.
func TestLicenceChangeIsAConfigurationChange(t *testing.T) {
	zl := zerolog.Nop()
	lc := &LocalConfiguration{logger: logger.NewModuleLogger((*logger.Logger)(&zl), "configuration.local.test")}
	tests := []struct {
		name  string
		agent LocalAgentConfig
	}{
		{name: "licence", agent: LocalAgentConfig{License: "a.b.c"}},
		{name: "revocation list", agent: LocalAgentConfig{LicenseRevocations: "d.e.f"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if !lc.hasConfigurationChanged(LocalConfigurationData{}, LocalConfigurationData{Agent: tt.agent}) {
				t.Fatal("change not detected")
			}
		})
	}
}

// The loader reads the revocation list beside the licence, even when the
// licence itself is inline.
func TestApplyLicenseSidecar_ReadsTheRevocationList(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "agent.yaml")
	if err := WriteRevocationsSidecar(configPath, "d.e.f"); err != nil {
		t.Fatal(err)
	}
	cfg := LocalConfigurationData{Agent: LocalAgentConfig{License: "inline"}}
	if err := applyLicenseSidecar(&cfg, configPath); err != nil {
		t.Fatal(err)
	}
	if cfg.Agent.LicenseRevocations != "d.e.f" {
		t.Fatalf("LicenseRevocations = %q, want d.e.f", cfg.Agent.LicenseRevocations)
	}
}
