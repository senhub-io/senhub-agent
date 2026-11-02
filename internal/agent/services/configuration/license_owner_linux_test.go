//go:build linux

package configuration

import (
	"os"
	"path/filepath"
	"syscall"
	"testing"
)

func TestRootLicenseActivateLeavesASidecarTheServiceGroupReads(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("needs root: the fix is about a root-written sidecar")
	}
	dir := t.TempDir()
	cfg := filepath.Join(dir, "agent.yaml")
	if err := os.WriteFile(cfg, []byte("agent:\n  name: x\n"), 0o640); err != nil {
		t.Fatal(err)
	}
	const serviceGID = 4242
	if err := os.Chown(cfg, 0, serviceGID); err != nil {
		t.Fatal(err)
	}
	if err := WriteLicenseSidecar(cfg, "header.payload.sig"); err != nil {
		t.Fatal(err)
	}
	fi, err := os.Stat(LicenseSidecarPath(cfg))
	if err != nil {
		t.Fatal(err)
	}
	st := fi.Sys().(*syscall.Stat_t)
	if fi.Mode().Perm() != 0o640 || st.Uid != 0 || st.Gid != serviceGID {
		t.Fatalf("sidecar %o %d:%d, want 0640 0:%d like agent.yaml", fi.Mode().Perm(), st.Uid, st.Gid, serviceGID)
	}
}
