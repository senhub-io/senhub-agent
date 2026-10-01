//go:build !windows

package app

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/user"
	"strconv"
	"syscall"

	"golang.org/x/sys/unix"
)

// hostLogDirAccess says whether the service user can write dir. It
// reads ownership and mode instead of creating a file, so it works for
// a caller who is not that user and leaves nothing behind.
func hostLogDirAccess(dir, serviceUser string) (logDirState, error) {
	info, err := os.Stat(dir)
	if errors.Is(err, fs.ErrNotExist) {
		return logDirState{}, nil
	}
	if err != nil {
		return logDirState{}, fmt.Errorf("stat %s: %w", dir, err)
	}
	if !info.IsDir() {
		return logDirState{Exists: true, Detail: "not a directory"}, nil
	}
	if serviceUser == "" {
		if err := unix.Access(dir, unix.W_OK); err != nil {
			return logDirState{Exists: true, Detail: "not writable by the current user"}, nil
		}
		return logDirState{Exists: true, Writable: true, Detail: "writable by the current user"}, nil
	}

	u, err := user.Lookup(serviceUser)
	if err != nil {
		return logDirState{}, fmt.Errorf("looking up %s: %w", serviceUser, err)
	}
	mode := info.Mode().Perm()
	st, ok := info.Sys().(*syscall.Stat_t)
	if ok && strconv.FormatUint(uint64(st.Uid), 10) == u.Uid && mode&0o200 != 0 {
		return logDirState{Exists: true, Writable: true, Detail: "owned by " + serviceUser}, nil
	}
	if ok && strconv.FormatUint(uint64(st.Gid), 10) == u.Gid && mode&0o020 != 0 {
		return logDirState{Exists: true, Writable: true, Detail: "group-writable by " + serviceUser}, nil
	}
	if mode&0o002 != 0 {
		return logDirState{Exists: true, Writable: true, Detail: "world-writable"}, nil
	}
	detail := fmt.Sprintf("mode %o", uint32(mode))
	if ok {
		detail = fmt.Sprintf("owned by uid %d, mode %o", st.Uid, uint32(mode))
	}
	return logDirState{Exists: true, Detail: detail}, nil
}
