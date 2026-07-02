package auto_update

import (
	"fmt"
	"os"
	"path/filepath"
)

// secureStageDir creates a fresh, unpredictably-named directory under baseDir to
// stage an update artifact. Staging under an application-owned base (not the
// multi-user temp dir) plus a random per-run name removes the two primitives a
// local attacker needs to substitute a verified installer with a malicious one
// before an elevated msiexec reads it back: a predictable path to pre-place, and
// a world-writable parent in which to race the write. baseDir is created 0o700
// when absent.
func secureStageDir(baseDir string) (string, error) {
	if err := os.MkdirAll(baseDir, 0o700); err != nil {
		return "", fmt.Errorf("creating staging base %s: %w", baseDir, err)
	}
	dir, err := os.MkdirTemp(baseDir, "update-*")
	if err != nil {
		return "", fmt.Errorf("creating staging dir under %s: %w", baseDir, err)
	}
	return dir, nil
}

// writeStagedFile writes data to name inside dir, refusing to follow or clobber
// a pre-existing entry (O_EXCL). Together with the random directory from
// secureStageDir this closes the verify-then-install TOCTOU window: nothing the
// caller did not just create can end up at the path handed to the installer.
func writeStagedFile(dir, name string, data []byte) (string, error) {
	path := filepath.Join(dir, name)
	f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return "", fmt.Errorf("creating staged file %s: %w", path, err)
	}
	if _, err := f.Write(data); err != nil {
		_ = f.Close()
		return "", fmt.Errorf("writing staged file %s: %w", path, err)
	}
	if err := f.Close(); err != nil {
		return "", fmt.Errorf("closing staged file %s: %w", path, err)
	}
	return path, nil
}
