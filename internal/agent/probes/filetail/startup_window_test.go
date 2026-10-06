package filetail

import (
	"path/filepath"
	"testing"
	"time"
)

// nxadm/tail registers its change watch only after its first read reaches
// the end of the file. A line appended between that read and the
// registration raises no event, and on a quiet log nothing wakes the tail
// for the next write, however far away. The probe reads such a line once
// the startup check has seen the file grow with nothing read.
func TestFileTail_LineAppendedInTheStartupWindowIsReadWithoutAnotherWrite(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "quiet.log")
	writeFile(t, file, backlog(10))

	p := startFileTail(t, map[string]interface{}{
		"paths":         []interface{}{file},
		"bookmark_path": filepath.Join(dir, "bookmark.json"),
	})
	defer stopFileTail(t, p)

	// Straight after the start, inside the window, then silence.
	appendLines(t, file, "2026-09-30 11:00:00 INFO written inside the startup window")

	if got := waitEmitted(p, 1, 2*startupVerifyDelay+4*time.Second); got != 1 {
		t.Fatalf("emitted %d records, want the line appended inside the startup window", got)
	}
	time.Sleep(300 * time.Millisecond)
	if got := p.emitted.Load(); got != 1 {
		t.Fatalf("emitted %d records, want exactly 1 (no replay after the restart at the offset)", got)
	}
}
