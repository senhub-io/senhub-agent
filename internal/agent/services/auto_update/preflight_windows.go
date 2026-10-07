//go:build windows

package auto_update

import (
	"os"
	"path/filepath"
)

// pathWritable reports whether path is writable. The Windows service runs
// as LocalSystem so this is a best-effort guard against a read-only
// install location rather than a non-root-service check. For a file it
// probes the directory: Windows refuses to open a running executable for
// writing, yet the self-update renames it and writes the new binary beside
// it (selfupdate.CommitBinary), which only needs the directory to be
// writable. Opening the file made config check warn on every ZIP install
// whose service was running.
func pathWritable(path string) bool {
	fi, err := os.Stat(path)
	if err != nil {
		return false
	}
	if fi.IsDir() {
		f, err := os.CreateTemp(path, ".senhub-write-probe-*")
		if err != nil {
			return false
		}
		name := f.Name()
		_ = f.Close()
		_ = os.Remove(name)
		return true
	}
	return pathWritable(filepath.Dir(path))
}
