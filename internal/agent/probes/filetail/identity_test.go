package filetail

import (
	"os"
	"path/filepath"
	"testing"
)

// The identity recorded for the file a tail opens is the identity of that
// file, not of whatever the path names at the first comparison: on Windows a
// stat taken by path resolves its identity lazily, so a rename and create
// before the first os.SameFile used to make the old stat name the new file.
func TestStatIdentitySurvivesARenameAndCreate(t *testing.T) {
	file := filepath.Join(t.TempDir(), "app.log")
	writeFile(t, file, "old\n")

	pinned, err := statIdentity(file)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(file, file+".1"); err != nil {
		t.Fatal(err)
	}
	writeFile(t, file, "new\n")

	cur, err := os.Stat(file)
	if err != nil {
		t.Fatal(err)
	}
	if os.SameFile(pinned, cur) {
		t.Fatal("the identity taken before the rotation names the new file")
	}
	moved, err := os.Stat(file + ".1")
	if err != nil {
		t.Fatal(err)
	}
	if !os.SameFile(pinned, moved) {
		t.Fatal("the identity taken before the rotation lost the renamed file")
	}
}
