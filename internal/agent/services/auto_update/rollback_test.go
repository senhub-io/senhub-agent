package auto_update

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

type fakeFS struct {
	files      map[string][]byte
	renameErrs map[string]error
}

func newFakeFS() *fakeFS {
	return &fakeFS{
		files:      map[string][]byte{},
		renameErrs: map[string]error{},
	}
}

func (f *fakeFS) ReadFile(name string) ([]byte, error) {
	data, ok := f.files[name]
	if !ok {
		return nil, os.ErrNotExist
	}
	return data, nil
}

func (f *fakeFS) WriteFileAtomic(name string, data []byte) error {
	f.files[name] = append([]byte(nil), data...)
	return nil
}

func (f *fakeFS) Remove(name string) error {
	if _, ok := f.files[name]; !ok {
		return os.ErrNotExist
	}
	delete(f.files, name)
	return nil
}

func (f *fakeFS) Rename(oldpath, newpath string) error {
	if err, ok := f.renameErrs[newpath]; ok {
		delete(f.renameErrs, newpath)
		return err
	}
	data, ok := f.files[oldpath]
	if !ok {
		return os.ErrNotExist
	}
	delete(f.files, oldpath)
	f.files[newpath] = data
	return nil
}

func (f *fakeFS) Exists(name string) bool {
	_, ok := f.files[name]
	return ok
}

func newTestRollbackEnv(t *testing.T, fs rollbackFS, binary, stateDir, version string, canSwap bool, now func() time.Time) rollbackEnv {
	t.Helper()
	return rollbackEnv{
		fs:       fs,
		stateDir: stateDir,
		binary:   binary,
		version:  version,
		canSwap:  canSwap,
		now:      now,
		log:      createTestModuleLogger(),
	}
}

func putJSON(t *testing.T, fs *fakeFS, name string, v any) {
	t.Helper()
	data, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	fs.files[name] = data
}

func putPending(t *testing.T, fs *fakeFS, stateDir string, p pendingUpdate) {
	t.Helper()
	putJSON(t, fs, filepath.Join(stateDir, pendingFileName), p)
}

func readFakeString(t *testing.T, fs *fakeFS, name string) string {
	t.Helper()
	data, err := fs.ReadFile(name)
	if err != nil {
		t.Fatalf("read %s: %v", name, err)
	}
	return string(data)
}

func readPending(t *testing.T, fs *fakeFS, stateDir string) pendingUpdate {
	t.Helper()
	data, err := fs.ReadFile(filepath.Join(stateDir, pendingFileName))
	if err != nil {
		t.Fatalf("read pending marker: %v", err)
	}
	var p pendingUpdate
	if err := json.Unmarshal(data, &p); err != nil {
		t.Fatalf("decode pending marker: %v", err)
	}
	return p
}

func readRolledBackRecord(t *testing.T, fs *fakeFS, stateDir string) RolledBackRecord {
	t.Helper()
	data, err := fs.ReadFile(filepath.Join(stateDir, rolledBackFileName))
	if err != nil {
		t.Fatalf("read rolled-back record: %v", err)
	}
	var r RolledBackRecord
	if err := json.Unmarshal(data, &r); err != nil {
		t.Fatalf("decode rolled-back record: %v", err)
	}
	return r
}

func addBinaries(fs *fakeFS, binary, binContent, prevContent string) {
	fs.files[binary] = []byte(binContent)
	fs.files[binary+".previous"] = []byte(prevContent)
}

func TestRecordSwapWritesExpectedJSON(t *testing.T) {
	fs := newFakeFS()
	now := time.Date(2024, 1, 2, 3, 4, 5, 0, time.UTC)
	binary := filepath.Join(t.TempDir(), "senhub-agent")
	stateDir := filepath.Join(t.TempDir(), "state")
	env := newTestRollbackEnv(t, fs, binary, stateDir, "2.0.0", true, func() time.Time { return now })

	if err := env.recordSwap("1.0.0", "2.0.0"); err != nil {
		t.Fatal(err)
	}

	data, err := fs.ReadFile(filepath.Join(stateDir, pendingFileName))
	if err != nil {
		t.Fatal(err)
	}
	var marker pendingUpdate
	if err := json.Unmarshal(data, &marker); err != nil {
		t.Fatal(err)
	}
	if marker.FromVersion != "1.0.0" || marker.ToVersion != "2.0.0" {
		t.Fatalf("unexpected marker: %+v", marker)
	}
	if marker.BootAttempts != 0 {
		t.Fatalf("BootAttempts = %d, want 0", marker.BootAttempts)
	}
	if !marker.SwappedAt.Equal(now) {
		t.Fatalf("SwappedAt = %v, want %v", marker.SwappedAt, now)
	}
}

