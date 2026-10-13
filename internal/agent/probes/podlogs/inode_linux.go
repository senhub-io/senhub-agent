//go:build linux

// internal/agent/probes/podlogs/inode_linux.go
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
