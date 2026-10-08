//go:build !windows

package filetail

import "os"

func openShared(path string) (*os.File, error) {
	return os.Open(path)
}