func TestConfirmAfterHealthyBoot(t *testing.T) {
	fs := newFakeFS()
	now := time.Date(2024, 1, 2, 3, 4, 5, 0, time.UTC)
	binary := filepath.Join(t.TempDir(), "senhub-agent")
	stateDir := filepath.Join(t.TempDir(), "state")
	env := newTestRollbackEnv(t, fs, binary, stateDir, "2.0.0", true, func() time.Time { return now })

	fs.files[env.previousPath()] = []byte("previous")
	putPending(t, fs, stateDir, pendingUpdate{FromVersion: "1.0.0", ToVersion: "2.0.0", SwappedAt: now.Add(-time.Minute)})
	putJSON(t, fs, filepath.Join(stateDir, rolledBackFileName), RolledBackRecord{
		FailedVersion:   "2.0.0",
		RestoredVersion: "1.0.0",
		RolledBackAt:    now.Add(-time.Minute),
		BootAttempts:    3,
		Outcome:         OutcomeRestored,
	})

	if err := env.confirm(); err != nil {
		t.Fatal(err)
	}
	if fs.Exists(filepath.Join(stateDir, pendingFileName)) {
		t.Fatalf("pending marker was not removed")
	}
	if fs.Exists(filepath.Join(stateDir, rolledBackFileName)) {
		t.Fatalf("rolled-back record was not removed")
	}
	if !fs.Exists(env.previousPath()) {
		t.Fatalf("previous binary should be kept after confirm")
	}
}

func TestBootRollsBackAfterThreeBootsWithoutConfirmation(t *testing.T) {
	fs := newFakeFS()
	now := time.Date(2024, 1, 2, 3, 4, 5, 0, time.UTC)
	binary := filepath.Join(t.TempDir(), "senhub-agent")
	stateDir := filepath.Join(t.TempDir(), "state")
	env := newTestRollbackEnv(t, fs, binary, stateDir, "2.0.0", true, func() time.Time { return now })

	addBinaries(fs, binary, "new-binary", "old-binary")
	putPending(t, fs, stateDir, pendingUpdate{FromVersion: "1.0.0", ToVersion: "2.0.0", SwappedAt: now.Add(-time.Minute), BootAttempts: 2})

	if action := env.boot(); action != bootRolledBack {
		t.Fatalf("action = %v, want bootRolledBack", action)
	}
	if got := readFakeString(t, fs, binary); got != "old-binary" {
		t.Fatalf("binary = %q, want old-binary", got)
	}
	if got := readFakeString(t, fs, env.failedPath()); got != "new-binary" {
		t.Fatalf("failed binary = %q, want new-binary", got)
	}
	if fs.Exists(env.previousPath()) {
		t.Fatalf("previous binary should have been renamed into place")
	}
	if fs.Exists(filepath.Join(stateDir, pendingFileName)) {
		t.Fatalf("pending marker was not removed")
	}

	record := readRolledBackRecord(t, fs, stateDir)
	if record.FailedVersion != "2.0.0" || record.RestoredVersion != "1.0.0" {
		t.Fatalf("unexpected record: %+v", record)
	}
	if record.BootAttempts != 3 || record.Outcome != OutcomeRestored {
		t.Fatalf("unexpected record: %+v", record)
	}
	if !record.RolledBackAt.Equal(now) {
		t.Fatalf("RolledBackAt = %v, want %v", record.RolledBackAt, now)
	}
}

