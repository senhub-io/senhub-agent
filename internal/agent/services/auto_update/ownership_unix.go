//go:build !windows

package auto_update

import (
	"fmt"
	"os"
	"syscall"
)

// statOwnership reads the owner uid and mode of path.
func statOwnership(path string) (ownership, error) {
	info, err := os.Stat(path)
	if err != nil {
		return ownership{}, err
	}
	sys, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return ownership{}, fmt.Errorf("no unix ownership information for %s", path)
	}
	return ownership{uid: int(sys.Uid), mode: info.Mode()}, nil
}
