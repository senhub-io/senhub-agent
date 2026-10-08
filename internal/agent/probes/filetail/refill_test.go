package filetail

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// A file emptied and refilled beyond the offset already read is the same
// inode and longer than the offset: only its first bytes tell it apart from
// an append.
func TestDivergence_RefilledFileIsToldFromAnAppend(t *testing.T) {
	file := filepath.Join(t.TempDir(), "app.log")
	writeFile(t, file, backlog(40))
	info, err := os.Stat(file)
	if err != nil {
		t.Fatal(err)
	}
	ts := &tailState{opened: info, headFP: fingerprint(file, DefaultFingerprintLength)}
	ts.offset.Store(info.Size() / 2)
	ts.checkedOffset = -1

	appendLines(t, file, newFileLines(3)...)
	if reason, _ := ts.divergence(file); reason != "" {
		t.Fatalf("an append was reported as %q", reason)
	}

	refilled := strings.ReplaceAll(backlog(80), "backlog", "refilled")
	if err := os.WriteFile(file, []byte(refilled), 0o644); err != nil {
		t.Fatal(err)
	}
	reason, resume := ts.divergence(file)
	if reason == "" || resume {
		t.Fatalf("a refilled file gave reason %q resume %v, want a restart from the start", reason, resume)
	}
}
