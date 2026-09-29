//go:build linux

// Package fsown keeps the files a privileged command writes readable by the
// service that runs without privilege.
package fsown

import (
	"fmt"
	"os"
	"path/filepath"
	"syscall"
)

// geteuid is swapped by tests.
var geteuid = os.Geteuid

// AlignToDir gives path the owner of its directory when the caller runs as
// root. The service runs as its own account and owns the configuration
// directory; a command run with sudo (`config set`, `secret set`) writes
// through a temporary file renamed into place, so without this the new file
// belongs to root and the service can no longer read it: the reload fails,
// and so does the next start. A no-op for any other caller, which already
// creates files as itself.
func AlignToDir(path string) error {
	if geteuid() != 0 {
		return nil
	}
	di, err := os.Stat(filepath.Dir(path))
	if err != nil {
		return fmt.Errorf("reading the owner of %s: %w", filepath.Dir(path), err)
	}
	st, ok := di.Sys().(*syscall.Stat_t)
	if !ok {
		return nil
	}
	if err := os.Lchown(path, int(st.Uid), int(st.Gid)); err != nil {
		return fmt.Errorf("giving %s the owner of its directory: %w", path, err)
	}
	return nil
}
