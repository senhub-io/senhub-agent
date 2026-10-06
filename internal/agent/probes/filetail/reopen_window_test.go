package filetail

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/nxadm/tail/watch"

	"senhub-agent.go/internal/agent/cliArgs"
	"senhub-agent.go/internal/agent/services/logger"
)

// The window nxadm/tail leaves at a start, between its read to the end of
// the file and the registration of its change watch, opens again at every
// reopen after a rotation. A line appended inside it raises no event and,
// on a quiet log, sits unread for the next write, a full rescan plus the
// stall grace later. The tail drops its watch here instead of racing the
// window, which leaves it in the state the race produces.
func TestFileTail_LineAppendedInTheReopenWindowIsReadWithoutAnotherWrite(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "quiet.log")
	writeFile(t, file, backlog(10))

	probe, err := NewFileTailProbe(map[string]interface{}{
		"paths":         []interface{}{file},
		"bookmark_path": filepath.Join(dir, "bookmark.json"),
	}, logger.NewLogger(&cliArgs.ParsedArgs{Env: "test"}))
	if err != nil {
		t.Fatalf("NewFileTailProbe: %v", err)
	}
	p := probe.(*FileTailProbe)
	p.verifyDelay = 300 * time.Millisecond
	if err := p.OnStart(make(chan struct{})); err != nil {
		t.Fatalf("OnStart: %v", err)
	}
	defer stopFileTail(t, p)

	// Past the looks taken at the start of the tail, which would otherwise
	// repair what the reopen leaves.
	time.Sleep(4 * p.verifyDelay)
	ts := tailStateOf(t, p, file)

	if err := os.Rename(file, file+".1"); err != nil {
		t.Fatal(err)
	}
	writeFile(t, file, "")
	fresh, err := os.Stat(file)
	if err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		ts.mu.Lock()
		reopened := ts.opened != nil && os.SameFile(ts.opened, fresh)
		ts.mu.Unlock()
		if reopened {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}

	if err := watch.RemoveWatch(file); err != nil {
		t.Logf("dropping the watch: %v", err)
	}
	lines := newFileLines(5)
	appendLines(t, file, lines...)

	if got := waitEmitted(p, uint64(len(lines)), 6*p.verifyDelay+4*time.Second); got != uint64(len(lines)) {
		t.Fatalf("emitted %d records, want the %d lines appended after the reopen", got, len(lines))
	}
	time.Sleep(300 * time.Millisecond)
	if got := p.emitted.Load(); got != uint64(len(lines)) {
		t.Fatalf("emitted %d records, want exactly %d (no replay after the restart)", got, len(lines))
	}
}
