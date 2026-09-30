//go:build windows

package filetail

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"senhub-agent.go/internal/agent/cliArgs"
	"senhub-agent.go/internal/agent/services/logger"
)

// A log another process keeps open for writing, as PRTG does with its
// core log: lines appended through that open handle must be read.
func TestFileTail_FileHeldOpenByItsWriter(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "Core.log")
	w, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()
	fmt.Fprintln(w, "backlog line")

	probe, err := NewFileTailProbe(map[string]interface{}{"paths": []interface{}{path}}, logger.NewLogger(&cliArgs.ParsedArgs{Env: "test"}))
	if err != nil {
		t.Fatal(err)
	}
	p := probe.(*FileTailProbe)
	quit := make(chan struct{})
	if err := p.OnStart(quit); err != nil {
		t.Fatal(err)
	}
	defer close(quit)
	time.Sleep(1500 * time.Millisecond)

	for i := 0; i < 5; i++ {
		fmt.Fprintf(w, "live line %d\n", i)
		time.Sleep(200 * time.Millisecond)
	}
	deadline := time.Now().Add(10 * time.Second)
	for p.emitted.Load() < 5 && time.Now().Before(deadline) {
		time.Sleep(100 * time.Millisecond)
	}
	if got := p.emitted.Load(); got != 5 {
		t.Fatalf("lines appended to a file held open by its writer: %d emitted, want 5", got)
	}
}
