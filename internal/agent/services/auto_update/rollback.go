package auto_update

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"senhub-agent.go/internal/agent/services/logger"
)

type rollbackFS interface {
	ReadFile(name string) ([]byte, error)
	WriteFileAtomic(name string, data []byte) error
	Remove(name string) error
	Rename(oldpath, newpath string) error
	Exists(name string) bool
}

type osRollbackFS struct{}

func (osRollbackFS) ReadFile(name string) ([]byte, error) {
	return os.ReadFile(name)
}

func (osRollbackFS) WriteFileAtomic(name string, data []byte) error {
	dir := filepath.Dir(name)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create directory: %w", err)
	}

	tmp, err := os.CreateTemp(dir, "."+filepath.Base(name)+".tmp-*")
	if err != nil {
		return fmt.Errorf("create temp file: %w", err)
	}
	tmpName := tmp.Name()

	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		os.Remove(tmpName)
		return fmt.Errorf("write temp file: %w", err)
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpName)
		return fmt.Errorf("close temp file: %w", err)
	}
	// The CLI that writes the marker runs as root and the daemon that reads it
	// does not; CreateTemp's 0600 would hide it from the daemon.
	if err := os.Chmod(tmpName, 0o644); err != nil {
		os.Remove(tmpName)
		return fmt.Errorf("set mode on temp file: %w", err)
	}
	if err := os.Rename(tmpName, name); err != nil {
		os.Remove(tmpName)
		return fmt.Errorf("rename temp file: %w", err)
	}
	return nil
}

func (osRollbackFS) Remove(name string) error {
	return os.Remove(name)
}

func (osRollbackFS) Rename(oldpath, newpath string) error {
	return os.Rename(oldpath, newpath)
}

func (osRollbackFS) Exists(name string) bool {
	_, err := os.Stat(name)
	return err == nil
}

const (
	pendingFileName    = "update-pending.json"
	rolledBackFileName = "update-rolled-back.json"
	maxBootAttempts    = 3
	rollbackWindow     = 10 * time.Minute
	confirmAfter       = 120 * time.Second
)

type pendingUpdate struct {
	FromVersion  string    `json:"from_version"`
	ToVersion    string    `json:"to_version"`
	SwappedAt    time.Time `json:"swapped_at"`
	BootAttempts int       `json:"boot_attempts"`
}

type RolledBackRecord struct {
	FailedVersion   string    `json:"failed_version"`
	RestoredVersion string    `json:"restored_version"`
	RolledBackAt    time.Time `json:"rolled_back_at"`
	BootAttempts    int       `json:"boot_attempts"`
	Outcome         string    `json:"outcome"`
}

const (
	OutcomeRestored     = "restored"
	OutcomeNotPerformed = "not_performed"
)

type rollbackEnv struct {
	fs       rollbackFS
	stateDir string
	binary   string
	version  string
	canSwap  bool
	now      func() time.Time
	log      *logger.ModuleLogger
}

type bootAction int

const (
	bootNone bootAction = iota
	bootTracking
	bootRolledBack
	bootRollbackNotPerformed
)

func (e rollbackEnv) previousPath() string {
	return e.binary + ".previous"
}

func (e rollbackEnv) failedPath() string {
	return filepath.Join(filepath.Dir(e.binary), "."+filepath.Base(e.binary)+".failed")
}

func (e rollbackEnv) recordSwap(from, to string) error {
	return e.writePendingMarker(pendingUpdate{
		FromVersion:  canonicalVersion(from),
		ToVersion:    canonicalVersion(to),
		SwappedAt:    e.now(),
		BootAttempts: 0,
	})
}

