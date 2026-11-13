package filetail

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"senhub-agent.go/internal/agent/services/agentstate"
)

// A tail that starts with the file in the middle of a private key, as
// after an agent restart between its BEGIN and END lines, masks the rest
// of the key although the masking state of the previous run is gone.
func TestFileTail_StartInsidePrivateKeyMasksTheRest(t *testing.T) {
	keyLines := generatePEMLines(t)
	dir := t.TempDir()
	file := filepath.Join(dir, "app.log")

	var head strings.Builder
	head.WriteString(backlog(40))
	for _, l := range keyLines[:11] {
		head.Write(l)
		head.WriteByte('\n')
	}
	if err := os.WriteFile(file, []byte(head.String()), 0o644); err != nil {
		t.Fatal(err)
	}

	records := agentstate.SubscribeLogs(4096)
	defer agentstate.UnsubscribeLogs(records)

	p := startFileTail(t, map[string]interface{}{
		"paths":         []interface{}{file},
		"bookmark_path": filepath.Join(dir, "bookmark.json"),
	})
	defer stopFileTail(t, p)
	awaitTailWatching(t, p, file)

	var rest []string
	for _, l := range keyLines[11:] {
		rest = append(rest, string(l))
	}
	rest = append(rest, "2026-10-29 12:00:01 INFO after the key")
	appendLines(t, file, rest...)

	var got []string
	deadline := time.After(10 * time.Second)
	for len(got) == 0 || got[len(got)-1] != "2026-10-29 12:00:01 INFO after the key" {
		select {
		case r := <-records:
			if r.Attributes["log.file.path"] == file {
				got = append(got, r.Body)
			}
		case <-deadline:
			t.Fatalf("records of %s so far: %q", file, got)
		}
	}

	end := string(keyLines[len(keyLines)-1])
	sawEnd := false
	for _, body := range got {
		for _, l := range keyLines[1 : len(keyLines)-1] {
			if strings.Contains(body, string(l)) {
				t.Fatalf("key body line sent in clear: %q", body)
			}
		}
		if strings.Contains(body, end) {
			if body != "***"+end {
				t.Fatalf("END line = %q, want %q", body, "***"+end)
			}
			sawEnd = true
		}
	}
	if !sawEnd {
		t.Fatalf("no END record among %q", got)
	}
}
