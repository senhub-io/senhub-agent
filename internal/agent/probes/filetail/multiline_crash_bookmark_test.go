package filetail

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"senhub-agent.go/internal/agent/services/agentstate"
)

// A record held by the multiline fold has not been published: the bookmark
// on disk must stay before its lines, so an agent killed while it waits
// reads them again at the next start instead of losing the record.
func TestFileTail_HeldMultilineRecordIsNotBookmarkedPast(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "core.log")
	bookmarkPath := filepath.Join(dir, "bookmark.json")
	records := agentstate.SubscribeLogs(4096)
	defer agentstate.UnsubscribeLogs(records)

	// flush_timeout 0 holds the last record until the next one starts.
	p := startMultilineTail(t, file, "0", records)
	stopped := false
	defer func() {
		if !stopped {
			stopFileTail(t, p)
		}
	}()

	recordA := []string{
		"2026-10-29 09:10:00.000000 ERROR TId 2 Core> record A failed",
		"   at Core.Run()",
		"   at Core.Main()",
	}
	recordB := []string{
		"2026-10-29 09:10:01.000000 ERROR TId 2 Core> record B failed",
		"   at Core.Retry()",
	}
	fi, err := os.Stat(file)
	if err != nil {
		t.Fatal(err)
	}
	startOfB := fi.Size()
	for _, l := range recordA {
		startOfB += int64(len(l)) + 1
	}
	appendLines(t, file, append(append([]string{}, recordA...), recordB...)...)

	got := bodiesFor(records, file, 1500*time.Millisecond)
	if len(got) != 1 || !strings.Contains(got[0], "record A") {
		t.Fatalf("records sent before B is complete: %q, want A alone", got)
	}

	// What a kill -9 leaves on disk: the bookmark as it is now.
	held := waitBookmarkOnDisk(t, bookmarkPath, file, func(e bookmarkEntry) bool {
		return e.Offset == startOfB
	}, 6*time.Second)
	crashed := filepath.Join(dir, "crashed.json")
	data, err := os.ReadFile(bookmarkPath)
	if err != nil {
		t.Fatal(err)
	}
	writeFile(t, crashed, string(data))
	stopFileTail(t, p)
	stopped = true
	bodiesFor(records, file, 500*time.Millisecond)

	again := startFileTail(t, map[string]interface{}{
		"paths":         []interface{}{file},
		"bookmark_path": crashed,
		"multiline": map[string]interface{}{
			"pattern":       `^\d{4}-\d{2}-\d{2} `,
			"flush_timeout": "0",
		},
	})
	time.Sleep(time.Second)
	stopFileTail(t, again)
	replayed := bodiesFor(records, file, 1500*time.Millisecond)
	if len(replayed) != 1 || replayed[0] != strings.Join(recordB, "\n") {
		t.Fatalf("after a restart from offset %d, sent %q, want record B once", held.Offset, replayed)
	}
}