func TestBootAttemptsOneAndTwoReturnTrackingAndDoNotTouchBinaries(t *testing.T) {
	tests := []struct {
		name    string
		initial int
		want    int
	}{
		{name: "first boot", initial: 0, want: 1},
		{name: "second boot", initial: 1, want: 2},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fs := newFakeFS()
			now := time.Date(2024, 1, 2, 3, 4, 5, 0, time.UTC)
			binary := filepath.Join(t.TempDir(), "senhub-agent")
			stateDir := filepath.Join(t.TempDir(), "state")
			env := newTestRollbackEnv(t, fs, binary, stateDir, "2.0.0", true, func() time.Time { return now })

			addBinaries(fs, binary, "new-binary", "old-binary")
			putPending(t, fs, stateDir, pendingUpdate{FromVersion: "1.0.0", ToVersion: "2.0.0", SwappedAt: now.Add(-time.Minute), BootAttempts: tt.initial})

			if action := env.boot(); action != bootTracking {
				t.Fatalf("action = %v, want bootTracking", action)
			}
			if got := readFakeString(t, fs, binary); got != "new-binary" {
				t.Fatalf("binary = %q, want new-binary", got)
			}
			if got := readFakeString(t, fs, env.previousPath()); got != "old-binary" {
				t.Fatalf("previous binary = %q, want old-binary", got)
			}
			if got := readPending(t, fs, stateDir).BootAttempts; got != tt.want {
				t.Fatalf("BootAttempts = %d, want %d", got, tt.want)
			}
		})
	}
}

func TestBootAfterRollbackWithRestoredVersionReturnsBootNone(t *testing.T) {
	fs := newFakeFS()
	now := time.Date(2024, 1, 2, 3, 4, 5, 0, time.UTC)
	binary := filepath.Join(t.TempDir(), "senhub-agent")
	stateDir := filepath.Join(t.TempDir(), "state")
	env := newTestRollbackEnv(t, fs, binary, stateDir, "1.0.0", true, func() time.Time { return now })

	fs.files[binary] = []byte("old-binary")

	if action := env.boot(); action != bootNone {
		t.Fatalf("action = %v, want bootNone", action)
	}
	if got := readFakeString(t, fs, binary); got != "old-binary" {
		t.Fatalf("binary = %q, want old-binary", got)
	}
	if fs.Exists(env.failedPath()) {
		t.Fatalf("failed binary should not exist")
	}
}

func TestBootStaleMarkerForOtherVersionRemovesMarker(t *testing.T) {
	fs := newFakeFS()
	now := time.Date(2024, 1, 2, 3, 4, 5, 0, time.UTC)
	binary := filepath.Join(t.TempDir(), "senhub-agent")
	stateDir := filepath.Join(t.TempDir(), "state")
	env := newTestRollbackEnv(t, fs, binary, stateDir, "1.0.0", true, func() time.Time { return now })

	putPending(t, fs, stateDir, pendingUpdate{FromVersion: "1.0.0", ToVersion: "2.0.0", SwappedAt: now.Add(-time.Minute), BootAttempts: 2})

	if action := env.boot(); action != bootNone {
		t.Fatalf("action = %v, want bootNone", action)
	}
	if fs.Exists(filepath.Join(stateDir, pendingFileName)) {
		t.Fatalf("stale pending marker was not removed")
	}
}

func TestBootCorruptMarkerJSONIsDiscarded(t *testing.T) {
	fs := newFakeFS()
	now := time.Date(2024, 1, 2, 3, 4, 5, 0, time.UTC)
	binary := filepath.Join(t.TempDir(), "senhub-agent")
	stateDir := filepath.Join(t.TempDir(), "state")
	env := newTestRollbackEnv(t, fs, binary, stateDir, "2.0.0", true, func() time.Time { return now })

	fs.files[filepath.Join(stateDir, pendingFileName)] = []byte("{not-json")

	if action := env.boot(); action != bootNone {
		t.Fatalf("action = %v, want bootNone", action)
	}
	if fs.Exists(filepath.Join(stateDir, pendingFileName)) {
		t.Fatalf("corrupt pending marker was not removed")
	}
}

func TestBootEmptyMarkerFileIsDiscarded(t *testing.T) {
	fs := newFakeFS()
	now := time.Date(2024, 1, 2, 3, 4, 5, 0, time.UTC)
	binary := filepath.Join(t.TempDir(), "senhub-agent")
	stateDir := filepath.Join(t.TempDir(), "state")
	env := newTestRollbackEnv(t, fs, binary, stateDir, "2.0.0", true, func() time.Time { return now })

	fs.files[filepath.Join(stateDir, pendingFileName)] = []byte{}

	if action := env.boot(); action != bootNone {
		t.Fatalf("action = %v, want bootNone", action)
	}
	if fs.Exists(filepath.Join(stateDir, pendingFileName)) {
		t.Fatalf("empty pending marker was not removed")
	}
}

