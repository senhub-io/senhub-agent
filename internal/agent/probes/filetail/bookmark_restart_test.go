package filetail

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"senhub-agent.go/internal/agent/cliArgs"
	"senhub-agent.go/internal/agent/services/logger"
)

// backlog returns n log lines, long enough together to exceed the
// fingerprint window so the restart takes the fingerprint path.
func backlog(n int) string {
	var b strings.Builder
	for i := 0; i < n; i++ {
		fmt.Fprintf(&b, "2026-09-30 10:00:%02d INFO backlog line %03d of an unchanged log\n", i%60, i)
	}
	return b.String()
}

func startFileTail(t *testing.T, cfg map[string]interface{}) *FileTailProbe {
	t.Helper()
	probe, err := NewFileTailProbe(cfg, logger.NewLogger(&cliArgs.ParsedArgs{Env: "test"}))
	if err != nil {
		t.Fatalf("NewFileTailProbe: %v", err)
	}
	p := probe.(*FileTailProbe)
	if err := p.OnStart(make(chan struct{})); err != nil {
		t.Fatalf("OnStart: %v", err)
	}
	return p
}

func stopFileTail(t *testing.T, p *FileTailProbe) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := p.OnShutdown(ctx); err != nil {
		t.Fatalf("OnShutdown: %v", err)
	}
}

func waitEmitted(p *FileTailProbe, want uint64, within time.Duration) uint64 {
	deadline := time.Now().Add(within)
	for p.emitted.Load() < want && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}
	return p.emitted.Load()
}

func appendLines(t *testing.T, path string, lines ...string) {
	t.Helper()
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	for _, l := range lines {
		if _, err := fmt.Fprintln(f, l); err != nil {
			t.Fatal(err)
		}
	}
}

// A watched file that produced no line during a run must resume where it
// was on the next start, not be read again from its first byte.
func TestFileTail_RestartDoesNotReplayAnIdleFile(t *testing.T) {
	dir := t.TempDir()
	idle := filepath.Join(dir, "WebServer.log")
	if err := os.WriteFile(idle, []byte(backlog(40)), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg := map[string]interface{}{
		"paths":         []interface{}{idle},
		"bookmark_path": filepath.Join(dir, "bookmark.json"),
	}

	first := startFileTail(t, cfg)
	// Read from disk, as a crash before any line would leave it.
	onDisk, err := newBookmark(cfg["bookmark_path"].(string))
	if err != nil {
		t.Fatal(err)
	}
	stored, ok := onDisk.Get(idle)
	if !ok {
		t.Fatal("no bookmark entry on disk for a file whose tail has started")
	}
	if fi, _ := os.Stat(idle); stored.Offset != fi.Size() {
		t.Fatalf("bookmark offset = %d, want end of file %d", stored.Offset, fi.Size())
	}
	time.Sleep(300 * time.Millisecond)
	stopFileTail(t, first)
	if got := first.emitted.Load(); got != 0 {
		t.Fatalf("first run emitted %d records from an idle file tailed from the end", got)
	}

	second := startFileTail(t, cfg)
	defer stopFileTail(t, second)
	if got := waitEmitted(second, 1, time.Second); got != 0 {
		t.Fatalf("restart replayed %d records of an unchanged file", got)
	}

	appendLines(t, idle, "2026-09-30 11:00:00 INFO after restart")
	if got := waitEmitted(second, 1, 5*time.Second); got != 1 {
		t.Fatalf("line appended after restart: %d emitted, want 1", got)
	}
}

// With from_beginning, a file seen for the first time is still read from
// its first byte, once: the restart that follows reads nothing again.
func TestFileTail_FromBeginningReadsANewFileOnceAcrossRestarts(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "Core.log")
	if err := os.WriteFile(file, []byte(backlog(40)), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg := map[string]interface{}{
		"paths":          []interface{}{file},
		"bookmark_path":  filepath.Join(dir, "bookmark.json"),
		"from_beginning": true,
	}

	first := startFileTail(t, cfg)
	if got := waitEmitted(first, 40, 5*time.Second); got != 40 {
		t.Fatalf("first sight with from_beginning: %d emitted, want 40", got)
	}
	stopFileTail(t, first)

	second := startFileTail(t, cfg)
	defer stopFileTail(t, second)
	if got := waitEmitted(second, 1, time.Second); got != 0 {
		t.Fatalf("restart replayed %d records already read", got)
	}
}

// Lines read just before a stop, inside the periodic flush window, are
// accounted by the final flush: a clean restart does not read them again.
func TestFileTail_RestartDoesNotReplayLinesReadJustBeforeStop(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "Probe.log")
	if err := os.WriteFile(file, []byte(backlog(40)), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg := map[string]interface{}{
		"paths":         []interface{}{file},
		"bookmark_path": filepath.Join(dir, "bookmark.json"),
	}

	first := startFileTail(t, cfg)
	appendLines(t, file, "a", "b", "c", "d", "e", "f", "g")
	if got := waitEmitted(first, 7, 5*time.Second); got != 7 {
		t.Fatalf("appended lines: %d emitted, want 7", got)
	}
	stopFileTail(t, first)

	stored, _ := first.bookmarks.Get(file)
	if fi, _ := os.Stat(file); stored.Offset != fi.Size() {
		t.Fatalf("bookmark after stop = %d, want end of file %d", stored.Offset, fi.Size())
	}

	second := startFileTail(t, cfg)
	defer stopFileTail(t, second)
	if got := waitEmitted(second, 1, time.Second); got != 0 {
		t.Fatalf("restart replayed %d records read before the stop", got)
	}
}

// A burst of lines within one flush interval left the bookmark at the
// first of them until another line arrived: on the Windows lab, five
// lines sent at once kept the bookmark one line in for minutes, and a
// crash then would have replayed four records already sent.
func TestFileTail_BookmarkCatchesUpWithoutAnotherLine(t *testing.T) {
	dir := t.TempDir()
	busy := filepath.Join(dir, "busy.log")
	if err := os.WriteFile(busy, []byte(backlog(40)), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg := map[string]interface{}{
		"paths":         []interface{}{busy},
		"bookmark_path": filepath.Join(dir, "bookmark.json"),
	}
	p := startFileTail(t, cfg)
	defer stopFileTail(t, p)

	appendLines(t, busy, "2026-09-30 10:01:00 INFO one", "2026-09-30 10:01:00 INFO two", "2026-09-30 10:01:00 INFO three", "2026-09-30 10:01:00 INFO four", "2026-09-30 10:01:00 INFO five")
	if got := waitEmitted(p, 5, 5*time.Second); got != 5 {
		t.Fatalf("emitted %d, want 5", got)
	}
	fi, _ := os.Stat(busy)
	deadline := time.Now().Add(3 * bookmarkFlushInterval)
	for {
		onDisk, err := newBookmark(cfg["bookmark_path"].(string))
		if err != nil {
			t.Fatal(err)
		}
		if stored, ok := onDisk.Get(busy); ok && stored.Offset == fi.Size() {
			return
		} else if time.Now().After(deadline) {
			t.Fatalf("bookmark offset = %d after %v with no new line, want end of file %d", stored.Offset, 3*bookmarkFlushInterval, fi.Size())
		}
		time.Sleep(100 * time.Millisecond)
	}
}
