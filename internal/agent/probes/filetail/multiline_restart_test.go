package filetail

import (
	"path/filepath"
	"testing"
	"time"

	"senhub-agent.go/internal/agent/services/agentstate"
)

func TestFileTail_RestartWithAPendingMultilineRecordKeepsItWhole(t *testing.T) {
	file := filepath.Join(t.TempDir(), "Core.log")
	records := agentstate.SubscribeLogs(4096)
	defer agentstate.UnsubscribeLogs(records)

	// Long flush delay so the multiline record stays held.
	p := startMultilineTail(t, file, "30s", records)
	defer stopFileTail(t, p)

	ts := tailStateOf(t, p, file)
	before := ts.offset.Load()
	appendLines(t, file, "2026-10-20 09:30:01.000001 ERROR TId 1 Core> failed:")

	deadline := time.Now().Add(5 * time.Second)
	for ts.offset.Load() <= before && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}
	if ts.offset.Load() <= before {
		t.Fatalf("offset did not advance after appending header line")
	}

	p.restartStalledTail(file, ts, "test restart", true)

	appendLines(t, file,
		"   at Frame.One()",
		"2026-10-20 09:30:02.000001 INFO  TId 2 Core> next",
	)

	want := "2026-10-20 09:30:01.000001 ERROR TId 1 Core> failed:\n   at Frame.One()"
	got := bodiesFor(records, file, 2*time.Second)
	if len(got) == 0 || got[0] != want {
		t.Fatalf("first record = %q, want %q", got, want)
	}
	for _, body := range got {
		if body == "2026-10-20 09:30:01.000001 ERROR TId 1 Core> failed:" {
			t.Fatalf("header published alone: %q", got)
		}
	}
}
