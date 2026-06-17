package filetail

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestFingerprint_UnstableUntilWindowFilled(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "app.log")

	// Below the window: no stable fingerprint.
	writeFile(t, p, "short line\n")
	if fp := fingerprint(p, 64); fp != "" {
		t.Errorf("file below window should have empty fingerprint, got %q", fp)
	}

	// At/above the window: stable fingerprint over the first n bytes.
	writeFile(t, p, strings.Repeat("a", 64))
	fp1 := fingerprint(p, 64)
	if fp1 == "" {
		t.Fatal("file at window size should have a fingerprint")
	}
	// Growing the file must NOT change the first-n-bytes fingerprint —
	// this is the property whose absence caused restart duplication.
	writeFile(t, p, strings.Repeat("a", 64)+"newly appended content")
	fp2 := fingerprint(p, 64)
	if fp1 != fp2 {
		t.Errorf("fingerprint changed after append: %q -> %q", fp1, fp2)
	}
}

func TestResolveStartOffset_SmallGrowingFileResumesNoDuplicate(t *testing.T) {
	// Regression for the rotation+restart duplication: a small file
	// (below the fingerprint window, so fingerprint == "") that grew
	// while the agent was down must RESUME at the stored offset, not
	// restart from 0.
	stored := bookmarkEntry{Offset: 60, Fingerprint: ""}
	got := resolveStartOffset(stored, true, "", 125, false)
	if got != 60 {
		t.Errorf("small grown file: got offset %d, want 60 (resume)", got)
	}
}

func TestResolveStartOffset_StableFingerprint(t *testing.T) {
	// Same stable fingerprint -> resume.
	if got := resolveStartOffset(bookmarkEntry{Offset: 4096, Fingerprint: "abc12345"}, true, "abc12345", 8192, false); got != 4096 {
		t.Errorf("matching fingerprint: got %d, want 4096", got)
	}
	// Different stable fingerprint -> rotated/replaced -> 0.
	if got := resolveStartOffset(bookmarkEntry{Offset: 4096, Fingerprint: "abc12345"}, true, "deadbeef", 8192, false); got != 0 {
		t.Errorf("differing fingerprint: got %d, want 0", got)
	}
}

func TestResolveStartOffset_TruncatedSmallFile(t *testing.T) {
	// Offset past current EOF (copytruncate on a small file) -> restart.
	if got := resolveStartOffset(bookmarkEntry{Offset: 500, Fingerprint: ""}, true, "", 20, false); got != 0 {
		t.Errorf("truncated file: got %d, want 0", got)
	}
}

func TestResolveStartOffset_NoBookmark(t *testing.T) {
	if got := resolveStartOffset(bookmarkEntry{}, false, "", 100, true); got != 0 {
		t.Errorf("no bookmark + from_beginning: got %d, want 0", got)
	}
	if got := resolveStartOffset(bookmarkEntry{}, false, "", 100, false); got != -1 {
		t.Errorf("no bookmark + tail: got %d, want -1 (seek EOF)", got)
	}
}
