package filetail

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

// A file that leaves the followed set takes its emitted counter with it: a
// glob over dated directories otherwise keeps one counter per directory
// ever seen.
func TestFileTail_EmittedCounterLeavesWithItsFile(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the polling watcher waits for a removed file to reappear and never ends the tail")
	}
	root := t.TempDir()
	first := filepath.Join(root, "2026-10-28", "app.log")
	second := filepath.Join(root, "2026-10-29", "app.log")
	for _, f := range []string{first, second} {
		if err := os.MkdirAll(filepath.Dir(f), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	writeFile(t, first, "")
	p := startFileTail(t, map[string]interface{}{
		"paths":         []interface{}{filepath.Join(root, "*", "app.log")},
		"bookmark_path": filepath.Join(t.TempDir(), "bookmark.json"),
	})
	defer stopFileTail(t, p)
	awaitTailWatching(t, p, first)
	if _, ok := p.fileEmitted.Load(first); !ok {
		t.Fatal("no counter for the file being read")
	}

	if err := os.RemoveAll(filepath.Dir(first)); err != nil {
		t.Fatal(err)
	}
	writeFile(t, second, "")
	deadline := time.Now().Add(15 * time.Second)
	for {
		p.mu.Lock()
		_, still := p.tailing[first]
		p.mu.Unlock()
		if !still {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("the tail on the removed file never ended")
		}
		time.Sleep(50 * time.Millisecond)
	}
	p.scanAndTail()
	awaitTailWatching(t, p, second)
	if _, err := p.Collect(); err != nil {
		t.Fatalf("Collect: %v", err)
	}

	var kept []string
	p.fileEmitted.Range(func(k, _ any) bool {
		kept = append(kept, k.(string))
		return true
	})
	if len(kept) != 1 || kept[0] != second {
		t.Fatalf("counters kept for %q, want only %s", kept, second)
	}
}
