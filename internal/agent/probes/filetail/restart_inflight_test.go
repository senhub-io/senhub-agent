package filetail

import (
	"fmt"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/nxadm/tail"

	"senhub-agent.go/internal/agent/cliArgs"
	"senhub-agent.go/internal/agent/services/agentstate"
	"senhub-agent.go/internal/agent/services/logger"
)

// signalingBookmarks reports every bookmark written once armed.
type signalingBookmarks struct {
	bookmarkStore
	armed atomic.Bool
	set   chan struct{}
}

func (s *signalingBookmarks) Set(file string, e bookmarkEntry) error {
	err := s.bookmarkStore.Set(file, e)
	if s.armed.Load() {
		select {
		case s.set <- struct{}{}:
		default:
		}
	}
	return err
}

// A killed tail can still hand over a line it had read. The restart must
// take the offset after that line, or the new tail reads it a second time.
// The old tail is a fake that delivers its in-flight line only once the
// restart has written its bookmark, or after a short wait if it does not.
func TestFileTail_RestartDoesNotReplayLinesInFlightAtKill(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "app.log")
	const total, inFlight = 50, 30
	lines := make([]string, total)
	ends := make([]int64, total)
	var content strings.Builder
	for i := range lines {
		lines[i] = fmt.Sprintf("2026-10-29 09:00:%02d INFO line %03d of a slow log", i%60, i)
		content.WriteString(lines[i] + "\n")
		ends[i] = int64(content.Len())
	}
	writeFile(t, file, content.String())

	records := agentstate.SubscribeLogs(4096)
	defer agentstate.UnsubscribeLogs(records)

	bookmarkPath := filepath.Join(dir, "bookmark.json")
	probe, err := NewFileTailProbe(map[string]interface{}{
		"paths":         []interface{}{file},
		"bookmark_path": bookmarkPath,
	}, logger.NewLogger(&cliArgs.ParsedArgs{Env: "test"}))
	if err != nil {
		t.Fatal(err)
	}
	p := probe.(*FileTailProbe)
	p.SetName("inflight")
	defer stopFileTail(t, p)
	bm, err := newBookmark(bookmarkPath)
	if err != nil {
		t.Fatal(err)
	}
	rec := &signalingBookmarks{bookmarkStore: bm, set: make(chan struct{}, 16)}
	p.bookmarks = rec

	ft := &tail.Tail{Filename: file, Lines: make(chan *tail.Line)}
	fi, err := statIdentity(file)
	if err != nil {
		t.Fatal(err)
	}
	fp := fingerprint(file, DefaultFingerprintLength)
	ts := &tailState{t: ft, opened: fi, headFP: fp, done: make(chan struct{})}
	p.mu.Lock()
	p.tailing[file] = ts
	p.wg.Add(1)
	p.mu.Unlock()
	go p.consume(file, ts, 0, fp, make(chan reopenEvent))

	send := func(i int) {
		ft.Lines <- &tail.Line{Text: lines[i], SeekInfo: tail.SeekInfo{Offset: ends[i]}, Time: time.Now()}
	}
	delivered := make(chan struct{})
	go func() {
		for i := 0; i < inFlight; i++ {
			send(i)
		}
		close(delivered)
		<-ft.Dying()
		select {
		case <-rec.set:
		case <-time.After(300 * time.Millisecond):
		}
		send(inFlight)
		close(ft.Lines)
		ft.Done()
	}()

	<-delivered
	deadline := time.Now().Add(5 * time.Second)
	for ts.offset.Load() < ends[inFlight-1] {
		if time.Now().After(deadline) {
			t.Fatalf("offset stuck at %d, want %d", ts.offset.Load(), ends[inFlight-1])
		}
		time.Sleep(10 * time.Millisecond)
	}
	rec.armed.Store(true)
	p.restartStalledTail(file, ts, "test", true)

	seen := map[string]int{}
	timeout := time.After(15 * time.Second)
	for len(seen) < total {
		select {
		case r := <-records:
			if r.Attributes["log.file.path"] == file {
				seen[r.Body]++
			}
		case <-timeout:
			t.Fatalf("read %d of %d distinct lines", len(seen), total)
		}
	}
	quiet := time.After(time.Second)
drain:
	for {
		select {
		case r := <-records:
			if r.Attributes["log.file.path"] == file {
				seen[r.Body]++
			}
		case <-quiet:
			break drain
		}
	}
	for _, l := range lines {
		if seen[l] != 1 {
			t.Errorf("%q published %d times, want once", l, seen[l])
		}
	}
}

// A restart that gives up waiting for the replaced reader resumes before
// the record that reader still holds, not after the lines it has read: the
// record is then read again instead of being skipped.
func TestFileTail_RestartAfterTimeoutResumesBeforeAHeldRecord(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "core.log")
	lines := []string{
		"2026-10-29 09:20:00 ERROR record A",
		"   at Core.Run()",
		"2026-10-29 09:20:01 ERROR record B",
		"   at Core.Retry()",
	}
	ends := make([]int64, len(lines))
	var content strings.Builder
	for i, l := range lines {
		content.WriteString(l + "\n")
		ends[i] = int64(content.Len())
	}
	writeFile(t, file, content.String())

	saved := consumeExitTimeout
	consumeExitTimeout = 200 * time.Millisecond
	defer func() { consumeExitTimeout = saved }()

	bookmarkPath := filepath.Join(dir, "bookmark.json")
	probe, err := NewFileTailProbe(map[string]interface{}{
		"paths":         []interface{}{file},
		"bookmark_path": bookmarkPath,
		"multiline": map[string]interface{}{
			"pattern":       `^\d{4}-\d{2}-\d{2} `,
			"flush_timeout": "0",
		},
	}, logger.NewLogger(&cliArgs.ParsedArgs{Env: "test"}))
	if err != nil {
		t.Fatal(err)
	}
	p := probe.(*FileTailProbe)
	bm, err := newBookmark(bookmarkPath)
	if err != nil {
		t.Fatal(err)
	}
	p.bookmarks = bm

	ft := &tail.Tail{Filename: file, Lines: make(chan *tail.Line)}
	release := make(chan struct{})
	defer func() {
		close(release)
		stopFileTail(t, p)
	}()
	fi, err := statIdentity(file)
	if err != nil {
		t.Fatal(err)
	}
	ts := &tailState{t: ft, opened: fi, done: make(chan struct{})}
	p.mu.Lock()
	p.tailing[file] = ts
	p.wg.Add(1)
	p.mu.Unlock()
	go p.consume(file, ts, 0, "", make(chan reopenEvent))
	go func() {
		<-release
		close(ft.Lines)
		ft.Done()
	}()
	for i := range lines {
		ft.Lines <- &tail.Line{Text: lines[i], SeekInfo: tail.SeekInfo{Offset: ends[i]}, Time: time.Now()}
	}
	deadline := time.Now().Add(5 * time.Second)
	for ts.offset.Load() != ends[3] || ts.committed.Load() != ends[1] {
		if time.Now().After(deadline) {
			t.Fatalf("offset %d committed %d, want %d and %d", ts.offset.Load(), ts.committed.Load(), ends[3], ends[1])
		}
		time.Sleep(10 * time.Millisecond)
	}

	p.restartStalledTail(file, ts, "test", true)
	e, ok := bm.Get(file)
	if !ok {
		t.Fatal("no bookmark after the restart")
	}
	if e.Offset != ends[1] && e.Offset != 0 {
		t.Fatalf("restart bookmarked offset %d, past the held record that starts at %d", e.Offset, ends[1])
	}
}
