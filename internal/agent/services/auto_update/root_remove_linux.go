//go:build linux

package auto_update

import (
	"errors"
	"fmt"
	"os"

	"golang.org/x/sys/unix"
)

const (
	// maxRemoveDepth and maxRemovePasses bound the work on a tree the service
	// user shapes: it cannot make this run forever by nesting or by adding
	// entries while they are removed.
	maxRemoveDepth  = 32
	maxRemovePasses = 3
)

// ErrRequestNotRemovable says a request entry is still there after every
// attempt to remove it.
var ErrRequestNotRemovable = errors.New("request entry cannot be removed")

// removeEntry deletes name from the directory open at dirfd, whatever the
// service user made it: a file, a link (the link goes, never its target), a
// FIFO, a socket, a device, or a directory with anything inside. Everything is
// relative to a descriptor and nothing follows a link, so no path string under
// a service-owned directory is ever resolved.
func removeEntry(dirfd int, name string, depth int) error {
	err := unix.Unlinkat(dirfd, name, 0)
	if err == nil || errors.Is(err, unix.ENOENT) {
		return nil
	}
	unlinkErr := err
	if !errors.Is(err, unix.EISDIR) && !errors.Is(err, unix.EPERM) {
		return fmt.Errorf("removing %s: %w", name, err)
	}
	// A directory (Linux answers EISDIR, other systems EPERM).
	if depth >= maxRemoveDepth {
		return fmt.Errorf("removing %s: nested more than %d levels: %w", name, maxRemoveDepth, ErrRequestNotRemovable)
	}
	for pass := 0; pass < maxRemovePasses; pass++ {
		err = unix.Unlinkat(dirfd, name, unix.AT_REMOVEDIR)
		if err == nil || errors.Is(err, unix.ENOENT) {
			return nil
		}
		if errors.Is(err, unix.ENOTDIR) {
			// Not a directory after all: the first refusal stands (an
			// immutable or otherwise protected entry).
			return fmt.Errorf("removing %s: %w: %w", name, unlinkErr, ErrRequestNotRemovable)
		}
		if !errors.Is(err, unix.ENOTEMPTY) && !errors.Is(err, unix.EEXIST) {
			return fmt.Errorf("removing directory %s: %w", name, err)
		}
		if err := emptyDirectory(dirfd, name, depth); err != nil {
			return err
		}
	}
	return fmt.Errorf("removing directory %s: it keeps being refilled: %w", name, ErrRequestNotRemovable)
}

func emptyDirectory(parentfd int, name string, depth int) error {
	fd, err := unix.Openat(parentfd, name, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
	if err != nil {
		if errors.Is(err, unix.ENOENT) {
			return nil
		}
		return fmt.Errorf("opening directory %s: %w", name, err)
	}
	f := os.NewFile(uintptr(fd), name)
	defer func() { _ = f.Close() }()
	entries, err := f.Readdirnames(-1)
	if err != nil {
		return fmt.Errorf("listing directory %s: %w", name, err)
	}
	var firstErr error
	for _, entry := range entries {
		if err := removeEntry(fd, entry, depth+1); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

// removeRequests removes the request entries relative to the request
// directory, opened without following a link, and returns those that are
// still there.
func removeRequests(dir string, names []string) (left []string, err error) {
	dirfd, err := unix.Open(dir, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
	if err != nil {
		if errors.Is(err, unix.ENOENT) {
			return nil, nil
		}
		return names, fmt.Errorf("opening the request directory %s: %w", dir, err)
	}
	defer func() { _ = unix.Close(dirfd) }()
	var firstErr error
	for _, name := range names {
		if rerr := removeEntry(dirfd, name, 0); rerr != nil {
			left = append(left, name)
			if firstErr == nil {
				firstErr = rerr
			}
		}
	}
	return left, firstErr
}
