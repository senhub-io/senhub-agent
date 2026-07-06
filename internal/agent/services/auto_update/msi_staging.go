package auto_update

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"senhub-agent.go/internal/agent/services/logger"
)

// stagedDirPrefix names per-run staging directories so a sweep can recognise
// its own artifacts (see sweepStagedUpdates).
const stagedDirPrefix = "update-"

// secureStageDir creates a fresh, unpredictably-named directory under baseDir to
// stage an update artifact. Staging under an application-owned base (not the
// multi-user temp dir) plus a random per-run name removes the two primitives a
// local attacker needs to substitute a verified installer with a malicious one
// before an elevated msiexec reads it back: a predictable path to pre-place, and
// a world-writable parent in which to race the write. The base is created (and,
// on Windows, ACL-locked and ownership-checked) by secureStageBase.
func secureStageDir(baseDir string) (string, error) {
	if err := secureStageBase(baseDir); err != nil {
		return "", fmt.Errorf("securing staging base %s: %w", baseDir, err)
	}
	dir, err := os.MkdirTemp(baseDir, stagedDirPrefix+"*")
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

// sweepStagedUpdates removes staging directories older than maxAge left behind by
// prior runs. A successful MSI launch keeps its directory (msiexec reads it back
// detached), but a persistently failing install would otherwise accumulate a
// fresh tens-of-MB MSI every cycle until the disk fills (M2). Best-effort: sweep
// failures are logged, never fatal to the update path.
func sweepStagedUpdates(baseDir string, maxAge time.Duration, log *logger.ModuleLogger) {
	entries, err := os.ReadDir(baseDir)
	if err != nil {
		// A missing base (first run) is not an error worth surfacing.
		if !os.IsNotExist(err) && log != nil {
			log.Warn().Err(err).Str("dir", baseDir).Msg("Failed to scan staging base for cleanup")
		}
		return
	}
	cutoff := time.Now().Add(-maxAge)
	for _, e := range entries {
		if !e.IsDir() || !strings.HasPrefix(e.Name(), stagedDirPrefix) {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		if info.ModTime().After(cutoff) {
			continue
		}
		stale := filepath.Join(baseDir, e.Name())
		if err := os.RemoveAll(stale); err != nil && log != nil {
			log.Warn().Err(err).Str("dir", stale).Msg("Failed to remove stale staging dir")
		}
	}
}
