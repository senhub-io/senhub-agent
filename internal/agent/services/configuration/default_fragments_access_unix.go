//go:build !windows

package configuration

import (
	"fmt"

	"golang.org/x/sys/unix"
)

// dockerSocketAccess reports whether the current process can open the socket
// both ways: a stat-only check passes while the agent runs as a user outside
// the socket's group, and the probe would then fail on every cycle.
func dockerSocketAccess(path string) error {
	if err := unix.Access(path, unix.R_OK|unix.W_OK); err != nil {
		return fmt.Errorf("access %s: %w", path, err)
	}
	return nil
}
