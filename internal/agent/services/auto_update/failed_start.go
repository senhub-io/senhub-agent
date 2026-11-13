package auto_update

import (
	"fmt"
	"path/filepath"
	"time"

	"senhub-agent.go/internal/agent/services/logger"
)

// RestoreAfterFailedStart puts the previous binary back in place when the
// service never came up on the new one: the CLI must never leave the host
// stopped with only a .previous file on disk.
func RestoreAfterFailedStart(stateDir, binary, failedVersion, restoredVersion string, log *logger.ModuleLogger) error {
	env := rollbackEnv{
		fs:       osRollbackFS{},
		stateDir: stateDir,
		binary:   binary,
		version:  restoredVersion,
		canSwap:  true,
		now:      time.Now,
		log:      log,
	}
	return env.restoreAfterFailedStart(failedVersion)
}

func (e rollbackEnv) restoreAfterFailedStart(failedVersion string) error {
	if !e.fs.Exists(e.previousPath()) {
		return fmt.Errorf("no previous binary at %s to restore", e.previousPath())
	}
	if e.fs.Exists(e.failedPath()) {
		if err := e.fs.Remove(e.failedPath()); err != nil {
			return fmt.Errorf("removing the stale failed binary %s: %w", e.failedPath(), err)
		}
	}
	if err := e.fs.Rename(e.binary, e.failedPath()); err != nil {
		return fmt.Errorf("moving the failed binary aside: %w", err)
	}
	if err := e.fs.Rename(e.previousPath(), e.binary); err != nil {
		if rbErr := e.fs.Rename(e.failedPath(), e.binary); rbErr != nil {
			return fmt.Errorf("restoring the previous binary: %w (putting the failed binary back also failed: %v)", err, rbErr)
		}
		return fmt.Errorf("restoring the previous binary: %w", err)
	}
	if err := e.writeRolledBackRecord(RolledBackRecord{
		FailedVersion:   canonicalVersion(failedVersion),
		RestoredVersion: canonicalVersion(e.version),
		RolledBackAt:    e.now(),
		BootAttempts:    0,
		Outcome:         OutcomeRestored,
	}); err != nil {
		e.log.Warn().Err(err).Msg("Could not write the rollback record after a failed start")
	}
	e.removePath(filepath.Join(e.stateDir, pendingFileName))
	return nil
}
