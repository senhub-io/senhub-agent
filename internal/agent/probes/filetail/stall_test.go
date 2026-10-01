package filetail

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func tailStateOf(t *testing.T, p *FileTailProbe, file string) *tailState {
	t.Helper()
	p.mu.Lock()
	defer p.mu.Unlock()
	ts := p.tailing[file]
	if ts == nil {
		t.Fatalf("no tail running on %s", file)
	}
	return ts
}

// stallProbe starts a probe on a file already holding a backlog, with a
// grace period short enough for a test. The first start tails from the end,
// so nothing has been emitted yet.
func stallProbe(t *testing.T, file string) (*FileTailProbe, *lockedBuffer) {
	t.Helper()
	p, logs := startLoggedFileTail(t, map[string]interface{}{
		"paths":         []interface{}{file},
		"bookmark_path": filepath.Join(filepath.Dir(file), "bookmark.json"),
	})
	p.mu.Lock()
	p.stallGrace = 100 * time.Millisecond
	p.mu.Unlock()
	return p, logs
}

func rescanUntilRestarted(t *testing.T, p *FileTailProbe, file string, old *tailState) *tailState {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		p.scanAndTail()
		p.mu.Lock()
		cur := p.tailing[file]
		p.mu.Unlock()
		if cur != nil && cur != old {
			return cur
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatal("the stalled tail was never restarted")
	return nil
}

// A tail still bound to the file that a rotation moved away, whose reopen
// never came: the path names another file than the one being read.
func TestFileTail_TailStuckOnRotatedFileIsReportedAndRestartedFromZero(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "access.log")
	other := filepath.Join(dir, "rotated.log")
	writeFile(t, file, backlog(40))
	writeFile(t, other, backlog(3))

	p, logs := stallProbe(t, file)
	defer stopFileTail(t, p)
	stuck := tailStateOf(t, p, file)

	otherInfo, err := os.Stat(other)
	if err != nil {
		t.Fatal(err)
	}
	stuck.mu.Lock()
	stuck.opened = otherInfo
	stuck.mu.Unlock()

	fresh := rescanUntilRestarted(t, p, file, stuck)
	if fresh == stuck {
		t.Fatal("same tail after the restart")
	}
	if _, err := p.Collect(); err == nil || !strings.Contains(err.Error(), file) || !strings.Contains(err.Error(), "restarted") {
		t.Fatalf("Collect error = %v, want the stall reported against %s", err, file)
	}
	if got := logs.count("tail does not follow its file"); got != 1 {
		t.Fatalf("stall logged %d times, want once", got)
	}

	// Restarted from the first byte: the 40 lines already there are read,
	// then the new ones.
	appendLines(t, file, newFileLines(5)...)
	if got := waitEmitted(p, 45, 5*time.Second); got != 45 {
		t.Fatalf("emitted %d of the 45 lines after the restart", got)
	}
}

// Same file, but shorter than what the tail has read: copytruncate whose
// reopen never came.
func TestFileTail_TailPastTheEndOfATruncatedFileIsReportedAndRestarted(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "access.log")
	writeFile(t, file, backlog(40))

	p, _ := stallProbe(t, file)
	defer stopFileTail(t, p)
	stuck := tailStateOf(t, p, file)

	if err := os.Truncate(file, 0); err != nil {
		t.Fatal(err)
	}
	lines := newFileLines(5)
	appendLines(t, file, lines...)
	// The tail normally follows a truncation itself. Once it has, pin the
	// state the self-check exists for: a read offset the file no longer
	// reaches, as when that reopen never comes.
	waitEmitted(p, uint64(len(lines)), 3*time.Second)
	stuck.offset.Store(1 << 20)

	fresh := rescanUntilRestarted(t, p, file, stuck)
	if fresh == stuck {
		t.Fatal("same tail after the restart")
	}
	if _, err := p.Collect(); err == nil || !strings.Contains(err.Error(), "restarted") {
		t.Fatalf("Collect error = %v, want the stall reported", err)
	}
	if got := waitEmitted(p, 5, 5*time.Second); got < 5 {
		t.Fatalf("emitted %d of the 5 lines of the truncated file", got)
	}
}

// A healthy tail is left alone: no restart, no error.
func TestFileTail_HealthyTailIsNeverRestarted(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "access.log")
	writeFile(t, file, backlog(40))

	p, _ := stallProbe(t, file)
	defer stopFileTail(t, p)
	first := tailStateOf(t, p, file)

	appendLines(t, file, newFileLines(20)...)
	waitEmitted(p, 20, 5*time.Second)
	for i := 0; i < 6; i++ {
		p.scanAndTail()
		time.Sleep(60 * time.Millisecond)
	}
	if tailStateOf(t, p, file) != first {
		t.Fatal("a healthy tail was restarted")
	}
	if _, err := p.Collect(); err != nil {
		t.Fatalf("Collect: %v", err)
	}
}

// Two probes on one path share the library's per-path event channel; on
// Linux that left both tails waiting on the old file after a rotation, with
// no error. The self-check brings them back.
func TestFileTail_TwoProbesOnOnePathRecoverFromARotation(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "access.log")
	writeFile(t, file, backlog(40))
	var probes []*FileTailProbe
	for _, name := range []string{"a", "b"} {
		p, _ := startLoggedFileTail(t, map[string]interface{}{
			"paths":         []interface{}{file},
			"bookmark_path": filepath.Join(dir, name+".json"),
		})
		p.mu.Lock()
		p.stallGrace = 100 * time.Millisecond
		p.mu.Unlock()
		defer stopFileTail(t, p)
		probes = append(probes, p)
	}
	time.Sleep(500 * time.Millisecond)

	if err := os.Rename(file, file+".1"); err != nil {
		t.Fatal(err)
	}
	writeFile(t, file, "")
	lines := newFileLines(30)
	appendLines(t, file, lines...)

	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		done := true
		for _, p := range probes {
			p.scanAndTail()
			if p.emitted.Load() < uint64(len(lines)) {
				done = false
			}
		}
		if done {
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatalf("emitted %d and %d of %d lines of the new file", probes[0].emitted.Load(), probes[1].emitted.Load(), len(lines))
}

// The right file, but a tail that reads nothing of what was appended: it
// restarts where it was, so nothing is replayed and nothing is lost.
func TestFileTail_TailThatReadsNothingOfAGrowingFileResumesAtItsOffset(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "access.log")
	content := backlog(40)
	writeFile(t, file, content)

	p, _ := stallProbe(t, file)
	defer stopFileTail(t, p)
	stuck := tailStateOf(t, p, file)

	lastThree := 0
	for _, l := range strings.SplitAfter(content, "\n")[37:40] {
		lastThree += len(l)
	}
	stuck.offset.Store(int64(len(content) - lastThree))

	fresh := rescanUntilRestarted(t, p, file, stuck)
	if fresh == stuck {
		t.Fatal("same tail after the restart")
	}
	if got := waitEmitted(p, 3, 5*time.Second); got != 3 {
		t.Fatalf("emitted %d, want the 3 lines past the offset", got)
	}
	time.Sleep(300 * time.Millisecond)
	if got := p.emitted.Load(); got != 3 {
		t.Fatalf("emitted %d, want exactly 3", got)
	}
}
