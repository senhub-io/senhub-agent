package auto_update

import (
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const (
	testFailedStartBinary   = "/opt/senhub-agent/senhub-agent"
	testFailedStartStateDir = "/state"
)

func failedPathFor(binary string) string {
	return filepath.Join(filepath.Dir(binary), "."+filepath.Base(binary)+".failed")
}

func TestRestoreAfterFailedStart_RestoresPreviousBinary(t *testing.T) {
	fs := newFakeFS()
	fs.files[testFailedStartBinary] = []byte("new")
	fs.files[testFailedStartBinary+".previous"] = []byte("old")
	putJSON(t, fs, filepath.Join(testFailedStartStateDir, pendingFileName), pendingUpdate{FromVersion: "0.7.1", ToVersion: "0.7.2"})
	env := newTestRollbackEnv(t, fs, testFailedStartBinary, testFailedStartStateDir, "0.7.1", true, func() time.Time { return time.Unix(1700000000, 0) })

	if err := env.restoreAfterFailedStart("0.7.2"); err != nil {
		t.Fatalf("restoreAfterFailedStart: %v", err)
	}
	if got := string(fs.files[testFailedStartBinary]); got != "old" {
		t.Errorf("binary = %q, want the previous one", got)
	}
	if got := string(fs.files[failedPathFor(testFailedStartBinary)]); got != "new" {
		t.Errorf("failed path = %q, want the new binary", got)
	}
	if _, ok := fs.files[testFailedStartBinary+".previous"]; ok {
		t.Error("the .previous file must be gone after the restore")
	}
	rec, err := readRolledBack(fs, testFailedStartStateDir)
	if err != nil {
		t.Fatalf("readRolledBack: %v", err)
	}
	if rec.Outcome != OutcomeRestored || rec.FailedVersion != "0.7.2" || rec.RestoredVersion != "0.7.1" {
		t.Errorf("unexpected record: %+v", rec)
	}
	if _, ok := fs.files[filepath.Join(testFailedStartStateDir, pendingFileName)]; ok {
		t.Error("the update-pending marker must be removed")
	}
}

func TestRestoreAfterFailedStart_NoPreviousBinary(t *testing.T) {
	fs := newFakeFS()
	fs.files[testFailedStartBinary] = []byte("new")
	env := newTestRollbackEnv(t, fs, testFailedStartBinary, testFailedStartStateDir, "0.7.1", true, time.Now)

	err := env.restoreAfterFailedStart("0.7.2")
	if err == nil || !strings.Contains(err.Error(), "no previous binary") {
		t.Fatalf("want a no-previous error, got %v", err)
	}
	if got := string(fs.files[testFailedStartBinary]); got != "new" {
		t.Errorf("binary must be untouched, got %q", got)
	}
	if rec, rerr := readRolledBack(fs, testFailedStartStateDir); rerr != nil || rec != nil {
		t.Errorf("no rollback record must be written, got %+v, %v", rec, rerr)
	}
}

func TestRestoreAfterFailedStart_SecondRenameFails(t *testing.T) {
	fs := newFakeFS()
	fs.files[testFailedStartBinary] = []byte("new")
	fs.files[testFailedStartBinary+".previous"] = []byte("old")
	fs.renameErrs[testFailedStartBinary] = errors.New("busy")
	env := newTestRollbackEnv(t, fs, testFailedStartBinary, testFailedStartStateDir, "0.7.1", true, time.Now)

	if err := env.restoreAfterFailedStart("0.7.2"); err == nil {
		t.Fatal("a failed restore must return an error")
	}
	if got := string(fs.files[testFailedStartBinary]); got != "new" {
		t.Errorf("the failed binary must be put back at the original path, got %q", got)
	}
}
