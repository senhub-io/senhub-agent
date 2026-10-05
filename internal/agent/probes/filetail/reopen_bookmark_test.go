package filetail

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

// waitBookmarkOnDisk polls the bookmark file, as a crash would leave it,
// until the entry of file satisfies ok.
func waitBookmarkOnDisk(t *testing.T, bookmarkPath, file string, ok func(bookmarkEntry) bool, within time.Duration) bookmarkEntry {
	t.Helper()
	var last bookmarkEntry
	deadline := time.Now().Add(within)
	for time.Now().Before(deadline) {
		store, err := newBookmark(bookmarkPath)
		if err != nil {
			t.Fatal(err)
		}
		if e, found := store.Get(file); found {
			last = e
			if ok(e) {
				return e
			}
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("bookmark of %s on disk = %+v after %v, never reached the expected state", file, last, within)
	return last
}

// newFileLines writes n distinct lines, more bytes in all than the
// previous file's offset, so a stale offset would land inside them.
func newFileLines(n int) []string {
	lines := make([]string, n)
	for i := range lines {
		lines[i] = fmt.Sprintf("2026-09-30 12:00:%02d INFO line %03d written to the new file after rotation", i%60, i)
	}
	return lines
}

// An idle file rotated or truncated is reopened by the tail, which reads
// the new file from its first byte. The bookmark must say so before any
// line arrives: it kept the previous file's offset, so a restart in that
// window resumed the new file there and skipped its first lines (#999).
func TestFileTail_ReopenAfterRotationBookmarksTheNewFileStart(t *testing.T) {
	cases := []struct {
		name   string
		rotate func(t *testing.T, file string)
	}{
		{"rename and recreate", func(t *testing.T, file string) {
			if err := os.Rename(file, file+".1"); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(file, nil, 0o644); err != nil {
				t.Fatal(err)
			}
		}},
		{"truncate", func(t *testing.T, file string) {
			if err := os.Truncate(file, 0); err != nil {
				t.Fatal(err)
			}
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// Windows tails by polling (filetail_probe.go), and nxadm/tail's
			// poller detects a truncation from os.Stat(path).Size(). Go's
			// Stat uses GetFileAttributesEx on Windows, which reads the
			// directory entry, and NTFS refreshes that size lazily while
			// other handles are open: the tail's own read handle is one, so
			// a size dropped to 0 by SetEndOfFile can go unseen and the
			// reopen never fires. Renames are caught by SameFile and
			// "not exist" instead, which is why only this case is skipped.
			if tc.name == "truncate" && runtime.GOOS == "windows" {
				t.Skip("truncation is not observable through os.Stat while the tail holds the file open on Windows")
			}
			dir := t.TempDir()
			file := filepath.Join(dir, "App.log")
			old := backlog(40)
			if err := os.WriteFile(file, []byte(old), 0o644); err != nil {
				t.Fatal(err)
			}
			bookmarkPath := filepath.Join(dir, "bookmark.json")
			cfg := map[string]interface{}{
				"paths":         []interface{}{file},
				"bookmark_path": bookmarkPath,
			}

			first := startFileTail(t, cfg)
			waitBookmarkOnDisk(t, bookmarkPath, file, func(e bookmarkEntry) bool {
				return e.Offset == int64(len(old))
			}, 2*time.Second)
			// Let the watch settle on the idle file before rotating it.
			time.Sleep(200 * time.Millisecond)

			tc.rotate(t, file)
			waitBookmarkOnDisk(t, bookmarkPath, file, func(e bookmarkEntry) bool {
				return e.Offset == 0 && e.Fingerprint == ""
			}, 5*time.Second)

			stopFileTail(t, first)
			if got := first.emitted.Load(); got != 0 {
				t.Fatalf("first run emitted %d records, want 0: nothing was written", got)
			}

			// Written while the agent is down, as a busy log does between a
			// rotation and the next start.
			lines := newFileLines(60)
			appendLines(t, file, lines...)

			second := startFileTail(t, cfg)
			defer stopFileTail(t, second)
			want := uint64(len(lines))
			if got := waitEmitted(second, want, 5*time.Second); got != want {
				t.Fatalf("restart emitted %d of the %d lines of the new file", got, want)
			}
			time.Sleep(300 * time.Millisecond)
			if got := second.emitted.Load(); got != want {
				t.Fatalf("restart emitted %d records, want exactly %d", got, want)
			}
		})
	}
}
