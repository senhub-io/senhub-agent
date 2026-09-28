//go:build linux

package fsown

import (
	"os"
	"path/filepath"
	"syscall"
	"testing"
)

func ownerOf(t *testing.T, p string) (uint32, uint32) {
	t.Helper()
	fi, err := os.Stat(p)
	if err != nil {
		t.Fatal(err)
	}
	st := fi.Sys().(*syscall.Stat_t)
	return st.Uid, st.Gid
}

// Run as root (the case this exists for): a file written by root in a
// directory the service account owns ends up owned by that account.
func TestAlignToDirHandsTheFileToTheDirectoryOwner(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("needs root: the ownership change is what is under test")
	}
	dir := t.TempDir()
	if err := os.Chown(dir, 4242, 4343); err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(dir, "00-http.yaml")
	if err := os.WriteFile(p, []byte("http: {}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := AlignToDir(p); err != nil {
		t.Fatalf("AlignToDir: %v", err)
	}
	if uid, gid := ownerOf(t, p); uid != 4242 || gid != 4343 {
		t.Errorf("owner = %d:%d, want 4242:4343", uid, gid)
	}
}

func TestAlignToDirLeavesANonRootWriterAlone(t *testing.T) {
	orig := geteuid
	geteuid = func() int { return 1000 }
	t.Cleanup(func() { geteuid = orig })
	if err := AlignToDir(filepath.Join(t.TempDir(), "absent")); err != nil {
		t.Errorf("a non-root writer must be a no-op, got %v", err)
	}
}
