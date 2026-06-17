package windowseventlog

import (
	"os"
	"path/filepath"
	"testing"
)

func TestBookmarkStore_MissingFileIsEmpty(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nope", "bookmark.json")
	s, err := newBookmarkStore(path)
	if err != nil {
		t.Fatalf("missing file should not error: %v", err)
	}
	if got := s.get("System"); got != "" {
		t.Errorf("expected empty bookmark, got %q", got)
	}
}

func TestBookmarkStore_RoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sub", "dir", "bookmark.json")
	s, _ := newBookmarkStore(path)
	s.set("System", "<BookmarkList><Bookmark Channel='System' RecordId='42'/></BookmarkList>")
	s.set("Application", "<BookmarkList><Bookmark Channel='Application' RecordId='7'/></BookmarkList>")
	if err := s.persist(); err != nil {
		t.Fatalf("persist: %v", err)
	}

	// Fresh store reads the same values back — survives a "restart".
	s2, err := newBookmarkStore(path)
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	if got := s2.get("System"); got == "" || got != s.get("System") {
		t.Errorf("System bookmark not restored: %q", got)
	}
	if got := s2.get("Application"); got != s.get("Application") {
		t.Errorf("Application bookmark not restored: %q", got)
	}
}

func TestBookmarkStore_EmptyPathDisablesPersistence(t *testing.T) {
	s, err := newBookmarkStore("")
	if err != nil {
		t.Fatalf("empty path should not error: %v", err)
	}
	s.set("System", "x")
	if err := s.persist(); err != nil {
		t.Errorf("persist with empty path should be a no-op, got %v", err)
	}
}

func TestBookmarkStore_CorruptFileReportsButRecovers(t *testing.T) {
	path := filepath.Join(t.TempDir(), "bookmark.json")
	if err := os.WriteFile(path, []byte("{not valid json"), 0o644); err != nil {
		t.Fatal(err)
	}
	s, err := newBookmarkStore(path)
	if err == nil {
		t.Error("corrupt file should surface an error")
	}
	// ...but the store is still usable and overwrites the corruption.
	s.set("System", "recovered")
	if err := s.persist(); err != nil {
		t.Fatalf("persist after corruption: %v", err)
	}
	s2, err := newBookmarkStore(path)
	if err != nil {
		t.Fatalf("reload after recovery: %v", err)
	}
	if s2.get("System") != "recovered" {
		t.Errorf("recovery write not persisted, got %q", s2.get("System"))
	}
}
