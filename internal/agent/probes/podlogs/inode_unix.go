//go:build unix

package podlogs

import (
	"io/fs"
	"syscall"
)

func inodeFromInfo(fi fs.FileInfo) uint64 {
	if st, ok := fi.Sys().(*syscall.Stat_t); ok {
		return uint64(st.Ino)
	}
	return 0
}
