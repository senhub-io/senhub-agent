//go:build linux

package configuration

import (
	"os"
	"path/filepath"
	"syscall"
	"testing"
)

func assertOwnerMode(t *testing.T, path string, uid, gid uint32, mode os.FileMode) {
	t.Helper()

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("%s: stat: %v", filepath.Base(path), err)
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		t.Fatalf("%s: unexpected stat type %T", filepath.Base(path), info.Sys())
	}
	if stat.Uid != uid || stat.Gid != gid || info.Mode().Perm() != mode {
		t.Errorf("%s is owned by %d:%d mode %#o after license activate as root, want %d:%d mode %#o (the service reads it through its group)",
			filepath.Base(path), stat.Uid, stat.Gid, info.Mode().Perm(), uid, gid, mode)
	}
}

func TestLicenseActivateKeepsAgentYAMLModeWhenRewritten(t *testing.T) {
	dir := t.TempDir()
	cfg := filepath.Join(dir, "agent.yaml")
	writeSealFile(t, cfg, "config_version: 2\nagent:\n  key: \"k\"\n  license: \"inline\"\n")
	if err := os.Chmod(cfg, 0o640); err != nil {
		t.Fatalf("chmod agent.yaml: %v", err)
	}

	if err := WriteLicenseSidecar(cfg, "eyJ.recette.sig"); err != nil {
		t.Fatalf("WriteLicenseSidecar: %v", err)
	}

	info, err := os.Stat(cfg)
	if err != nil {
		t.Fatalf("stat agent.yaml: %v", err)
	}
	if got := info.Mode().Perm(); got != 0o640 {
		t.Errorf("agent.yaml mode is %#o after license activate, want 0640", got)
	}

	content, err := os.ReadFile(cfg)
	if err != nil {
		t.Fatalf("read agent.yaml: %v", err)
	}
	if contains(string(content), "inline") {
		t.Error("agent.yaml still contains the inline license after license activate")
	}
	if !contains(string(content), `key: "k"`) && !contains(string(content), "key: k") {
		t.Error("agent.yaml lost its agent key after license activate")
	}

	licensePath := filepath.Join(dir, "license.jwt")
	license, err := os.ReadFile(licensePath)
	if err != nil {
		t.Fatalf("read license.jwt: %v", err)
	}
	if string(license) != "eyJ.recette.sig\n" {
		t.Errorf("license.jwt content is %q, want %q", license, "eyJ.recette.sig\n")
	}
}

func TestLicenseActivateAsRootUnderRootOwnedDirKeepsServiceGroup(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("needs root: ownership after a privileged write is under test")
	}

	const serviceGID = 4343
	dir := t.TempDir()
	if err := os.Chown(dir, 0, 0); err != nil {
		t.Fatalf("chown directory: %v", err)
	}
	if err := os.Chmod(dir, 0o755); err != nil {
		t.Fatalf("chmod directory: %v", err)
	}

	cfg := filepath.Join(dir, "agent.yaml")
	writeSealFile(t, cfg, "config_version: 2\nagent:\n  key: \"k\"\n  license: \"inline\"\n")
	if err := os.Chown(cfg, 0, serviceGID); err != nil {
		t.Fatalf("chown agent.yaml: %v", err)
	}
	if err := os.Chmod(cfg, 0o640); err != nil {
		t.Fatalf("chmod agent.yaml: %v", err)
	}

	if err := WriteLicenseSidecar(cfg, "eyJ.recette.sig"); err != nil {
		t.Fatalf("WriteLicenseSidecar: %v", err)
	}

	assertOwnerMode(t, cfg, 0, serviceGID, 0o640)
	assertOwnerMode(t, filepath.Join(dir, "license.jwt"), 0, serviceGID, 0o640)
}
