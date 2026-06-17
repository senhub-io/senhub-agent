package filetail

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
)

// bookmark persists per-file read offsets so an agent restart resumes a
// tail without loss or duplication. The on-disk shape is a flat JSON
// map of absolute file path -> byte offset. A small per-file fingerprint
// (the leading bytes' checksum) guards against the "file truncated /
// replaced, old offset now points past EOF" infinite-loop hazard called
// out in the issue: when the fingerprint no longer matches, the offset
// is treated as stale and reset to 0.
//
// The store is safe for concurrent use by the per-file tail goroutines
// of a single probe instance. Multiple probe instances MUST use
// distinct BookmarkPath values (enforced by config, documented in the
// README) so they do not stomp each other's offsets.
type bookmark struct {
	path string

	mu      sync.Mutex
	offsets map[string]bookmarkEntry
}

type bookmarkEntry struct {
	Offset      int64  `json:"offset"`
	Fingerprint string `json:"fingerprint,omitempty"`
}

// noopBookmark is returned when BookmarkPath is empty: offset tracking
// is disabled and every method is a cheap no-op.
type bookmarkStore interface {
	Get(file string) (bookmarkEntry, bool)
	Set(file string, entry bookmarkEntry) error
}

type noopBookmark struct{}

func (noopBookmark) Get(string) (bookmarkEntry, bool) { return bookmarkEntry{}, false }
func (noopBookmark) Set(string, bookmarkEntry) error  { return nil }

// newBookmark loads an existing bookmark file (if present) or starts an
// empty store. A missing file is not an error — first run. A corrupt
// file is reported so the operator notices, but the store still starts
// empty rather than refusing to run.
func newBookmark(path string) (bookmarkStore, error) {
	if path == "" {
		return noopBookmark{}, nil
	}
	b := &bookmark{path: path, offsets: map[string]bookmarkEntry{}}

	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return b, nil
		}
		return nil, fmt.Errorf("filetail: reading bookmark %s: %w", path, err)
	}
	if len(data) == 0 {
		return b, nil
	}
	if err := json.Unmarshal(data, &b.offsets); err != nil {
		return nil, fmt.Errorf("filetail: parsing bookmark %s: %w", path, err)
	}
	return b, nil
}

func (b *bookmark) Get(file string) (bookmarkEntry, bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	e, ok := b.offsets[file]
	return e, ok
}

// Set records the offset and atomically rewrites the bookmark file
// (write-temp-then-rename) so a crash mid-write cannot corrupt it.
func (b *bookmark) Set(file string, entry bookmarkEntry) error {
	b.mu.Lock()
	b.offsets[file] = entry
	snapshot := make(map[string]bookmarkEntry, len(b.offsets))
	for k, v := range b.offsets {
		snapshot[k] = v
	}
	b.mu.Unlock()

	data, err := json.Marshal(snapshot)
	if err != nil {
		return fmt.Errorf("filetail: marshaling bookmark: %w", err)
	}

	dir := filepath.Dir(b.path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("filetail: creating bookmark dir %s: %w", dir, err)
	}
	tmp, err := os.CreateTemp(dir, ".bookmark-*.tmp")
	if err != nil {
		return fmt.Errorf("filetail: creating temp bookmark: %w", err)
	}
	tmpName := tmp.Name()
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		os.Remove(tmpName)
		return fmt.Errorf("filetail: writing temp bookmark: %w", err)
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpName)
		return fmt.Errorf("filetail: closing temp bookmark: %w", err)
	}
	if err := os.Rename(tmpName, b.path); err != nil {
		os.Remove(tmpName)
		return fmt.Errorf("filetail: renaming bookmark into place: %w", err)
	}
	return nil
}
