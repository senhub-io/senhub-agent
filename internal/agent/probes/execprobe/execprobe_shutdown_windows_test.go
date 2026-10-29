//go:build windows

package execprobe

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"golang.org/x/sys/windows"
)

func windowsProcessExists(pid uint32) bool {
	h, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, pid)
	if err != nil {
		return false
	}
	defer windows.CloseHandle(h) //nolint:errcheck // test cleanup
	var code uint32
	if err := windows.GetExitCodeProcess(h, &code); err != nil {
		return false
	}
	return code == 259 // STILL_ACTIVE
}

func TestOnShutdown_JobObjectEndsGrandchild(t *testing.T) {
	dir := t.TempDir()
	pidFile := filepath.Join(dir, "child.pid")
	script := filepath.Join(dir, "slow.ps1")
	body := "$p = Start-Process -FilePath ping -ArgumentList '-n','30','127.0.0.1' -PassThru -WindowStyle Hidden\r\n" +
		"Set-Content -Path '" + pidFile + "' -Value $p.Id\r\n" +
		"Start-Sleep -Seconds 30\r\n"
	if err := os.WriteFile(script, []byte(body), 0o600); err != nil {
		t.Fatalf("writing script: %v", err)
	}
	psExe := filepath.Join(os.Getenv("SystemRoot"), "System32", "WindowsPowerShell", "v1.0", "powershell.exe")

	p := newTestProbe(t, map[string]interface{}{
		"command": psExe,
		"args":    []interface{}{"-NoProfile", "-NonInteractive", "-ExecutionPolicy", "Bypass", "-File", script},
		"timeout": 60,
	})
	if err := p.OnStart(make(chan struct{})); err != nil {
		t.Fatalf("OnStart: %v", err)
	}
	collected := make(chan struct{})
	go func() {
		_, _ = p.Collect()
		close(collected)
	}()

	var pid uint32
	deadline := time.Now().Add(20 * time.Second)
	for pid == 0 {
		if b, err := os.ReadFile(pidFile); err == nil {
			if n, convErr := strconv.Atoi(strings.TrimSpace(string(b))); convErr == nil {
				pid = uint32(n)
			}
		}
		if time.Now().After(deadline) {
			t.Fatal("script did not report the grandchild PID")
		}
		time.Sleep(50 * time.Millisecond)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 6*time.Second)
	defer cancel()
	if err := p.OnShutdown(ctx); err != nil {
		t.Fatalf("OnShutdown: %v", err)
	}
	select {
	case <-collected:
	case <-time.After(time.Second):
		t.Error("Collect still running after OnShutdown returned")
	}
	for end := time.Now().Add(3 * time.Second); windowsProcessExists(pid); time.Sleep(50 * time.Millisecond) {
		if time.Now().After(end) {
			t.Fatalf("grandchild %d survived OnShutdown", pid)
		}
	}
}
