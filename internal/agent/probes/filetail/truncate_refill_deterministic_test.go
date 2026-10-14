package filetail

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeLines(t *testing.T, f *os.File, first, count int) int64 {
	t.Helper()
	for i := first; i < first+count; i++ {
		if _, err := fmt.Fprintf(f, "line %03d %s\n", i, strings.Repeat("x", 80)); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := f.Seek(0, io.SeekEnd); err != nil {
		t.Fatal(err)
	}
	fi, err := f.Stat()
	if err != nil {
		t.Fatal(err)
	}
	return fi.Size()
}

func TestRestartedAtStartPreventsFingerprintResetAfterTruncation(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "app.log")
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()

	oldSize := writeLines(t, f, 1, 20)
	oldFP := fingerprint(path, DefaultFingerprintLength)
	if oldFP == "" {
		t.Fatal("initial file did not reach fingerprint window")
	}
	fi, err := f.Stat()
	if err != nil {
		t.Fatal(err)
	}

	ts := &tailState{}
	ts.mu.Lock()
	ts.opened = fi
	ts.offset.Store(oldSize)
	ts.headFP = oldFP
	ts.checkedOffset = oldSize
	ts.mu.Unlock()

	// Truncate and write the first 10 lines of the new content.
	if err := f.Truncate(0); err != nil {
		t.Fatal(err)
	}
	if _, err := f.Seek(0, io.SeekStart); err != nil {
		t.Fatal(err)
	}
	sizeAfter10 := writeLines(t, f, 21, 10)

	// The running tail saw the size shrink and resumed at 0 on its own.
	ts.restartedAtStart(path)
	ts.offset.Store(sizeAfter10)

	// Then the writer refills past the offset reached before the truncation.
	sizeAfter60 := writeLines(t, f, 31, 50)
	ts.offset.Store(sizeAfter60)

	reason, resume := ts.divergence(path)
	if reason != "" || resume {
		t.Fatalf("fingerprint poll reset again after the tail had already restarted: reason=%q resume=%v", reason, resume)
	}
}

func TestFingerprintDetectRefillPastOffsetBetweenPolls(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "app.log")
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()

	oldSize := writeLines(t, f, 1, 20)
	oldFP := fingerprint(path, DefaultFingerprintLength)
	if oldFP == "" {
		t.Fatal("initial file did not reach fingerprint window")
	}
	oldFi, err := f.Stat()
	if err != nil {
		t.Fatal(err)
	}

	// Truncate and refill past the old offset before any poll can react.
	if err := f.Truncate(0); err != nil {
		t.Fatal(err)
	}
	if _, err := f.Seek(0, io.SeekStart); err != nil {
		t.Fatal(err)
	}
	newSize := writeLines(t, f, 21, 60)
	newFP := fingerprint(path, DefaultFingerprintLength)
	if newFP == "" || newFP == oldFP {
		t.Fatalf("test setup did not produce a changed fingerprint: old=%q new=%q", oldFP, newFP)
	}

	ts := &tailState{}
	ts.mu.Lock()
	ts.opened = oldFi
	ts.offset.Store(oldSize)
	ts.headFP = oldFP
	ts.checkedOffset = oldSize
	ts.mu.Unlock()

	reason, resume := ts.divergence(path)
	if reason != "the start of the file changed since it was opened" || resume {
		t.Fatalf("divergence did not detect refill past offset: reason=%q resume=%v", reason, resume)
	}

	got := resolveStartOffset(bookmarkEntry{Offset: oldSize, Fingerprint: oldFP}, true, newFP, newSize, false)
	if got != 0 {
		t.Fatalf("resolveStartOffset with a stale fingerprint = %d, want 0", got)
	}
}
