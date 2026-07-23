package configuration

import (
	"os"
	"path/filepath"
	"testing"
)

func TestApplyLicenseSidecar_FillsWhenInlineEmpty(t *testing.T) {
	dir := t.TempDir()
	cfg := filepath.Join(dir, "agent.yaml")
	if err := os.WriteFile(LicenseSidecarPath(cfg), []byte("eyJ-sidecar-jwt\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	var c LocalConfigurationData
	if err := applyLicenseSidecar(&c, cfg); err != nil {
		t.Fatalf("applyLicenseSidecar: %v", err)
	}
	if c.Agent.License != "eyJ-sidecar-jwt" {
		t.Errorf("license = %q, want the trimmed sidecar token", c.Agent.License)
	}
}

func TestApplyLicenseSidecar_InlineWins(t *testing.T) {
	dir := t.TempDir()
	cfg := filepath.Join(dir, "agent.yaml")
	if err := os.WriteFile(LicenseSidecarPath(cfg), []byte("eyJ-sidecar-jwt\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	c := LocalConfigurationData{}
	c.Agent.License = "eyJ-inline-jwt"
	if err := applyLicenseSidecar(&c, cfg); err != nil {
		t.Fatalf("applyLicenseSidecar: %v", err)
	}
	if c.Agent.License != "eyJ-inline-jwt" {
		t.Errorf("license = %q, want the inline value to win over the sidecar", c.Agent.License)
	}
}

func TestApplyLicenseSidecar_NoSidecarIsFreeTier(t *testing.T) {
	dir := t.TempDir()
	cfg := filepath.Join(dir, "agent.yaml")

	var c LocalConfigurationData
	if err := applyLicenseSidecar(&c, cfg); err != nil {
		t.Fatalf("applyLicenseSidecar: %v", err)
	}
	if c.Agent.License != "" {
		t.Errorf("license = %q, want empty (free tier) when no sidecar exists", c.Agent.License)
	}
}

func TestWriteRemoveLicenseSidecar_RoundTrip(t *testing.T) {
	dir := t.TempDir()
	cfg := filepath.Join(dir, "agent.yaml")
	if err := os.WriteFile(cfg, []byte("config_version: 2\nagent:\n  key: k\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	if err := WriteLicenseSidecar(cfg, "eyJ-fake-jwt"); err != nil {
		t.Fatalf("WriteLicenseSidecar: %v", err)
	}
	got, err := os.ReadFile(LicenseSidecarPath(cfg))
	if err != nil {
		t.Fatalf("sidecar not written: %v", err)
	}
	if string(got) != "eyJ-fake-jwt\n" {
		t.Errorf("sidecar contents = %q, want the token with a trailing newline", got)
	}

	after, err := LoadFromDisk(cfg, nil)
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	if after.Agent.License != "eyJ-fake-jwt" {
		t.Errorf("loaded license = %q, want the sidecar token", after.Agent.License)
	}

	if err := RemoveLicenseSidecar(cfg); err != nil {
		t.Fatalf("RemoveLicenseSidecar: %v", err)
	}
	if _, err := os.Stat(LicenseSidecarPath(cfg)); !os.IsNotExist(err) {
		t.Errorf("sidecar still present after remove (err=%v)", err)
	}
	after, err = LoadFromDisk(cfg, nil)
	if err != nil {
		t.Fatalf("reload after remove: %v", err)
	}
	if after.Agent.License != "" {
		t.Errorf("loaded license = %q, want empty after remove", after.Agent.License)
	}
}

func TestResolveEffectiveLicense_Precedence(t *testing.T) {
	dir := t.TempDir()
	cfg := filepath.Join(dir, "agent.yaml")
	if err := os.WriteFile(LicenseSidecarPath(cfg), []byte("eyJ-sidecar\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	// Inline non-empty wins.
	got, err := ResolveEffectiveLicense(cfg, "eyJ-inline")
	if err != nil {
		t.Fatalf("resolve inline: %v", err)
	}
	if got != "eyJ-inline" {
		t.Errorf("got %q, want the inline value", got)
	}

	// Empty inline falls back to the sidecar.
	got, err = ResolveEffectiveLicense(cfg, "")
	if err != nil {
		t.Fatalf("resolve sidecar: %v", err)
	}
	if got != "eyJ-sidecar" {
		t.Errorf("got %q, want the sidecar value", got)
	}
}
