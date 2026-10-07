//go:build windows

package auto_update

import (
	"os"
	"path/filepath"
	"testing"
)

// The running service holds its executable open, so opening it for
// writing fails even though the self-update (rename, then write beside it)
// works. pathWritable must answer for the directory, not the open file.
func TestPathWritableProbesTheDirectoryOfAFile(t *testing.T) {
	dir := t.TempDir()
	exe := filepath.Join(dir, "senhub-agent.exe")
	if err := os.WriteFile(exe, []byte("x"), 0o755); err != nil {
		t.Fatal(err)
	}
	held, err := os.Open(exe)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = held.Close() }()
	if !pathWritable(exe) {
		t.Fatal("a file in a writable directory is reported unwritable")
	}
	if pathWritable(filepath.Join(dir, "missing", "senhub-agent.exe")) {
		t.Fatal("a file in a missing directory is reported writable")
	}
}
