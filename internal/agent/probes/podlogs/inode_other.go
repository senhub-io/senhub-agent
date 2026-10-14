//go:build !unix

package podlogs

import "io/fs"

func inodeFromInfo(fi fs.FileInfo) uint64 {
	return 0
}
