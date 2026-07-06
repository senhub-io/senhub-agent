package configuration

import (
	"fmt"
	"os"
	"path/filepath"
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

	// fsync the directory so the rename entry is durable; best-effort — a
	// filesystem that rejects a directory Sync must not fail the write.
	if d, derr := os.Open(dir); derr == nil {
		_ = d.Sync()
		_ = d.Close()
	}
	return nil
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
