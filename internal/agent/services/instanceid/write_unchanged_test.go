package instanceid

import (
	"os"
	"path/filepath"
	"testing"
)

func TestWrite_SameIDAndModeTouchesNothing(t *testing.T) {
	if os.PathSeparator == '\\' {
		t.Skip("file identity is compared by inode")
	}
	dir := t.TempDir()
	key := "6f9619ff-8b86-4d11-b42d-00c04fc964ff"
	id := "0a1b2c3d-1111-4222-8333-444455556666"
	if err := Write(dir, id, key); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(dir, FileName)
	before, err := os.Stat(target)
	if err != nil {
		t.Fatal(err)
	}

	if err := Write(dir, id, key); err != nil {
		t.Fatal(err)
	}
	after, err := os.Stat(target)
	if err != nil {
		t.Fatal(err)
	}
	if !os.SameFile(before, after) {
		t.Error("an unchanged instance id was rewritten through a new file")
	}
	entries, _ := os.ReadDir(dir)
	if len(entries) != 1 {
		t.Errorf("directory holds %d entries, want only %s", len(entries), FileName)
	}

	other := "0a1b2c3d-1111-4222-8333-777788889999"
	if err := Write(dir, other, key); err != nil {
		t.Fatal(err)
	}
	if got, err := Read(dir); err != nil || got != other {
		t.Errorf("Read after a changed id = %q, %v; want %q", got, err, other)
	}
}
