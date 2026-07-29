package configuration

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestWriteOTLPStrategyFragment(t *testing.T) {
	t.Run("http protocol written", func(t *testing.T) {
		dir := t.TempDir()
		if err := WriteOTLPStrategyFragment(dir, "vm.example:4318", "http"); err != nil {
			t.Fatalf("WriteOTLPStrategyFragment: %v", err)
		}
		body, err := os.ReadFile(filepath.Join(dir, "strategies.d", "10-otlp.yaml"))
		if err != nil {
			t.Fatalf("read fragment: %v", err)
		}
		if !strings.Contains(string(body), "endpoint: vm.example:4318") || !strings.Contains(string(body), "protocol: http") {
			t.Errorf("fragment missing endpoint/protocol:\n%s", body)
		}
	})

	t.Run("empty protocol defaults to grpc", func(t *testing.T) {
		dir := t.TempDir()
		if err := WriteOTLPStrategyFragment(dir, "otlp:4317", ""); err != nil {
			t.Fatalf("WriteOTLPStrategyFragment: %v", err)
		}
		body, _ := os.ReadFile(filepath.Join(dir, "strategies.d", "10-otlp.yaml"))
		if !strings.Contains(string(body), "protocol: grpc") {
			t.Errorf("empty protocol should default to grpc:\n%s", body)
		}
	})

	t.Run("invalid protocol rejected", func(t *testing.T) {
		dir := t.TempDir()
		if err := WriteOTLPStrategyFragment(dir, "otlp:4317", "thrift"); err == nil {
			t.Error("invalid protocol must be rejected, not written")
		}
	})

	t.Run("empty endpoint is a no-op", func(t *testing.T) {
		dir := t.TempDir()
		if err := WriteOTLPStrategyFragment(dir, "", "grpc"); err != nil {
			t.Fatalf("no-op should not error: %v", err)
		}
		if _, err := os.Stat(filepath.Join(dir, "strategies.d", "10-otlp.yaml")); !os.IsNotExist(err) {
			t.Error("no fragment should be written for an empty endpoint")
		}
	})
}

func TestApplyInstallOverrides(t *testing.T) {
	dir := t.TempDir()
	cfg := filepath.Join(dir, "agent.yaml")
	// A minimal generated-style agent.yaml with an agent block and comments.
	seed := `# SenHub Agent — global configuration
config_version: 2

agent:
  key: 11111111-2222-3333-4444-555555555555
  # license: ""  # add your license token here
`
	if err := os.WriteFile(cfg, []byte(seed), 0o600); err != nil {
		t.Fatal(err)
	}

	if err := ApplyInstallOverrides(cfg, "eyJ-fake-jwt", map[string]string{"site": "paris", "env": "prod"}); err != nil {
		t.Fatalf("ApplyInstallOverrides: %v", err)
	}

	// Re-load through the real loader: license + global_tags must be set.
	after, err := LoadFromDisk(cfg, nil)
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	if after.Agent.License != "eyJ-fake-jwt" {
		t.Errorf("license = %q, want the provisioned token", after.Agent.License)
	}
	if after.Agent.GlobalTags["site"] != "paris" || after.Agent.GlobalTags["env"] != "prod" {
		t.Errorf("global_tags = %v, want site=paris env=prod", after.Agent.GlobalTags)
	}
	// The agent key must be preserved, not clobbered.
	if after.Agent.Key != "11111111-2222-3333-4444-555555555555" {
		t.Errorf("agent key changed to %q", after.Agent.Key)
	}
	// A template comment survives the node-level edit.
	raw, _ := os.ReadFile(cfg)
	if !strings.Contains(string(raw), "SenHub Agent — global configuration") {
		t.Errorf("header comment lost:\n%s", raw)
	}

	// Empty inputs are a no-op (no error, file unchanged in content shape).
	if err := ApplyInstallOverrides(cfg, "", nil); err != nil {
		t.Fatalf("no-op ApplyInstallOverrides: %v", err)
	}
}

// TestSetLicenseField_PreservesMultiFileLayout is the regression test for C1:
// activating/removing a license on a multi-file install must not re-emit empty
// probes:/storage: blocks into agent.yaml — doing so flips isLegacyMonolithic
// back to true and makes LoadFromDisk ignore probes.d/, leaving the agent with
// zero probes. The pre-fix code (full unmarshal/marshal of
// LocalConfigurationData) fails this test; SetLicenseField passes it.
func TestSetLicenseField_PreservesMultiFileLayout(t *testing.T) {
	dir := t.TempDir()
	cfg := filepath.Join(dir, "agent.yaml")
	if err := os.WriteFile(cfg, []byte(`# SenHub Agent — global configuration
config_version: 2
agent:
  key: 11111111-2222-3333-4444-555555555555
`), 0o600); err != nil {
		t.Fatal(err)
	}
	probesDir := filepath.Join(dir, "probes.d")
	if err := os.MkdirAll(probesDir, 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(probesDir, "10-cpu.yaml"), []byte(`- name: cpu-local
  type: cpu
`), 0o600); err != nil {
		t.Fatal(err)
	}

	assertMultiFileIntact := func(t *testing.T, wantLicense string) {
		t.Helper()
		raw, _ := os.ReadFile(cfg)
		if HasMonolithicMarkers(raw) {
			t.Fatalf("agent.yaml flipped to monolithic (probes:/storage: re-emitted):\n%s", raw)
		}
		after, err := LoadFromDisk(cfg, nil)
		if err != nil {
			t.Fatalf("reload: %v", err)
		}
		if got := len(after.Probes); got != 1 || after.Probes[0].Name != "cpu-local" {
			t.Fatalf("probes.d dropped: got %d probes %v, want the cpu-local probe", got, after.Probes)
		}
		if after.Agent.License != wantLicense {
			t.Errorf("license = %q, want %q", after.Agent.License, wantLicense)
		}
		if after.Agent.Key != "11111111-2222-3333-4444-555555555555" {
			t.Errorf("agent key changed to %q", after.Agent.Key)
		}
	}

	// activate
	if err := SetLicenseField(cfg, "eyJ-fake-jwt"); err != nil {
		t.Fatalf("SetLicenseField activate: %v", err)
	}
	assertMultiFileIntact(t, "eyJ-fake-jwt")

	// remove
	if err := SetLicenseField(cfg, ""); err != nil {
		t.Fatalf("SetLicenseField remove: %v", err)
	}
	assertMultiFileIntact(t, "")
}
