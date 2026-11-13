package filetail

import (
	"bytes"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rs/zerolog"

	"senhub-agent.go/internal/agent/services/logger"
)

// The warnings about a doubtful configuration name the probe they are about:
// the poller names a probe after constructing it, so they are logged at start.
func TestConfigWarningNamesTheProbe(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "Core.log")
	writeFile(t, file, "")

	var buf bytes.Buffer
	zl := zerolog.New(&buf)
	probe, err := NewFileTailProbe(map[string]interface{}{
		"paths":         []interface{}{file},
		"bookmark_path": filepath.Join(dir, "bookmark.json"),
		"multiline": map[string]interface{}{
			"pattern": `^\d{4}-\d{2}-\d{2} `,
			"negate":  true,
			"match":   "after",
		},
	}, (*logger.Logger)(&zl))
	if err != nil {
		t.Fatalf("NewFileTailProbe: %v", err)
	}
	p := probe.(*FileTailProbe)
	p.SetName("core-log")
	if err := p.OnStart(make(chan struct{})); err != nil {
		t.Fatalf("OnStart: %v", err)
	}
	defer stopFileTail(t, p)

	for _, line := range strings.Split(buf.String(), "\n") {
		if strings.Contains(line, "negate") && strings.Contains(line, `"level":"warn"`) {
			if !strings.Contains(line, `"probe":"core-log"`) {
				t.Fatalf("the multiline warning does not name the probe: %s", line)
			}
			return
		}
	}
	t.Fatalf("no multiline warning logged:\n%s", buf.String())
}