func TestBootAttemptsMaxButOutsideWindowRemovesMarkerWithoutRollback(t *testing.T) {
	fs := newFakeFS()
	now := time.Date(2024, 1, 2, 3, 4, 5, 0, time.UTC)
	binary := filepath.Join(t.TempDir(), "senhub-agent")
	stateDir := filepath.Join(t.TempDir(), "state")
	env := newTestRollbackEnv(t, fs, binary, stateDir, "2.0.0", true, func() time.Time { return now })

	addBinaries(fs, binary, "new-binary", "old-binary")
	putPending(t, fs, stateDir, pendingUpdate{FromVersion: "1.0.0", ToVersion: "2.0.0", SwappedAt: now.Add(-11 * time.Minute), BootAttempts: 2})

	if action := env.boot(); action != bootNone {
		t.Fatalf("action = %v, want bootNone", action)
	}
	if fs.Exists(filepath.Join(stateDir, pendingFileName)) {
		t.Fatalf("expired pending marker was not removed")
	}
	if got := readFakeString(t, fs, binary); got != "new-binary" {
		t.Fatalf("binary = %q, want new-binary", got)
	}
	if got := readFakeString(t, fs, env.previousPath()); got != "old-binary" {
		t.Fatalf("previous binary = %q, want old-binary", got)
	}
	if fs.Exists(env.failedPath()) {
		t.Fatalf("failed binary should not exist")
	}
}

func TestBootPreviousMissingRecordsNotPerformedAndKeepsNewBinary(t *testing.T) {
	fs := newFakeFS()
	now := time.Date(2024, 1, 2, 3, 4, 5, 0, time.UTC)
	binary := filepath.Join(t.TempDir(), "senhub-agent")
	stateDir := filepath.Join(t.TempDir(), "state")
	env := newTestRollbackEnv(t, fs, binary, stateDir, "2.0.0", true, func() time.Time { return now })

	fs.files[binary] = []byte("new-binary")
	putPending(t, fs, stateDir, pendingUpdate{FromVersion: "1.0.0", ToVersion: "2.0.0", SwappedAt: now.Add(-time.Minute), BootAttempts: 2})

	if action := env.boot(); action != bootRollbackNotPerformed {
		t.Fatalf("action = %v, want bootRollbackNotPerformed", action)
	}
	if got := readFakeString(t, fs, binary); got != "new-binary" {
		t.Fatalf("binary = %q, want new-binary", got)
	}
	if fs.Exists(env.failedPath()) {
		t.Fatalf("failed binary should not exist")
	}
	if fs.Exists(env.previousPath()) {
		t.Fatalf("previous binary should still be missing")
	}
	if fs.Exists(filepath.Join(stateDir, pendingFileName)) {
		t.Fatalf("pending marker was not removed")
	}
	record := readRolledBackRecord(t, fs, stateDir)
	if record.Outcome != OutcomeNotPerformed || record.FailedVersion != "2.0.0" || record.RestoredVersion != "1.0.0" {
		t.Fatalf("unexpected record: %+v", record)
	}
}

func TestBootCanSwapFalseRecordsNotPerformed(t *testing.T) {
	fs := newFakeFS()
	now := time.Date(2024, 1, 2, 3, 4, 5, 0, time.UTC)
	binary := filepath.Join(t.TempDir(), "senhub-agent")
	stateDir := filepath.Join(t.TempDir(), "state")
	env := newTestRollbackEnv(t, fs, binary, stateDir, "2.0.0", false, func() time.Time { return now })

	addBinaries(fs, binary, "new-binary", "old-binary")
	putPending(t, fs, stateDir, pendingUpdate{FromVersion: "1.0.0", ToVersion: "2.0.0", SwappedAt: now.Add(-time.Minute), BootAttempts: 2})

	if action := env.boot(); action != bootRollbackNotPerformed {
		t.Fatalf("action = %v, want bootRollbackNotPerformed", action)
	}
	if got := readFakeString(t, fs, binary); got != "new-binary" {
		t.Fatalf("binary = %q, want new-binary", got)
	}
	if got := readFakeString(t, fs, env.previousPath()); got != "old-binary" {
		t.Fatalf("previous binary = %q, want old-binary", got)
	}
	if fs.Exists(env.failedPath()) {
		t.Fatalf("failed binary should not exist")
	}
	record := readRolledBackRecord(t, fs, stateDir)
	if record.Outcome != OutcomeNotPerformed {
		t.Fatalf("Outcome = %q, want %q", record.Outcome, OutcomeNotPerformed)
	}
}

