package windowseventlog

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
)

// bookmarkFile maps an Event Log channel name to the opaque wevtapi
// bookmark XML for that channel. EvtSubscribe takes one bookmark per
// channel, so a probe watching N channels carries N bookmarks; we
// persist them together in a single JSON file (the operator-supplied
// bookmark_path) keyed by channel.
//
// The value is the verbatim XML produced by EvtRender(EvtRenderBookmark)
// — we never parse it, only round-trip it back into EvtCreateBookmark on
// the next start. JSON is just the envelope; it keeps a multi-channel
// probe to one file and survives partial channel reconfiguration (a
// removed channel's stale bookmark is simply ignored).
type bookmarkFile map[string]string

// bookmarkStore guards concurrent bookmark updates from the per-channel
// reader goroutines and serialises the atomic file write. It is the
// OS-agnostic half of the bookmark feature, unit-tested without a
// Windows host; the wevtapi rendering that produces the XML values lives
// in subscription_windows.go.
type bookmarkStore struct {
	path string

	mu        sync.Mutex
	bookmarks bookmarkFile
}

// newBookmarkStore loads any persisted bookmarks from path. A missing
// file is not an error — it yields an empty store (first run). A present
// but unreadable/corrupt file IS reported so the operator learns their
// bookmark state was lost rather than silently re-reading from scratch.
// An empty path disables persistence: get always misses and persist is a
// no-op, so the probe tails from now on each start.
func newBookmarkStore(path string) (*bookmarkStore, error) {
	s := &bookmarkStore{path: path, bookmarks: bookmarkFile{}}
	if path == "" {
		return s, nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return s, nil
		}
		return s, fmt.Errorf("read bookmark file %s: %w", path, err)
	}
	if len(data) == 0 {
		return s, nil
	}
	if err := json.Unmarshal(data, &s.bookmarks); err != nil {
		// Reset to empty so the corrupt content is replaced on the next
		// persist rather than failing every write.
		s.bookmarks = bookmarkFile{}
		return s, fmt.Errorf("parse bookmark file %s: %w", path, err)
	}
	return s, nil
}

// get returns the persisted bookmark XML for a channel, or "" when none
// exists (first run for that channel, or persistence disabled).
func (s *bookmarkStore) get(channel string) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.bookmarks[channel]
}

// set records the latest bookmark XML for a channel in memory. Call
// persist to flush it to disk.
func (s *bookmarkStore) set(channel, xml string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.bookmarks[channel] = xml
}

// persist atomically writes the current bookmark set to disk. No-op when
// the path is empty (persistence disabled). The write is temp-file +
// rename so a crash mid-write never leaves a truncated bookmark file
// that would lose progress for every channel.
func (s *bookmarkStore) persist() error {
	if s.path == "" {
		return nil
	}
	s.mu.Lock()
	data, err := json.Marshal(s.bookmarks)
	s.mu.Unlock()
	if err != nil {
		return fmt.Errorf("marshal bookmarks: %w", err)
	}

	dir := filepath.Dir(s.path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create bookmark dir %s: %w", dir, err)
	}

	tmp, err := os.CreateTemp(dir, ".bookmark-*.tmp")
	if err != nil {
		return fmt.Errorf("create temp bookmark file in %s: %w", dir, err)
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName) // no-op once the rename succeeds

	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("write temp bookmark file: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close temp bookmark file: %w", err)
	}
	if err := os.Rename(tmpName, s.path); err != nil {
		return fmt.Errorf("rename bookmark file into place: %w", err)
	}
	return nil
}
