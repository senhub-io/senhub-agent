package configuration

import (
	"os"
	"path/filepath"
	"strings"
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

func TestMigrateLicenseToSidecar_MovesInlineJWT(t *testing.T) {
	dir := t.TempDir()
	cfg := filepath.Join(dir, "agent.yaml")
	if err := os.WriteFile(cfg, []byte("config_version: 2\nagent:\n  key: k\n  license: eyJ-inline-jwt\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	if err := MigrateLicenseToSidecar(cfg, nil); err != nil {
		t.Fatalf("MigrateLicenseToSidecar: %v", err)
	}

	// The sidecar now holds the token.
	got, err := os.ReadFile(LicenseSidecarPath(cfg))
	if err != nil {
		t.Fatalf("sidecar not written: %v", err)
	}
	if strings.TrimSpace(string(got)) != "eyJ-inline-jwt" {
		t.Errorf("sidecar = %q, want the migrated token", got)
	}
	// The inline field is cleared, but the effective license is unchanged.
	inline, err := readInlineLicense(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if inline != "" {
		t.Errorf("inline license = %q, want cleared after migration", inline)
	}
	after, err := LoadFromDisk(cfg, nil)
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	if after.Agent.License != "eyJ-inline-jwt" {
		t.Errorf("effective license = %q, want unchanged after migration", after.Agent.License)
	}
	// No backup file left behind on success.
	entries, _ := filepath.Glob(cfg + ".backup.*")
	if len(entries) != 0 {
		t.Errorf("migration left backup files: %v", entries)
	}

	// Idempotent: a second run is a no-op (nothing inline to move).
	if err := MigrateLicenseToSidecar(cfg, nil); err != nil {
		t.Fatalf("second MigrateLicenseToSidecar: %v", err)
	}
}

func TestMigrateLicenseToSidecar_NoOpOnReferenceOrEmpty(t *testing.T) {
	for _, tc := range []struct {
		name    string
		license string
	}{
		{"empty", ""},
		{"file-reference", "${file:/etc/senhub/license.jwt}"},
		{"secret-reference", "${secret:agent.license}"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			cfg := filepath.Join(dir, "agent.yaml")
			body := "config_version: 2\nagent:\n  key: k\n"
			if tc.license != "" {
				body += "  license: \"" + tc.license + "\"\n"
			}
			if err := os.WriteFile(cfg, []byte(body), 0o600); err != nil {
				t.Fatal(err)
			}

			if err := MigrateLicenseToSidecar(cfg, nil); err != nil {
				t.Fatalf("MigrateLicenseToSidecar: %v", err)
			}
			if _, err := os.Stat(LicenseSidecarPath(cfg)); !os.IsNotExist(err) {
				t.Errorf("sidecar created for %s (err=%v), want no-op", tc.name, err)
			}
			inline, _ := readInlineLicense(cfg)
			if inline != tc.license {
				t.Errorf("inline license = %q, want unchanged %q", inline, tc.license)
			}
		})
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