func TestBootSecondRenameFailureRestoresOriginalBinary(t *testing.T) {
	fs := newFakeFS()
	now := time.Date(2024, 1, 2, 3, 4, 5, 0, time.UTC)
	binary := filepath.Join(t.TempDir(), "senhub-agent")
	stateDir := filepath.Join(t.TempDir(), "state")
	env := newTestRollbackEnv(t, fs, binary, stateDir, "2.0.0", true, func() time.Time { return now })

	addBinaries(fs, binary, "new-binary", "old-binary")
	fs.renameErrs[binary] = errors.New("injected rename failure")
	putPending(t, fs, stateDir, pendingUpdate{FromVersion: "1.0.0", ToVersion: "2.0.0", SwappedAt: now.Add(-time.Minute), BootAttempts: 2})

	if action := env.boot(); action != bootRollbackNotPerformed {
		t.Fatalf("action = %v, want bootRollbackNotPerformed", action)
	}
	if got := readFakeString(t, fs, binary); got != "new-binary" {
		t.Fatalf("binary = %q, want new-binary", got)
	}
	if fs.Exists(env.failedPath()) {
		t.Fatalf("failed binary should have been renamed back")
	}
	if got := readFakeString(t, fs, env.previousPath()); got != "old-binary" {
		t.Fatalf("previous binary = %q, want old-binary", got)
	}
	record := readRolledBackRecord(t, fs, stateDir)
	if record.Outcome != OutcomeNotPerformed {
		t.Fatalf("Outcome = %q, want %q", record.Outcome, OutcomeNotPerformed)
	}
}

func TestConfirmWithNoMarkerIsNoOp(t *testing.T) {
	fs := newFakeFS()
	now := time.Date(2024, 1, 2, 3, 4, 5, 0, time.UTC)
	binary := filepath.Join(t.TempDir(), "senhub-agent")
	stateDir := filepath.Join(t.TempDir(), "state")
	env := newTestRollbackEnv(t, fs, binary, stateDir, "2.0.0", true, func() time.Time { return now })

	if err := env.confirm(); err != nil {
		t.Fatal(err)
	}
	if fs.Exists(filepath.Join(stateDir, pendingFileName)) {
		t.Fatalf("pending marker should not exist")
	}
}

func TestConfirmWithMarkerForOtherVersionLeavesIt(t *testing.T) {
	fs := newFakeFS()
	now := time.Date(2024, 1, 2, 3, 4, 5, 0, time.UTC)
	binary := filepath.Join(t.TempDir(), "senhub-agent")
	stateDir := filepath.Join(t.TempDir(), "state")
	env := newTestRollbackEnv(t, fs, binary, stateDir, "1.0.0", true, func() time.Time { return now })

	putPending(t, fs, stateDir, pendingUpdate{FromVersion: "1.0.0", ToVersion: "2.0.0", SwappedAt: now.Add(-time.Minute)})

	if err := env.confirm(); err != nil {
		t.Fatal(err)
	}
	if !fs.Exists(filepath.Join(stateDir, pendingFileName)) {
		t.Fatalf("pending marker for another version should have been left in place")
	}
}

func TestSkipsVersion(t *testing.T) {
	tests := []struct {
		name          string
		record        *RolledBackRecord
		recordJSON    string
		candidate     string
		wantSkip      bool
		wantRecordNil bool
	}{
		{name: "same version", record: &RolledBackRecord{FailedVersion: "2.0.0"}, candidate: "2.0.0", wantSkip: true},
		{name: "older version", record: &RolledBackRecord{FailedVersion: "2.0.0"}, candidate: "1.9.0", wantSkip: true},
		{name: "newer version", record: &RolledBackRecord{FailedVersion: "2.0.0"}, candidate: "2.1.0", wantSkip: false},
		{name: "unparseable candidate", record: &RolledBackRecord{FailedVersion: "2.0.0"}, candidate: "not-a-version", wantSkip: true},
		{name: "unparseable failed version", record: &RolledBackRecord{FailedVersion: "not-a-version"}, candidate: "2.1.0", wantSkip: true},
		{name: "no record", candidate: "2.1.0", wantSkip: false, wantRecordNil: true},
		{name: "unreadable record", recordJSON: "not-json", candidate: "2.1.0", wantSkip: false, wantRecordNil: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fs := newFakeFS()
			binary := filepath.Join(t.TempDir(), "senhub-agent")
			stateDir := filepath.Join(t.TempDir(), "state")

			if tt.recordJSON != "" {
				fs.files[filepath.Join(stateDir, rolledBackFileName)] = []byte(tt.recordJSON)
			} else if tt.record != nil {
				putJSON(t, fs, filepath.Join(stateDir, rolledBackFileName), *tt.record)
			}

			env := newTestRollbackEnv(t, fs, binary, stateDir, "2.0.0", true, func() time.Time { return time.Unix(0, 0) })
			skip, record := env.skipsVersion(tt.candidate)

			if skip != tt.wantSkip {
				t.Fatalf("skip = %v, want %v", skip, tt.wantSkip)
			}
			if (record == nil) != tt.wantRecordNil {
				t.Fatalf("record nil = %v, want nil=%v", record == nil, tt.wantRecordNil)
			}
		})
	}
}

