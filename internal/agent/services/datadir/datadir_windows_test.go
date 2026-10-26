//go:build windows

package datadir

import (
	"os"
	"path/filepath"
	"testing"
)

func TestApplyRemovesUsersACE(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "SenHub")
	if err := os.Mkdir(dir, 0o750); err != nil {
		t.Fatal(err)
	}
	b := winBackend{}

	before, err := b.inspect(dir)
	if err != nil {
		t.Fatal(err)
	}
	if before.Protected {
		t.Fatal("a fresh temp directory should inherit its ACL")
	}

	if err := b.apply(dir); err != nil {
		t.Fatal(err)
	}
	after, err := b.inspect(dir)
	if err != nil {
		t.Fatal(err)
	}
	if !after.Protected {
		t.Error("DACL is not protected after apply")
	}
	got := map[string]bool{}
	for _, ace := range after.Allowed {
		got[ace.SID] = true
	}
	if !got["S-1-5-18"] || !got["S-1-5-32-544"] || len(got) != 2 {
		t.Errorf("expected exactly SYSTEM and Administrators, got %v", got)
	}
	if repair, reason := NeedsRepair(after); repair {
		t.Errorf("applied DACL still needs repair: %s", reason)
	}

	if err := os.WriteFile(filepath.Join(dir, "child.txt"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	child, err := b.inspect(filepath.Join(dir, "child.txt"))
	if err != nil {
		t.Fatal(err)
	}
	for _, ace := range child.Allowed {
		if ace.SID == sidBuiltinUsers {
			t.Error("new child file inherited a Users ACE")
		}
	}
}
