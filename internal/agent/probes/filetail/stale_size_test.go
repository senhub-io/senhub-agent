package filetail

import (
	"io"
	"os"
	"path/filepath"
	"testing"
)

func staleSizeTail(t *testing.T) (ts *tailState, path string, f *os.File, size int64) {
	t.Helper()
	path = filepath.Join(t.TempDir(), "app.log")
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { f.Close() })
	size = writeLines(t, f, 1, 20)
	fp := fingerprint(path, DefaultFingerprintLength)
	if fp == "" {
		t.Fatal("initial file did not reach the fingerprint window")
	}
	fi, err := f.Stat()
	if err != nil {
		t.Fatal(err)
	}
	ts = &tailState{}
	ts.mu.Lock()
	ts.opened = fi
	ts.headFP = fp
	ts.mu.Unlock()
	return ts, path, f, size
}

// A size seen by path that lags the offset the tail already read, on a
// file whose first bytes did not change, is metadata lagging a writer that
// holds the file open: nothing was truncated and nothing may be re-read.
func TestDivergenceIgnoresAStaleSizeWhenTheHeadIsUnchanged(t *testing.T) {
	ts, path, _, size := staleSizeTail(t)
	ts.offset.Store(size + 4096)

	if reason, resume := ts.divergence(path); reason != "" || resume {
		t.Fatalf("a stale size was read as a truncation: reason=%q resume=%v", reason, resume)
	}
}

func TestDivergenceStillSeesATruncationRefilledWithOtherContent(t *testing.T) {
	ts, path, f, size := staleSizeTail(t)
	ts.offset.Store(size)

	if err := f.Truncate(0); err != nil {
		t.Fatal(err)
	}
	if _, err := f.Seek(0, io.SeekStart); err != nil {
		t.Fatal(err)
	}
	writeLines(t, f, 100, 14)

	reason, _ := ts.divergence(path)
	if reason == "" {
		t.Fatal("a real truncation followed by other content was not detected")
	}
}

func TestDivergenceStillSeesATruncationToNothing(t *testing.T) {
	ts, path, f, size := staleSizeTail(t)
	ts.offset.Store(size)

	if err := f.Truncate(0); err != nil {
		t.Fatal(err)
	}

	if reason, _ := ts.divergence(path); reason != "the file is shorter than the offset already read" {
		t.Fatalf("reason = %q, want the truncation to be seen", reason)
	}
}
