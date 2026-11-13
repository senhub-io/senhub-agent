package filetail

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// A bookmark write refused right after a rotation (on Windows, a rename
// denied while another handle has the file open) must be retried at the
// next flush, even when the new file stays quiet: otherwise the disk keeps
// the previous file's offset and a restart resumes the new file there.
func TestFileTail_FailedBookmarkWriteIsRetriedOnAQuietFile(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "App.log")
	if err := os.WriteFile(file, []byte(backlog(40)), 0o644); err != nil {
		t.Fatalf("write backlog: %v", err)
	}
	bookmarkPath := filepath.Join(dir, "bookmark.json")
	cfg := map[string]interface{}{
		"paths":         []interface{}{file},
		"bookmark_path": bookmarkPath,
	}

	p := startFileTail(t, cfg)
	defer stopFileTail(t, p)
	awaitTailWatching(t, p, file)

	info, err := os.Stat(file)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	waitBookmarkOnDisk(t, bookmarkPath, file, func(e bookmarkEntry) bool {
		return e.Offset == info.Size()
	}, 30*time.Second)

	// A rename onto a non-empty directory fails on Linux, macOS and Windows.
	if err := os.Remove(bookmarkPath); err != nil {
		t.Fatalf("remove bookmark: %v", err)
	}
	if err := os.Mkdir(bookmarkPath, 0o755); err != nil {
		t.Fatalf("mkdir bookmark: %v", err)
	}
	if err := os.WriteFile(filepath.Join(bookmarkPath, "keep"), []byte("x"), 0o644); err != nil {
		t.Fatalf("write keep: %v", err)
	}

	// The new file stays empty: no line will trigger another write.
	if err := os.Rename(file, file+".1"); err != nil {
		t.Fatalf("rotate: %v", err)
	}
	if err := os.WriteFile(file, nil, 0o644); err != nil {
		t.Fatalf("recreate: %v", err)
	}

	// At least one flush tick fails while the path is a directory.
	time.Sleep(3 * time.Second)

	if err := os.RemoveAll(bookmarkPath); err != nil {
		t.Fatalf("unblock bookmark: %v", err)
	}

	waitBookmarkOnDisk(t, bookmarkPath, file, func(e bookmarkEntry) bool {
		return e.Offset == 0 && e.Fingerprint == ""
	}, 15*time.Second)
}
