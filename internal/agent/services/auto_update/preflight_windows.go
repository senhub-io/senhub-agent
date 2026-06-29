//go:build windows

package auto_update

import "os"

// pathWritable reports whether path is writable. The Windows service runs
// as LocalSystem so this is a best-effort guard against a read-only
// install location rather than a non-root-service check.
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
	f, err := os.OpenFile(path, os.O_WRONLY, 0)
	if err != nil {
		return false
	}
	_ = f.Close()
	return true
}
