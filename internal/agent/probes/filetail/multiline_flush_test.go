package filetail

import (
	"path/filepath"
	"strings"
	"testing"
	"time"

	"senhub-agent.go/internal/agent/services/agentstate"
)

const readinessMark = "multiline readiness"

// startMultilineTail starts a tail on file that folds on a leading date and
// sends a quiet record after flushTimeout, and returns once lines written
// to the file reach the log rail.
func startMultilineTail(t *testing.T, file, flushTimeout string, records <-chan agentstate.LogRecord) *FileTailProbe {
	t.Helper()
	writeFile(t, file, "")
	p := startFileTail(t, map[string]interface{}{
		"paths":         []interface{}{file},
		"bookmark_path": filepath.Join(filepath.Dir(file), "bookmark.json"),
		"multiline": map[string]interface{}{
			"pattern":       `^\d{4}-\d{2}-\d{2} `,
			"flush_timeout": flushTimeout,
		},
	})
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		appendLines(t, file, "2026-10-20 09:29:00.000000 INFO  TId 1 Core> "+readinessMark)
		wait := time.After(3 * time.Second)
	poll:
		for {
			select {
			case r := <-records:
				if r.Attributes["log.file.path"] == file && strings.Contains(r.Body, readinessMark) {
					dropPendingReadiness(records)
					return p
				}
			case <-wait:
				break poll
			}
		}
	}
	t.Fatalf("tail on %s never sent a line", file)
	return p
}

// dropPendingReadiness drops the readiness records still on their way.
func dropPendingReadiness(records <-chan agentstate.LogRecord) {
	for {
		select {
		case <-records:
		case <-time.After(1500 * time.Millisecond):
			return
		}
	}
}

// bodiesFor collects the records of file until none came for quiet.
func bodiesFor(records <-chan agentstate.LogRecord, file string, quiet time.Duration) []string {
	var got []string
	for {
		select {
		case r := <-records:
			if r.Attributes["log.file.path"] == file && !strings.Contains(r.Body, readinessMark) {
				got = append(got, r.Body)
			}
		case <-time.After(quiet):
			return got
		}
	}
}

// The last message of a file that goes quiet is sent once the flush
// timeout passes, not held until the next message or the agent's stop.
func TestFileTail_QuietFileSendsTheLastRecord(t *testing.T) {
	file := filepath.Join(t.TempDir(), "Core.log")
	records := agentstate.SubscribeLogs(4096)
	defer agentstate.UnsubscribeLogs(records)
	p := startMultilineTail(t, file, "300ms", records)
	defer stopFileTail(t, p)

	appendLines(t, file, "2026-10-20 09:30:01.000001 ERROR TId 1234 Core> request failed:\r", "   at Paessler.Core.Request()")
	got := bodiesFor(records, file, 2*time.Second)
	want := "2026-10-20 09:30:01.000001 ERROR TId 1234 Core> request failed:\n   at Paessler.Core.Request()"
	if len(got) != 1 || got[0] != want {
		t.Fatalf("records = %q, want the one folded record %q", got, want)
	}
}

// A stack trace written line by line keeps folding while each line comes
// within the flush timeout. A pause longer than the timeout sends what
// came so far; a continuation after it stands on its own.
func TestFileTail_SlowStackTraceStaysOneRecordWithinTheTimeout(t *testing.T) {
	file := filepath.Join(t.TempDir(), "app.log")
	records := agentstate.SubscribeLogs(4096)
	defer agentstate.UnsubscribeLogs(records)
	p := startMultilineTail(t, file, "1s", records)
	defer stopFileTail(t, p)

	appendLines(t, file, "2026-10-20 09:30:01.000001 ERROR TId 1 Core> failed:")
	time.Sleep(300 * time.Millisecond)
	appendLines(t, file, "   at Frame.One()")
	time.Sleep(300 * time.Millisecond)
	appendLines(t, file, "   at Frame.Two()")
	appendLines(t, file, "2026-10-20 09:30:05.000001 INFO  TId 2 Core> next")
	time.Sleep(2 * time.Second)
	appendLines(t, file, "   late continuation")

	got := bodiesFor(records, file, 2500*time.Millisecond)
	want := []string{
		"2026-10-20 09:30:01.000001 ERROR TId 1 Core> failed:\n   at Frame.One()\n   at Frame.Two()",
		"2026-10-20 09:30:05.000001 INFO  TId 2 Core> next",
		"   late continuation",
	}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("records\n got %q\nwant %q", got, want)
	}
}