func TestRollbackSequenceNeverRollsForwardAgain(t *testing.T) {
	fs := newFakeFS()
	now := time.Date(2024, 1, 2, 3, 4, 5, 0, time.UTC)
	binary := filepath.Join(t.TempDir(), "senhub-agent")
	stateDir := filepath.Join(t.TempDir(), "state")
	clock := func() time.Time { return now }
	newVersion := newTestRollbackEnv(t, fs, binary, stateDir, "2.0.0", true, clock)

	addBinaries(fs, binary, "new-binary", "old-binary")
	if err := newVersion.recordSwap("1.0.0", "2.0.0"); err != nil {
		t.Fatal(err)
	}
	for boot, want := range []bootAction{bootTracking, bootTracking, bootRolledBack} {
		now = now.Add(20 * time.Second)
		if got := newVersion.boot(); got != want {
			t.Fatalf("boot %d: action = %v, want %v", boot+1, got, want)
		}
	}

	restored := newTestRollbackEnv(t, fs, binary, stateDir, "1.0.0", true, clock)
	for i := 0; i < 5; i++ {
		if got := restored.boot(); got != bootNone {
			t.Fatalf("restored boot %d: action = %v, want bootNone", i+1, got)
		}
	}
	if got := readFakeString(t, fs, binary); got != "old-binary" {
		t.Fatalf("binary = %q, want old-binary", got)
	}
	if skip, _ := restored.skipsVersion("2.0.0"); !skip {
		t.Error("the failed version must be skipped by the periodic check")
	}
	if skip, _ := restored.skipsVersion("2.0.1"); skip {
		t.Error("a newer release must not be skipped")
	}
}

func TestBootClearsANotPerformedRecordOnceTheOperatorRestored(t *testing.T) {
	tests := []struct {
		name     string
		version  string
		outcome  string
		wantKept bool
	}{
		{name: "still on the failed version", version: "2.0.0", outcome: OutcomeNotPerformed, wantKept: true},
		{name: "operator went back", version: "1.0.0", outcome: OutcomeNotPerformed, wantKept: false},
		{name: "restored record holds the version back", version: "1.0.0", outcome: OutcomeRestored, wantKept: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fs := newFakeFS()
			stateDir := filepath.Join(t.TempDir(), "state")
			putJSON(t, fs, filepath.Join(stateDir, rolledBackFileName), RolledBackRecord{FailedVersion: "2.0.0", RestoredVersion: "1.0.0", Outcome: tt.outcome})
			env := newTestRollbackEnv(t, fs, filepath.Join(t.TempDir(), "senhub-agent"), stateDir, tt.version, true, time.Now)

			if got := env.boot(); got != bootNone {
				t.Fatalf("action = %v, want bootNone", got)
			}
			if kept := fs.Exists(filepath.Join(stateDir, rolledBackFileName)); kept != tt.wantKept {
				t.Fatalf("record kept = %v, want %v", kept, tt.wantKept)
			}
		})
	}
}

func TestCanonicalVersionIgnoresTypedPrefix(t *testing.T) {
	for _, in := range []string{"0.6.3", "v0.6.3", " v0.6.3 "} {
		if got := canonicalVersion(in); got != "0.6.3" {
			t.Errorf("canonicalVersion(%q) = %q, want 0.6.3", in, got)
		}
	}
}
