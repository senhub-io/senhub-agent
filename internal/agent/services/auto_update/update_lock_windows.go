//go:build windows

package auto_update

import (
	"errors"
	"os"
	"sync"

	"golang.org/x/sys/windows"
)

func tryLockFile(path string) (func(), error) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, err
	}
	var ov windows.Overlapped
	err = windows.LockFileEx(
		windows.Handle(f.Fd()),
		windows.LOCKFILE_EXCLUSIVE_LOCK|windows.LOCKFILE_FAIL_IMMEDIATELY,
		0, 1, 0, &ov,
	)
	if err != nil {
		f.Close() //nolint:errcheck // the lock was not taken; nothing to flush
		if errors.Is(err, windows.ERROR_LOCK_VIOLATION) {
			return nil, ErrUpdateInProgress
		}
		return nil, err
	}
	var once sync.Once
	return func() {
		once.Do(func() {
			var ov windows.Overlapped
			windows.UnlockFileEx(windows.Handle(f.Fd()), 0, 1, 0, &ov) //nolint:errcheck // closing the handle releases it anyway
			f.Close()                                                  //nolint:errcheck // empty lock file
		})
	}, nil
}