func (e rollbackEnv) boot() bootAction {
	path := filepath.Join(e.stateDir, pendingFileName)
	data, err := e.fs.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			e.clearOperatorRestored()
			return bootNone
		}
		e.log.Warn().Err(err).Str("path", path).Msg("cannot read pending update marker")
		e.removePath(path)
		return bootNone
	}

	var marker pendingUpdate
	if err := json.Unmarshal(data, &marker); err != nil || marker.ToVersion == "" {
		if err != nil {
			e.log.Warn().Err(err).Str("path", path).Msg("corrupt pending update marker")
		} else {
			e.log.Warn().Str("path", path).Msg("pending update marker has no to_version")
		}
		e.removePath(path)
		return bootNone
	}

	if canonicalVersion(marker.ToVersion) != canonicalVersion(e.version) {
		e.log.Info().Str("path", path).Str("to_version", marker.ToVersion).Str("running_version", e.version).Msg("removing stale pending update marker")
		if rmErr := e.fs.Remove(path); rmErr != nil && !errors.Is(rmErr, os.ErrNotExist) {
			e.log.Warn().Err(rmErr).Str("path", path).Msg("failed to remove stale pending update marker")
		}
		return bootNone
	}

	now := e.now()
	marker.BootAttempts++
	if err := e.writePendingMarker(marker); err != nil {
		e.log.Warn().Err(err).Str("path", path).Msg("failed to persist boot attempt")
	}

	if marker.BootAttempts < maxBootAttempts {
		return bootTracking
	}

	age := time.Duration(0)
	if !now.Before(marker.SwappedAt) {
		age = now.Sub(marker.SwappedAt)
	}
	if age > rollbackWindow {
		e.log.Warn().Str("path", path).Dur("age", age).Int("boot_attempts", marker.BootAttempts).Msg("pending update marker exceeded rollback window; removing without rollback")
		e.removePath(path)
		return bootNone
	}

	e.log.Error().
		Str("from_version", marker.FromVersion).
		Str("to_version", marker.ToVersion).
		Int("boot_attempts", marker.BootAttempts).
		Msg("update never confirmed; rolling back")

	record := RolledBackRecord{
		FailedVersion:   marker.ToVersion,
		RestoredVersion: marker.FromVersion,
		RolledBackAt:    now,
		BootAttempts:    marker.BootAttempts,
		Outcome:         OutcomeNotPerformed,
	}

	if !e.canSwap {
		e.log.Error().
			Str("from_version", marker.FromVersion).
			Str("to_version", marker.ToVersion).
			Str("manual_restore", "sudo senhub-agent update "+marker.FromVersion).
			Msg("rollback not performed because daemon cannot swap its own binary")
		e.finishRollback(record, path)
		return bootRollbackNotPerformed
	}

	if !e.fs.Exists(e.previousPath()) {
		e.log.Error().
			Str("from_version", marker.FromVersion).
			Str("to_version", marker.ToVersion).
			Str("previous_path", e.previousPath()).
			Msg("rollback not performed because previous binary is missing")
		e.finishRollback(record, path)
		return bootRollbackNotPerformed
	}

	failed := e.failedPath()
	if e.fs.Exists(failed) {
		if err := e.fs.Remove(failed); err != nil {
			e.log.Warn().Err(err).Str("path", failed).Msg("failed to remove old failed binary; rollback cannot proceed")
			e.finishRollback(record, path)
			return bootRollbackNotPerformed
		}
	}

	if err := e.fs.Rename(e.binary, failed); err != nil {
		e.log.Error().Err(err).Str("path", failed).Msg("rollback not performed because binary could not be set aside")
		e.finishRollback(record, path)
		return bootRollbackNotPerformed
	}

	if err := e.fs.Rename(e.previousPath(), e.binary); err != nil {
		e.log.Error().Err(err).Str("path", e.binary).Msg("failed to rename previous binary into place; restoring original")
		if undoErr := e.fs.Rename(failed, e.binary); undoErr != nil {
			e.log.Error().Err(undoErr).Str("path", e.binary).Msg("failed to restore original binary after failed rollback")
		}
		e.finishRollback(record, path)
		return bootRollbackNotPerformed
	}

	record.Outcome = OutcomeRestored
	if err := e.writeRolledBackRecord(record); err != nil {
		e.log.Warn().Err(err).Str("path", filepath.Join(e.stateDir, rolledBackFileName)).Msg("failed to write rolled-back record")
	}
	e.removePath(path)
	return bootRolledBack
}

// clearOperatorRestored drops a not-performed record once the running version
// is no longer the one that failed: the operator has done the rollback the
// record asked for. A restored record is kept, it is what holds the failed
// version back from the periodic check.
func (e rollbackEnv) clearOperatorRestored() {
	record, err := readRolledBack(e.fs, e.stateDir)
	if err != nil || record == nil || record.Outcome != OutcomeNotPerformed || record.FailedVersion == e.version {
		return
	}
	e.removePath(filepath.Join(e.stateDir, rolledBackFileName))
}

