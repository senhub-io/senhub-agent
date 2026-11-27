package filetail

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// A probe stopped right after a hot reload, on a fresh bookmark file, must
// not replay the whole file on the next start, even when the file is
// reached through a symlinked directory (ent#123).
func TestFileTail_StopRightAfterStartDoesNotReplayThroughASymlinkedDir(t *testing.T) {
	real := filepath.Join(t.TempDir(), "unifi")
	if err := os.MkdirAll(real, 0o755); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(t.TempDir(), "logs")
	if err := os.Symlink(real, link); err != nil {
		t.Skipf("symlink: %v", err)
	}
	path := filepath.Join(link, "server.log")
	if err := os.WriteFile(filepath.Join(real, "server.log"), []byte(backlog(40)), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg := map[string]interface{}{
		"paths":         []interface{}{path},
		"bookmark_path": filepath.Join(t.TempDir(), "new.json"),
		"multiline":     map[string]interface{}{"pattern": `^\d{4}-\d{2}-\d{2} `},
	}

	first := startFileTail(t, cfg)
	stopFileTail(t, first)

	second := startFileTail(t, cfg)
	defer stopFileTail(t, second)
	if got := waitEmitted(second, 1, time.Second); got != 0 {
		t.Fatalf("restart replayed %d records of a file stopped right after start", got)
	}

	// The tail resumes at the bookmark, the end of the 40-line backlog:
	// only what is written now comes out. Multiline holds the last record
	// until the next line opens one, so two lines give exactly one.
	appendLines(t, path, "2026-09-30 11:00:00 INFO after immediate stop",
		"2026-09-30 11:00:01 INFO closes the record")
	if got := waitEmitted(second, 1, 30*time.Second); got != 1 {
		t.Fatalf("second run emitted %d records, want 1", got)
	}
	time.Sleep(500 * time.Millisecond)
	if got := second.emitted.Load(); got != 1 {
		t.Fatalf("second run emitted %d records, want 1: the backlog was replayed", got)
	}
}
