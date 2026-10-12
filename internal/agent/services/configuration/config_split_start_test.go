package configuration

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/rs/zerolog"

	"senhub-agent.go/internal/agent/cliArgs"
	"senhub-agent.go/internal/agent/services/logger"
)

// lockedBuffer is a log sink safe for the watcher goroutine that keeps
// writing while the test reads.
type lockedBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *lockedBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *lockedBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

func startCapturingLog(t *testing.T, cfg string) string {
	t.Helper()
	var buf lockedBuffer
	zl := zerolog.New(&buf)
	lc := NewLocalConfiguration(&cliArgs.ParsedArgs{ConfigPath: cfg}, (*logger.Logger)(&zl))
	if err := lc.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	_ = lc.Shutdown(context.Background())
	return buf.String()
}

// Install defaults beside a monolithic file: the start splits it and
// reports neither a failed split nor a failed seal.
func TestStart_InstallDefaultsBesideMonolithicSplits(t *testing.T) {
	dir := t.TempDir()
	cfg := filepath.Join(dir, "agent.yaml")
	if err := os.WriteFile(cfg, []byte(monolithicHTTPConfig), 0o600); err != nil {
		t.Fatal(err)
	}
	seedInstallDefaults(t, dir)

	out := startCapturingLog(t, cfg)
	if _, err := os.Stat(filepath.Join(dir, "probes.d", "00-host.yaml.install-default")); err != nil {
		t.Errorf("defaults not set aside: %v", err)
	}
	if strings.Contains(out, "not split") {
		t.Errorf("the split was reported as failed:\n%s", out)
	}
	if n := strings.Count(out, "Legacy monolithic config detected"); n > 1 {
		t.Errorf("legacy warning logged %d times, want at most 1", n)
	}
}

// User fragments beside a monolithic file: one line about the split, one
// legacy notice, no seal failure.
func TestStart_UserFragmentsBesideMonolithicLogOncePerKind(t *testing.T) {
	dir := t.TempDir()
	cfg := filepath.Join(dir, "agent.yaml")
	if err := os.WriteFile(cfg, []byte(monolithicHTTPConfig), 0o600); err != nil {
		t.Fatal(err)
	}
	seedInstallDefaults(t, dir)
	if err := os.WriteFile(filepath.Join(dir, "probes.d", "10-mine.yaml"), []byte("- name: mine\n  type: cpu\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	out := startCapturingLog(t, cfg)
	if n := strings.Count(out, "not split"); n != 1 {
		t.Errorf("split failure logged %d times, want 1:\n%s", n, out)
	}
	if n := strings.Count(out, "Legacy monolithic config detected"); n > 1 {
		t.Errorf("legacy warning logged %d times, want at most 1", n)
	}
	if strings.Contains(out, "Sealing inline secrets failed") {
		t.Errorf("a refused split must not also report a seal failure:\n%s", out)
	}
	if !strings.Contains(out, "10-mine.yaml") {
		t.Errorf("the message must name the file:\n%s", out)
	}
}
