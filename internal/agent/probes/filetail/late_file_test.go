package filetail

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"senhub-agent.go/internal/agent/cliArgs"
	"senhub-agent.go/internal/agent/services/logger"
)

func TestFileTail_LiteralPathInMissingDirectoryIsReadOnceItAppears(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "mounted-later")
	file := filepath.Join(dir, "server.log")

	baseLogger := logger.NewLogger(&cliArgs.ParsedArgs{Env: "test"})
	probe, err := NewFileTailProbe(map[string]interface{}{
		"paths": []interface{}{file},
		"multiline": map[string]interface{}{
			"pattern": `^\[`,
			"negate":  true,
			"match":   "after",
		},
	}, baseLogger)
	if err != nil {
		t.Fatalf("NewFileTailProbe: %v", err)
	}
	p := probe.(*FileTailProbe)
	quit := make(chan struct{})
	if err := p.OnStart(quit); err != nil {
		t.Fatalf("OnStart: %v", err)
	}
	defer close(quit)

	p.mu.Lock()
	started := len(p.tailing)
	p.mu.Unlock()
	if started != 0 {
		t.Fatalf("a tail was started on a missing file (%d active)", started)
	}

	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	content := "[t1] INFO first\n[t2] ERROR second\n\tat frame\n[t3] INFO third\n"
	if err := os.WriteFile(file, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	p.scanAndTail()

	// The file was created after the probe started watching for it, so it
	// is read from its first byte even with from_beginning false. The
	// third record stays pending in the multiline assembler until the
	// next record starts.
	deadline := time.Now().Add(5 * time.Second)
	for p.emitted.Load() < 2 && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}
	if got := p.emitted.Load(); got != 2 {
		t.Fatalf("records emitted = %d, want 2", got)
	}
}
