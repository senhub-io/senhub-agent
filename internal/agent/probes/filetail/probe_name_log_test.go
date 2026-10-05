package filetail

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rs/zerolog"
)

// Two probes on one file wrote identical Warn lines: nothing said which
// of them the line was about.
func TestFileTail_EveryWarnAndInfoLineNamesTheProbe(t *testing.T) {
	dir := t.TempDir()
	missing := filepath.Join(dir, "absent", "app.log")
	logs := &lockedBuffer{}
	base := zerolog.New(logs).Level(zerolog.DebugLevel)
	probe, err := NewFileTailProbe(map[string]interface{}{
		"paths":         []interface{}{"/nonexistent-dir/*/app.log", missing},
		"bookmark_path": filepath.Join(dir, "bookmark.json"),
	}, &base)
	if err != nil {
		t.Fatal(err)
	}
	p := probe.(*FileTailProbe)
	p.SetName("filetail_b")
	if err := p.OnStart(make(chan struct{})); err != nil {
		t.Fatal(err)
	}
	p.recordIssues(map[string]string{missing: "does not exist for the agent service"})
	p.recordIssues(map[string]string{})
	stopFileTail(t, p)

	seen := 0
	logs.mu.Lock()
	raw := logs.b.String()
	logs.mu.Unlock()
	for _, line := range strings.Split(strings.TrimSpace(raw), "\n") {
		var entry map[string]interface{}
		if err := json.Unmarshal([]byte(line), &entry); err != nil {
			t.Fatalf("log line %q: %v", line, err)
		}
		level, _ := entry["level"].(string)
		if level != "warn" && level != "info" {
			continue
		}
		seen++
		if entry["probe"] != "filetail_b" {
			t.Errorf("%s line without the probe name: %s", level, line)
		}
	}
	if seen < 2 {
		t.Fatalf("only %d warn/info lines captured:\n%s", seen, raw)
	}
}
