//go:build linux

package auto_update

import (
	"fmt"
	"os"
	"path/filepath"

	"golang.org/x/sys/unix"
)

// acquireRootLock takes an exclusive flock on the update lock of the root
// state directory. AcquireUpdateLock is a no-op off Windows, because the
// service never replaces its own binary there; the root unit does, and a
// second run started by hand must not swap the binary under the first.
func acquireRootLock(dir string) (func(), error) {
	path := filepath.Join(dir, updateLockFileName)
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0o600)
	if err != nil {
		return nil, fmt.Errorf("opening the update lock %s: %w", path, err)
	}
	if err := unix.Flock(int(f.Fd()), unix.LOCK_EX|unix.LOCK_NB); err != nil {
		_ = f.Close()
		if err == unix.EWOULDBLOCK {
			return nil, ErrUpdateInProgress
		}
		return nil, fmt.Errorf("locking %s: %w", path, err)
	}
	return func() { _ = f.Close() }, nil
}
