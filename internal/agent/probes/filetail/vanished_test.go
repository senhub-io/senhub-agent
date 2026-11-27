package filetail

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestFileTail_DeletedGlobFileIsDropped(t *testing.T) {
	dir := t.TempDir()
	first := filepath.Join(dir, "app-1.log")
	writeFile(t, first, "")
	bookmark := filepath.Join(t.TempDir(), "bookmark.json")
	p := startFileTail(t, map[string]interface{}{
		"paths":         []interface{}{filepath.Join(dir, "*.log")},
		"bookmark_path": bookmark,
	})
	defer stopFileTail(t, p)
	p.mu.Lock()
	p.stallGrace = 100 * time.Millisecond
	p.mu.Unlock()
	awaitTailWatching(t, p, first)

	if err := os.Remove(first); err != nil {
		t.Fatal(err)
	}
	second := filepath.Join(dir, "app-2.log")
	writeFile(t, second, "")

	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		p.scanAndTail()
		p.mu.Lock()
		_, ok := p.tailing[first]
		p.mu.Unlock()
		if !ok {
			break
		}
		time.Sleep(150 * time.Millisecond)
	}

	p.mu.Lock()
	_, firstOK := p.tailing[first]
	_, secondOK := p.tailing[second]
	p.mu.Unlock()
	if firstOK {
		t.Fatal("deleted glob-matched file still has a tail")
	}
	if !secondOK {
		t.Fatal("new glob-matched file has no tail")
	}
	if _, ok := p.fileEmitted.Load(first); ok {
		t.Fatal("deleted file still in fileEmitted")
	}
}

func TestFileTail_DeletedLiteralFileKeepsItsTail(t *testing.T) {
	dir := t.TempDir()
	first := filepath.Join(dir, "app-1.log")
	writeFile(t, first, "")
	bookmark := filepath.Join(t.TempDir(), "bookmark.json")
	p := startFileTail(t, map[string]interface{}{
		"paths":         []interface{}{first},
		"bookmark_path": bookmark,
	})
	defer stopFileTail(t, p)
	p.mu.Lock()
	p.stallGrace = 100 * time.Millisecond
	p.mu.Unlock()
	awaitTailWatching(t, p, first)

	if err := os.Remove(first); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 8; i++ {
		p.scanAndTail()
		time.Sleep(150 * time.Millisecond)
	}

	p.mu.Lock()
	_, ok := p.tailing[first]
	p.mu.Unlock()
	if !ok {
		t.Fatal("deleted literal path lost its tail")
	}
}
