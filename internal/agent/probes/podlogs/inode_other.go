//go:build !linux

// internal/agent/probes/podlogs/inode_other.go
package podlogs

import "io/fs"

func inodeFromInfo(fi fs.FileInfo) uint64 {
	return 0
}
