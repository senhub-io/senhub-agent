//go:build !windows

package execprobe

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

func readPID(t *testing.T, path string) int {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if b, err := os.ReadFile(path); err == nil {
			pid, convErr := strconv.Atoi(strings.TrimSpace(string(b)))
			if convErr != nil {
				t.Fatalf("pid file %s: %v", path, convErr)
			}
			return pid
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("pid file %s never written", path)
	return 0
}

func waitGone(t *testing.T, name string, pid int) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if err := syscall.Kill(pid, 0); errors.Is(err, syscall.ESRCH) {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Errorf("%s (pid %d) is still running after the collection was cut", name, pid)
}

// The scheduler's deadline reaches the command: with a 1 s context and a
// 10 s exec timeout, a command that would run 5 s is cut at about 1 s, as an
// error wrapping the context's, and its whole process group is gone.
func TestCollectContext_ContextDeadlineCutsTheRunAndKillsTheGroup(t *testing.T) {
	dir := t.TempDir()
	shellPID := filepath.Join(dir, "shell.pid")
	childPID := filepath.Join(dir, "child.pid")
	script := writeScript(t,
		"echo $$ > "+shellPID+".tmp && mv "+shellPID+".tmp "+shellPID+"\n"+
			"sleep 5 &\n"+
			"echo $! > "+childPID+".tmp && mv "+childPID+".tmp "+childPID+"\n"+
			"wait\n",
		"",
	)
	p := newTestProbe(t, map[string]interface{}{"command": script, "timeout": 10})

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	start := time.Now()
	points, err := p.CollectContext(ctx)
	elapsed := time.Since(start)

	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("err = %v, want one wrapping context.DeadlineExceeded", err)
	}
	if points != nil {
		t.Errorf("a cut run must return no datapoints, got %d", len(points))
	}
	if elapsed < 900*time.Millisecond || elapsed > 3*time.Second {
		t.Errorf("cut after %v, want about 1s (the context), not 5s (the command) or 10s (the exec timeout)", elapsed)
	}
	waitGone(t, "shell", readPID(t, shellPID))
	waitGone(t, "sleep child", readPID(t, childPID))
}
