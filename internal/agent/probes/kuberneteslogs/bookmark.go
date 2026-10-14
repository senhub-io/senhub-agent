package kuberneteslogs

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

const bookmarkFlushInterval = 5 * time.Second

type bookmarkEntry struct {
	Inode  uint64 `json:"inode"`
	Offset int64  `json:"offset"`
}

type bookmarkStore interface {
	Get(path string) (bookmarkEntry, bool)
	Set(path string, entry bookmarkEntry)
	Delete(path string)
	FlushIfDue(now time.Time) error
	Flush() error
}

type bookmark struct {
	path      string
	mu        sync.Mutex
	entries   map[string]bookmarkEntry
	dirty     bool
	lastFlush time.Time
}

type noopBookmark struct{}

func (noopBookmark) Get(string) (bookmarkEntry, bool) { return bookmarkEntry{}, false }
func (noopBookmark) Set(string, bookmarkEntry)        {}
func (noopBookmark) Delete(string)                    {}
func (noopBookmark) FlushIfDue(time.Time) error       { return nil }
func (noopBookmark) Flush() error                     { return nil }

func newBookmark(path string) (bookmarkStore, error) {
	if path == "" {
		return noopBookmark{}, nil
	}

	b := &bookmark{
		path:    path,
		entries: map[string]bookmarkEntry{},
	}

	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return b, nil
		}
		return nil, fmt.Errorf("kubernetes_logs: reading bookmark %s: %w", path, err)
	}
	if len(data) == 0 {
		return b, nil
	}
	if err := json.Unmarshal(data, &b.entries); err != nil {
		return nil, fmt.Errorf("kubernetes_logs: parsing bookmark %s: %w", path, err)
	}
	return b, nil
}

func (b *bookmark) Get(path string) (bookmarkEntry, bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	e, ok := b.entries[path]
	return e, ok
}

func (b *bookmark) Set(path string, entry bookmarkEntry) {
	b.mu.Lock()
	b.entries[path] = entry
	b.dirty = true
	b.mu.Unlock()
}

func (b *bookmark) Delete(path string) {
	b.mu.Lock()
	if _, ok := b.entries[path]; ok {
		delete(b.entries, path)
		b.dirty = true
	}
	b.mu.Unlock()
}

func (b *bookmark) FlushIfDue(now time.Time) error {
	b.mu.Lock()
	if !b.dirty || now.Sub(b.lastFlush) < bookmarkFlushInterval {
		b.mu.Unlock()
		return nil
	}
	b.mu.Unlock()
	return b.flush()
}

func (b *bookmark) Flush() error {
	return b.flush()
}

func (b *bookmark) flush() error {
	b.mu.Lock()
	snapshot := make(map[string]bookmarkEntry, len(b.entries))
	for k, v := range b.entries {
		snapshot[k] = v
	}
	b.mu.Unlock()

	data, err := json.Marshal(snapshot)
	if err != nil {
		return fmt.Errorf("kubernetes_logs: marshaling bookmark: %w", err)
	}

	dir := filepath.Dir(b.path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("kubernetes_logs: creating bookmark dir %s: %w", dir, err)
	}

	tmp, err := os.CreateTemp(dir, ".kubernetes-logs-bookmark-*.tmp")
	if err != nil {
		return fmt.Errorf("kubernetes_logs: creating temp bookmark: %w", err)
	}
	tmpName := tmp.Name()
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		os.Remove(tmpName)
		return fmt.Errorf("kubernetes_logs: writing temp bookmark: %w", err)
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpName)
		return fmt.Errorf("kubernetes_logs: closing temp bookmark: %w", err)
	}
	if err := os.Rename(tmpName, b.path); err != nil {
		os.Remove(tmpName)
		return fmt.Errorf("kubernetes_logs: renaming bookmark into place: %w", err)
	}

	b.mu.Lock()
	b.dirty = false
	b.lastFlush = time.Now()
	b.mu.Unlock()

	return nil
}
