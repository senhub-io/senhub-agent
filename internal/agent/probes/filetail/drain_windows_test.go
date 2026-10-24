//go:build windows

package filetail

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"senhub-agent.go/internal/agent/services/agentstate"
)

const drainBound = 1500 * time.Millisecond

// retryFor repeats op until it succeeds or the bound passes, and returns the
// time the successful attempt came at.
func retryFor(bound time.Duration, op func() error) (time.Duration, error) {
	start := time.Now()
	for {
		err := op()
		if err == nil {
			return time.Since(start), nil
		}
		if time.Since(start) > bound {
			return time.Since(start), err
		}
		time.Sleep(25 * time.Millisecond)
	}
}

// rotatedScenario rotates app.log by rename-and-create and calls during
// once the probe has read the rotated file and the new file's first line,
// which is when the drain window opens.
func rotatedScenario(t *testing.T, during func(dir, file, old string)) {
	t.Helper()
	dir := t.TempDir()
	file := filepath.Join(dir, "app.log")
	writeFile(t, file, "")

	records := agentstate.SubscribeLogs(4096)
	defer agentstate.UnsubscribeLogs(records)

	p := startPollingFileTail(t, file, filepath.Join(dir, "bookmark.json"))
	defer stopFileTail(t, p)
	awaitTailWatching(t, p, file)

	appendLines(t, file, "a-1")
	time.Sleep(400 * time.Millisecond)
	old := file + ".1"
	if err := os.Rename(file, old); err != nil {
		t.Fatalf("first rotation: %v", err)
	}
	appendLines(t, old, "old-1")
	writeFile(t, file, "")
	appendLines(t, file, "new-1")
	waitEmittedAtLeastLine(t, records, file, "new-1")
	during(dir, file, old)
}

// Lines written to the old file just before it is renamed away, then the new
// file created: all are read, none twice. Rotations are three seconds apart:
// a file that lives less than the rotation check's two looks (about 2 s) can
// be renamed away before the tail notices it, which no drain recovers.
func TestFileTailWindows_DrainsLinesWrittenBeforeRename(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "app.log")
	writeFile(t, file, "")

	records := agentstate.SubscribeLogs(4096)
	defer agentstate.UnsubscribeLogs(records)

	p := startPollingFileTail(t, file, filepath.Join(dir, "bookmark.json"))
	defer stopFileTail(t, p)
	awaitTailWatching(t, p, file)

	const n, m = 20, 10
	var want []string
	for round := 0; round < 3; round++ {
		old := fmt.Sprintf("%s.%d", file, round)
		for i := 0; i < n; i++ {
			l := fmt.Sprintf("r%d-old-%02d", round, i)
			want = append(want, l)
			appendLines(t, file, l)
		}
		if err := os.Rename(file, old); err != nil {
			t.Fatalf("round %d: renaming the rotated file: %v", round, err)
		}
		writeFile(t, file, "")
		for i := 0; i < m; i++ {
			l := fmt.Sprintf("r%d-new-%02d", round, i)
			want = append(want, l)
			appendLines(t, file, l)
		}
		time.Sleep(3 * time.Second)
	}
	got := collectBodies(t, records, file, len(want))
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("line %d = %q, want %q (all: %q)", i, got[i], want[i], got)
		}
	}
}

// The application keeps rotating while the probe drains: none of its
// operations on the drained file may be refused.
func TestFileTailWindows_RotatedFileCanBeRenamedAgainDuringDrain(t *testing.T) {
	rotatedScenario(t, func(dir, file, old string) {
		if err := os.Rename(old, file+".2"); err != nil {
			t.Fatalf("renaming the drained file (app.log.1 to app.log.2) failed: %v", err)
		}
	})
}

func TestFileTailWindows_RotatedFileCanBeDeletedDuringDrain(t *testing.T) {
	rotatedScenario(t, func(dir, file, old string) {
		if err := os.Remove(old); err != nil {
			t.Fatalf("deleting the drained file (app.log.1) failed: %v", err)
		}
	})
}

// A rename-with-replace onto the name of the file being drained can be
// refused by Windows while the probe holds that file. The probe releases it
// within one second, so the rename must succeed within the bound; the time
// it took is the honest limit: a rotation onto the same backup name within
// a second of the previous one may wait.
func TestFileTailWindows_RenameReplacingDrainedFileSucceedsWithinBound(t *testing.T) {
	rotatedScenario(t, func(dir, file, old string) {
		elapsed, err := retryFor(drainBound, func() error { return os.Rename(file, old) })
		if err != nil {
			t.Fatalf("renaming app.log onto the drained app.log.1 still failed after %v: %v", elapsed, err)
		}
		t.Logf("rename of app.log onto the drained app.log.1 succeeded after %v", elapsed)
	})
}

// The drain handle is released within the bound: the drained file can be
// deleted and its name reused.
func TestFileTailWindows_DrainHandleReleasedWithinBound(t *testing.T) {
	rotatedScenario(t, func(dir, file, old string) {
		if err := os.Remove(old); err != nil {
			t.Fatalf("deleting the drained file failed: %v", err)
		}
		elapsed, err := retryFor(drainBound, func() error {
			return os.WriteFile(old, []byte("reused\n"), 0o644)
		})
		if err != nil {
			t.Fatalf("the name app.log.1 was not reusable after %v: %v", elapsed, err)
		}
		t.Logf("name reused after %v", elapsed)
	})
}