func (e rollbackEnv) confirm() error {
	path := filepath.Join(e.stateDir, pendingFileName)
	data, err := e.fs.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return fmt.Errorf("read pending update marker: %w", err)
	}

	var marker pendingUpdate
	if err := json.Unmarshal(data, &marker); err != nil || marker.ToVersion == "" {
		if err != nil {
			e.log.Warn().Err(err).Str("path", path).Msg("ignoring unreadable pending update marker during confirm")
		} else {
			e.log.Warn().Str("path", path).Msg("ignoring pending update marker with no to_version during confirm")
		}
		return nil
	}
	if canonicalVersion(marker.ToVersion) != canonicalVersion(e.version) {
		return nil
	}

	if err := e.fs.Remove(path); err != nil {
		return fmt.Errorf("remove pending update marker: %w", err)
	}

	rolledBackPath := filepath.Join(e.stateDir, rolledBackFileName)
	if e.fs.Exists(rolledBackPath) {
		if err := e.fs.Remove(rolledBackPath); err != nil {
			return fmt.Errorf("remove rolled-back record: %w", err)
		}
	}
	return nil
}

func (e rollbackEnv) skipsVersion(candidate string) (bool, *RolledBackRecord) {
	record, err := readRolledBack(e.fs, e.stateDir)
	if err != nil {
		e.log.Warn().Err(err).Str("path", filepath.Join(e.stateDir, rolledBackFileName)).Msg("cannot read rolled-back record")
		return false, nil
	}
	if record == nil {
		return false, nil
	}

	newer, err := shouldUpdateTo(record.FailedVersion, candidate)
	if err != nil {
		e.log.Warn().Err(err).Str("failed_version", record.FailedVersion).Str("candidate", candidate).Msg("cannot compare rolled-back version; failing closed")
		return true, record
	}
	if !newer {
		return true, record
	}
	return false, record
}

func ReadRolledBack(stateDir string) (*RolledBackRecord, error) {
	return readRolledBack(osRollbackFS{}, stateDir)
}

func readRolledBack(fs rollbackFS, stateDir string) (*RolledBackRecord, error) {
	path := filepath.Join(stateDir, rolledBackFileName)
	data, err := fs.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, fmt.Errorf("read rolled-back record: %w", err)
	}

	var record RolledBackRecord
	if err := json.Unmarshal(data, &record); err != nil {
		return nil, fmt.Errorf("parse rolled-back record: %w", err)
	}
	return &record, nil
}

func (e rollbackEnv) writePendingMarker(marker pendingUpdate) error {
	data, err := json.Marshal(marker)
	if err != nil {
		return fmt.Errorf("marshal pending update marker: %w", err)
	}
	if err := e.fs.WriteFileAtomic(filepath.Join(e.stateDir, pendingFileName), data); err != nil {
		return fmt.Errorf("write pending update marker: %w", err)
	}
	return nil
}

func (e rollbackEnv) writeRolledBackRecord(record RolledBackRecord) error {
	data, err := json.Marshal(record)
	if err != nil {
		return fmt.Errorf("marshal rolled-back record: %w", err)
	}
	if err := e.fs.WriteFileAtomic(filepath.Join(e.stateDir, rolledBackFileName), data); err != nil {
		return fmt.Errorf("write rolled-back record: %w", err)
	}
	return nil
}

func (e rollbackEnv) finishRollback(record RolledBackRecord, pendingPath string) {
	if err := e.writeRolledBackRecord(record); err != nil {
		e.log.Warn().Err(err).Str("path", filepath.Join(e.stateDir, rolledBackFileName)).Msg("failed to write rolled-back record")
	}
	e.removePath(pendingPath)
}

func (e rollbackEnv) removePath(path string) {
	if err := e.fs.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		e.log.Warn().Err(err).Str("path", path).Msg("failed to remove file")
	}
}

// canonicalVersion drops the "v" an operator may type ("update v0.6.3"):
// tags and the running version carry none, and a marker that differs from
// the running version only by it would be discarded as stale, leaving that
// update without a rollback.
func canonicalVersion(v string) string {
	return strings.TrimPrefix(strings.TrimSpace(v), "v")
}
