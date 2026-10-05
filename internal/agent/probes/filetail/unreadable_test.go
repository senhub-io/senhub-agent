package filetail

import (
	"bytes"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/rs/zerolog"
)

type lockedBuffer struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (l *lockedBuffer) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.b.Write(p)
}

func (l *lockedBuffer) count(substr string) int {
	l.mu.Lock()
	defer l.mu.Unlock()
	return strings.Count(l.b.String(), substr)
}

func startLoggedFileTail(t *testing.T, cfg map[string]interface{}) (*FileTailProbe, *lockedBuffer) {
	t.Helper()
	buf := &lockedBuffer{}
	base := zerolog.New(buf).Level(zerolog.DebugLevel)
	probe, err := NewFileTailProbe(cfg, &base)
	if err != nil {
		t.Fatalf("NewFileTailProbe: %v", err)
	}
	p := probe.(*FileTailProbe)
	if err := p.OnStart(make(chan struct{})); err != nil {
		t.Fatalf("OnStart: %v", err)
	}
	return p, buf
}

func skipUnlessPermissionsApply(t *testing.T) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("POSIX permission bits do not apply on Windows")
	}
	if os.Geteuid() == 0 {
		t.Skip("root reads files whatever their mode")
	}
}

func TestFileTail_UnreadableFileIsAProbeErrorUntilItBecomesReadable(t *testing.T) {
	skipUnlessPermissionsApply(t)
	dir := t.TempDir()
	file := filepath.Join(dir, "app.log")
	writeFile(t, file, backlog(40))
	if err := os.Chmod(file, 0o000); err != nil {
		t.Fatal(err)
	}

	p, logs := startLoggedFileTail(t, map[string]interface{}{
		"paths":         []interface{}{file},
		"bookmark_path": filepath.Join(dir, "bookmark.json"),
	})
	defer stopFileTail(t, p)

	if _, err := p.Collect(); err == nil || !strings.Contains(err.Error(), file) || !strings.Contains(err.Error(), "permission denied") {
		t.Fatalf("Collect error = %v, want one naming %s and the permission failure", err, file)
	}
	for i := 0; i < 3; i++ {
		p.scanAndTail()
	}
	if got := logs.count("configured path cannot be read"); got != 1 {
		t.Fatalf("Warn logged %d times over 4 scans, want once per state change", got)
	}
	if got := logs.count(`"level":"warn"`); got < 1 {
		t.Fatal("the unreadable path was not logged at Warn")
	}

	if err := os.Chmod(file, 0o644); err != nil {
		t.Fatal(err)
	}
	p.scanAndTail()
	if _, err := p.Collect(); err != nil {
		t.Fatalf("Collect after the file became readable: %v", err)
	}
	if got := logs.count("configured path is readable again"); got != 1 {
		t.Fatalf("recovery logged %d times, want 1", got)
	}
	appendLines(t, file, newFileLines(5)...)
	if got := waitEmitted(p, 5, 5*time.Second); got != 5 {
		t.Fatalf("emitted %d of 5 lines once readable", got)
	}
}

func TestFileTail_PathInUnsearchableDirectoryIsAProbeError(t *testing.T) {
	skipUnlessPermissionsApply(t)
	root := t.TempDir()
	dir := filepath.Join(root, "private")
	if err := os.Mkdir(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(dir, "app.log")
	writeFile(t, file, backlog(40))
	if err := os.Chmod(dir, 0o000); err != nil {
		t.Fatal(err)
	}
	defer os.Chmod(dir, 0o755)

	p, _ := startLoggedFileTail(t, map[string]interface{}{
		"paths": []interface{}{file, filepath.Join(dir, "*.log")},
	})
	defer stopFileTail(t, p)

	_, err := p.Collect()
	if err == nil || strings.Count(err.Error(), "permission denied") < 2 {
		t.Fatalf("Collect error = %v, want the literal path and the glob both reported", err)
	}
}

func TestFileTail_MissingLiteralPathIsAProbeErrorUntilItAppears(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "later.log")
	missingGlob := filepath.Join(dir, "none-*.log")

	p, _ := startLoggedFileTail(t, map[string]interface{}{
		"paths": []interface{}{file, missingGlob},
	})
	defer stopFileTail(t, p)

	_, err := p.Collect()
	if err == nil || !strings.Contains(err.Error(), "does not exist") || !strings.Contains(err.Error(), file) {
		t.Fatalf("Collect error = %v, want the missing literal path reported", err)
	}
	if strings.Contains(err.Error(), "none-*.log") {
		t.Fatalf("a glob matching nothing yet is not an error: %v", err)
	}

	writeFile(t, file, backlog(3))
	p.scanAndTail()
	if _, err := p.Collect(); err != nil {
		t.Fatalf("Collect once the file exists: %v", err)
	}
}
