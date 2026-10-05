package configuration

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync/atomic"

	"senhub-agent.go/internal/agent/services/fsown"
)

// atomicWriteFile writes data to path durably: it writes a uniquely named temp
// file in the same directory, fsyncs it, renames it over path, then fsyncs the
// directory so the rename survives a crash. A crash/power-loss mid-write can
// therefore never leave a truncated or empty config file — the old content
// stays until the atomic rename lands the new one. Every boot-time config
// rewrite (seal, split, stamp, license/install edits) goes through this rather
// than os.WriteFile so an OOM-kill or power loss cannot brick the agent's
// boot-critical YAML.
func atomicWriteFile(path string, data []byte, perm os.FileMode) error {
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, "."+filepath.Base(path)+".tmp-*")
	if err != nil {
		return fmt.Errorf("creating temp file for %s: %w", path, err)
	}
	tmpName := tmp.Name()
	removeTmp := true
	defer func() {
		if removeTmp {
			_ = os.Remove(tmpName)
		}
	}()

	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("writing temp file for %s: %w", path, err)
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("syncing temp file for %s: %w", path, err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("closing temp file for %s: %w", path, err)
	}
	if err := os.Chmod(tmpName, perm); err != nil {
		return fmt.Errorf("setting mode on temp file for %s: %w", path, err)
	}
	if err := os.Rename(tmpName, path); err != nil {
		return fmt.Errorf("renaming temp file to %s: %w", path, err)
	}
	removeTmp = false
	if err := fsown.AlignToDir(path); err != nil {
		return err
	}

	// fsync the directory so the rename entry is durable; best-effort — a
	// filesystem that rejects a directory Sync must not fail the write.
	if d, derr := os.Open(dir); derr == nil {
		_ = d.Sync()
		_ = d.Close()
	}
	return nil
}

// maxRewriteAttempts bounds how many times rewriteFile redoes an edit because
// the file changed under it.
const maxRewriteAttempts = 5

// ErrConfigChangedConcurrently is returned by rewriteFile when the file kept
// changing between the read and the write for every attempt. Nothing was
// written: the caller may retry later (the seal does, at the next start).
var ErrConfigChangedConcurrently = errors.New("configuration file kept changing while it was being rewritten")

// beforeCommitHook is a test seam: it runs after an edit was computed and
// before the file is re-read to detect a concurrent write.
var beforeCommitHook atomic.Pointer[func(path string, attempt int)]

// rewriteFile is the read-modify-write primitive for operator-owned config
// files. It reads path, hands the bytes to edit, and just before the atomic
// rename re-reads the file: if the bytes changed since the first read (an
// operator saved the file meanwhile) the edit is redone from the fresh bytes,
// so that change is never overwritten. After maxRewriteAttempts it gives up
// without writing and returns ErrConfigChangedConcurrently.
//
// edit returns the new content, or nil to leave the file untouched. It may run
// several times and must derive everything from the bytes it is given. A
// narrow window remains between the re-read and the rename; it cannot be closed
// without a lock the operator's editor would not honour.
func rewriteFile(path string, edit func(data []byte) ([]byte, error)) (bool, error) {
	for attempt := 1; attempt <= maxRewriteAttempts; attempt++ {
		data, err := os.ReadFile(path) // #nosec G304 - the agent's own configuration
		if err != nil {
			return false, err
		}
		out, err := edit(data)
		if err != nil || out == nil {
			return false, err
		}
		if h := beforeCommitHook.Load(); h != nil {
			(*h)(path, attempt)
		}
		fresh, err := os.ReadFile(path) // #nosec G304 - the agent's own configuration
		if err != nil {
			return false, err
		}
		if !bytes.Equal(fresh, data) {
			continue
		}
		if err := atomicWriteFile(path, out, fileModeOr(path, 0o600)); err != nil {
			return false, err
		}
		return true, nil
	}
	return false, fmt.Errorf("%s: %w", path, ErrConfigChangedConcurrently)
}

// fileModeOr returns the current mode of path, or fallback when path does not
// exist or cannot be stat'd. Used to preserve an operator's file mode across a
// rewrite (e.g. a 0640 root:senhub fragment stays group-readable to the service
// user) instead of clamping every rewrite to 0600.
func fileModeOr(path string, fallback os.FileMode) os.FileMode {
	if fi, err := os.Stat(path); err == nil {
		return fi.Mode().Perm()
	}
	return fallback
}
