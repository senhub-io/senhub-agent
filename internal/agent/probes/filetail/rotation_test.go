package filetail

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/nxadm/tail"
)

func TestResolveStartOffset_OffsetPastEndRestartsAtZeroWhateverTheFingerprint(t *testing.T) {
	cases := []struct {
		name   string
		stored bookmarkEntry
		fp     string
	}{
		{"matching fingerprint", bookmarkEntry{Offset: 1863515, Fingerprint: "deadbeef"}, "deadbeef"},
		{"different fingerprint", bookmarkEntry{Offset: 1863515, Fingerprint: "deadbeef"}, "cafef00d"},
		{"no stable fingerprint", bookmarkEntry{Offset: 1863515}, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := resolveStartOffset(tc.stored, true, tc.fp, 274000, false); got != 0 {
				t.Fatalf("offset = %d, want 0", got)
			}
		})
	}
	if got := resolveStartOffset(bookmarkEntry{Offset: 500, Fingerprint: "deadbeef"}, true, "deadbeef", 500, false); got != 500 {
		t.Fatalf("offset at exact EOF = %d, want 500 (resume)", got)
	}
}

// The bookmark an agent left after a rotation it did not see to the end:
// the old file's size as offset, the new file's fingerprint.
func TestFileTail_RestartWithOffsetPastEndAndMatchingFingerprintReadsFromZero(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "access.log")
	content := backlog(40)
	writeFile(t, file, content)
	bookmarkPath := filepath.Join(dir, "bookmark.json")

	store, err := newBookmark(bookmarkPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Set(file, bookmarkEntry{Offset: int64(len(content)) * 7, Fingerprint: fingerprint(file, DefaultFingerprintLength)}); err != nil {
		t.Fatal(err)
	}

	p := startFileTail(t, map[string]interface{}{
		"paths":         []interface{}{file},
		"bookmark_path": bookmarkPath,
	})
	defer stopFileTail(t, p)

	if got := waitEmitted(p, 40, 5*time.Second); got != 40 {
		t.Fatalf("emitted %d of the 40 lines of the new file", got)
	}
}

// A running probe whose file is rotated by rename-and-create keeps reading
// the new file, and the bookmark pairs the new file's offset with the new
// file's own fingerprint.
func TestFileTail_CreateModeRotationKeepsCollecting(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "access.log")
	writeFile(t, file, backlog(40))
	bookmarkPath := filepath.Join(dir, "bookmark.json")

	p := startFileTail(t, map[string]interface{}{
		"paths":         []interface{}{file},
		"bookmark_path": bookmarkPath,
	})
	defer stopFileTail(t, p)
	time.Sleep(300 * time.Millisecond)

	for round := 0; round < 2; round++ {
		time.Sleep(300 * time.Millisecond)
		before := p.emitted.Load()
		// Each round moves the file to a name of its own: Windows refuses a
		// rename onto a file another handle still has open, and the probe
		// keeps the rotated file open for a while to read its last lines.
		if err := os.Rename(file, fmt.Sprintf("%s.%d", file, round+1)); err != nil {
			t.Fatal(err)
		}
		writeFile(t, file, "")
		lines := newFileLines(60)
		appendLines(t, file, lines...)

		want := before + uint64(len(lines))
		if got := waitEmitted(p, want, 5*time.Second); got != want {
			t.Fatalf("round %d: emitted %d, want %d", round, got, want)
		}
		info, err := os.Stat(file)
		if err != nil {
			t.Fatal(err)
		}
		waitBookmarkOnDisk(t, bookmarkPath, file, func(e bookmarkEntry) bool {
			return e.Offset == info.Size() && e.Fingerprint == fingerprint(file, DefaultFingerprintLength)
		}, 5*time.Second)
	}
}

func TestFileTail_CopytruncateKeepsCollecting(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "access.log")
	writeFile(t, file, backlog(40))

	p := startFileTail(t, map[string]interface{}{
		"paths":         []interface{}{file},
		"bookmark_path": filepath.Join(dir, "bookmark.json"),
	})
	defer stopFileTail(t, p)
	time.Sleep(300 * time.Millisecond)

	for round := 0; round < 2; round++ {
		before := p.emitted.Load()
		if err := os.Truncate(file, 0); err != nil {
			t.Fatal(err)
		}
		// Shorter than what was read, as copytruncate leaves a file between
		// two rotations; the library sees a truncation only when the size
		// has dropped by the time it looks.
		lines := newFileLines(10 - 5*round)
		appendLines(t, file, lines...)
		want := before + uint64(len(lines))
		if got := waitEmitted(p, want, 5*time.Second); got != want {
			t.Fatalf("round %d: emitted %d, want %d", round, got, want)
		}
	}
}

// With no reopen event, an offset read in the old file must be saved with
// the old file's fingerprint, not the one of whatever the path names when
// the save happens.
func TestFileTail_PersistedFingerprintBelongsToTheFileTheOffsetWasReadFrom(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "access.log")
	old := backlog(40)
	writeFile(t, file, old)
	oldFP := fingerprint(file, DefaultFingerprintLength)
	if oldFP == "" {
		t.Fatal("test file too short for a stable fingerprint")
	}
	bookmarkPath := filepath.Join(dir, "bookmark.json")

	probe, err := NewFileTailProbe(map[string]interface{}{
		"paths":         []interface{}{file},
		"bookmark_path": bookmarkPath,
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	p := probe.(*FileTailProbe)
	p.bookmarks, err = newBookmark(bookmarkPath)
	if err != nil {
		t.Fatal(err)
	}

	// ReOpen is off, so the tail ends when the file is renamed and no
	// reopen is ever signalled: the situation of a tail that dies during a
	// rotation.
	tl, err := tail.TailFile(file, tail.Config{Follow: true, MustExist: true, CompleteLines: true})
	if err != nil {
		t.Fatal(err)
	}
	ts := &tailState{t: tl}
	p.tailing[file] = ts
	p.wg.Add(1)
	go p.consume(file, ts, 0, oldFP, make(chan reopenEvent))

	if got := waitEmitted(p, 40, 5*time.Second); got != 40 {
		t.Fatalf("emitted %d of 40 lines", got)
	}
	time.Sleep(200 * time.Millisecond)
	if err := os.Rename(file, file+".1"); err != nil {
		t.Fatal(err)
	}
	writeFile(t, file, strings.Join(newFileLines(60), "\n")+"\n")
	if fingerprint(file, DefaultFingerprintLength) == oldFP {
		t.Fatal("new file has the old fingerprint; the test proves nothing")
	}
	p.wg.Wait()

	got := waitBookmarkOnDisk(t, bookmarkPath, file, func(e bookmarkEntry) bool { return e.Offset == int64(len(old)) }, 2*time.Second)
	if got.Fingerprint != oldFP {
		t.Fatalf("persisted fingerprint = %q, want the old file's %q", got.Fingerprint, oldFP)
	}
	p.mu.Lock()
	_, stillRegistered := p.tailing[file]
	p.mu.Unlock()
	if stillRegistered {
		t.Fatal("dead tail still registered; the rescan would never restart it")
	}
}
