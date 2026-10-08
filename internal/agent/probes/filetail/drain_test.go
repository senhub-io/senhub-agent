package filetail

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"senhub-agent.go/internal/agent/cliArgs"
	"senhub-agent.go/internal/agent/services/agentstate"
	"senhub-agent.go/internal/agent/services/logger"
)

const drainReadiness = "2026-09-30 10:59:59 INFO tail readiness"

func TestDrainFromReadsCompleteLinesOnly(t *testing.T) {
	path := filepath.Join(t.TempDir(), "a.log")
	if err := os.WriteFile(path, []byte("one\ntwo\nthree\npartial"), 0o644); err != nil {
		t.Fatal(err)
	}
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	got, end, err := drainFrom(f, 4)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0] != "two" || got[1] != "three" {
		t.Fatalf("drained %q, want [two three]", got)
	}
	if end != 14 {
		t.Fatalf("end offset = %d, want 14 (before the partial line)", end)
	}
}

func startPollingFileTail(t *testing.T, file, bookmark string) *FileTailProbe {
	t.Helper()
	probe, err := NewFileTailProbe(map[string]interface{}{
		"paths":         []interface{}{file},
		"bookmark_path": bookmark,
	}, logger.NewLogger(&cliArgs.ParsedArgs{Env: "test"}))
	if err != nil {
		t.Fatal(err)
	}
	p := probe.(*FileTailProbe)
	p.polling[file] = true
	if err := p.OnStart(make(chan struct{})); err != nil {
		t.Fatal(err)
	}
	return p
}

func collectBodies(t *testing.T, records <-chan agentstate.LogRecord, file string, n int) []string {
	t.Helper()
	var got []string
	deadline := time.After(10 * time.Second)
	for len(got) < n {
		select {
		case r := <-records:
			if r.Attributes["log.file.path"] == file && r.Body != drainReadiness {
				got = append(got, r.Body)
			}
		case <-deadline:
			t.Fatalf("read %d of %d lines: %q", len(got), n, got)
		}
	}
	select {
	case r := <-records:
		if r.Body != drainReadiness {
			t.Fatalf("extra record after the expected lines: %q (all: %q)", r.Body, got)
		}
	case <-time.After(700 * time.Millisecond):
	}
	return got
}

// The writer appends to the old inode after the rename and before the tail
// looks again: those lines are read once, ahead of the new file's.
func TestFileTail_RotationDrainsLinesWrittenToTheOldFile(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the rotated file is not drained on Windows: holding it would refuse the application's own rotation")
	}
	dir := t.TempDir()
	file := filepath.Join(dir, "app.log")
	writeFile(t, file, "")

	records := agentstate.SubscribeLogs(4096)
	defer agentstate.UnsubscribeLogs(records)

	p := startPollingFileTail(t, file, filepath.Join(dir, "bookmark.json"))
	defer stopFileTail(t, p)
	awaitTailWatching(t, p, file)

	var want []string
	seq := 0
	next := func(tag string) string {
		seq++
		line := fmt.Sprintf("%s-%03d", tag, seq)
		want = append(want, line)
		return line
	}
	for round := 0; round < 4; round++ {
		appendLines(t, file, next("before"))
		time.Sleep(400 * time.Millisecond)
		old := fmt.Sprintf("%s.%d", file, round)
		if err := os.Rename(file, old); err != nil {
			t.Fatal(err)
		}
		appendLines(t, old, next("old"), next("old"))
		writeFile(t, file, "")
		appendLines(t, file, next("new"), next("new"))
		time.Sleep(600 * time.Millisecond)
	}

	got := collectBodies(t, records, file, len(want))
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("line %d = %q, want %q (all: %q)", i, got[i], want[i], got)
		}
	}
}

// A writer that keeps its descriptor past the tail's switch to the new file
// (logrotate signals it afterwards) still has its lines read, once.
func TestFileTail_RotationReadsLateWritesToTheOldFile(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the rotated file is not drained on Windows: holding it would refuse the application's own rotation")
	}
	dir := t.TempDir()
	file := filepath.Join(dir, "app.log")
	writeFile(t, file, "")

	records := agentstate.SubscribeLogs(4096)
	defer agentstate.UnsubscribeLogs(records)

	p := startPollingFileTail(t, file, filepath.Join(dir, "bookmark.json"))
	defer stopFileTail(t, p)
	awaitTailWatching(t, p, file)

	appendLines(t, file, "a-1")
	time.Sleep(400 * time.Millisecond)
	old := file + ".1"
	if err := os.Rename(file, old); err != nil {
		t.Fatal(err)
	}
	writeFile(t, file, "")
	appendLines(t, file, "b-1")
	waitEmittedAtLeastLine(t, records, file, "b-1")
	appendLines(t, old, "late-1", "late-2")
	appendLines(t, file, "b-2")

	got := collectBodies(t, records, file, 3)
	seen := map[string]int{}
	for _, l := range got {
		seen[l]++
	}
	for _, l := range []string{"late-1", "late-2", "b-2"} {
		if seen[l] != 1 {
			t.Fatalf("%q read %d times (all: %q)", l, seen[l], got)
		}
	}
}

// waitEmittedAtLeastLine consumes records until body is seen.
func waitEmittedAtLeastLine(t *testing.T, records <-chan agentstate.LogRecord, file, body string) {
	t.Helper()
	deadline := time.After(10 * time.Second)
	for {
		select {
		case r := <-records:
			if r.Attributes["log.file.path"] == file && r.Body == body {
				return
			}
		case <-deadline:
			t.Fatalf("never saw %q", body)
		}
	}
}
