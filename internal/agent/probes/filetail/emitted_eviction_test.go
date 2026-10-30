package filetail

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// A file that leaves the followed set takes its emitted counter with it: a
// glob over dated directories otherwise keeps one counter per directory
// ever seen.
func TestFileTail_EmittedCounterLeavesWithItsFile(t *testing.T) {
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
	// Whether the tail library ends a tail whose directory was removed, and
	// when, depends on its watcher and the host's load. What is under test is
	// what the probe does once a tail ends, so the tail is ended here.
	p.mu.Lock()
	ts := p.tailing[first]
	p.mu.Unlock()
	if ts == nil {
		t.Fatal("no tail registered for the file being read")
	}
	ts.t.Kill(errors.New("file removed"))
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
