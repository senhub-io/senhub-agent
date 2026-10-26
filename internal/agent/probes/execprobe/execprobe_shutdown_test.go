//go:build !windows

package execprobe

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestOnShutdown_KillsRunningCommandAndChildren(t *testing.T) {
	dir := t.TempDir()
	pidFile := filepath.Join(dir, "pids")
	script := filepath.Join(dir, "slow.sh")
	body := "#!/bin/sh\nsleep 30 &\necho $$ $! > \"" + pidFile + ".tmp\"\nmv \"" + pidFile + ".tmp\" \"" + pidFile + "\"\nsleep 30\n"
	if err := os.WriteFile(script, []byte(body), 0o700); err != nil {
		t.Fatalf("writing script: %v", err)
	}

	p := newTestProbe(t, map[string]interface{}{"command": script, "timeout": 60})
	if err := p.OnStart(make(chan struct{})); err != nil {
		t.Fatalf("OnStart: %v", err)
	}

	collected := make(chan struct{})
	go func() {
		_, _ = p.Collect()
		close(collected)
	}()

	var pids []int
	deadline := time.Now().Add(5 * time.Second)
	for len(pids) != 2 {
		if b, err := os.ReadFile(pidFile); err == nil {
			pids = pids[:0]
			for _, f := range strings.Fields(string(b)) {
				n, convErr := strconv.Atoi(f)
				if convErr != nil {
					t.Fatalf("bad pid %q", f)
				}
				pids = append(pids, n)
			}
		}
		if time.Now().After(deadline) {
			t.Fatal("script did not report its PIDs")
		}
		time.Sleep(10 * time.Millisecond)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 6*time.Second)
	defer cancel()
	start := time.Now()
	if err := p.OnShutdown(ctx); err != nil {
		t.Fatalf("OnShutdown: %v", err)
	}
	if elapsed := time.Since(start); elapsed > 6*time.Second {
		t.Errorf("OnShutdown took %v, want under 6s", elapsed)
	}

	select {
	case <-collected:
	case <-time.After(time.Second):
		t.Error("Collect still running after OnShutdown returned")
	}

	for _, pid := range pids {
		gone := false
		for end := time.Now().Add(3 * time.Second); time.Now().Before(end); time.Sleep(20 * time.Millisecond) {
			if err := syscall.Kill(pid, 0); err == syscall.ESRCH {
				gone = true
				break
			}
		}
		if !gone {
			_ = syscall.Kill(pid, syscall.SIGKILL)
			t.Errorf("process %d survived OnShutdown", pid)
		}
	}
}
