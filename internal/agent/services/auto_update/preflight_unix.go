//go:build !windows

package auto_update

import "golang.org/x/sys/unix"

// pathWritable reports whether the current process can write to path,
// honouring the real uid/gid and file mode via the kernel access(2)
// check.
func pathWritable(path string) bool {
	return unix.Access(path, unix.W_OK) == nil
}
