package app

import (
	"bytes"
	"errors"
	"os"
	"os/exec"
	"runtime"
	"testing"
)

func TestMainDispatchesUpdateWatch(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping subprocess test in short mode")
	}
	if os.Getenv("SENHUB_UPDATE_WATCH_HELPER") == "1" {
		// Without --state-dir the Windows watcher refuses to run and writes
		// nothing, so the test never starts a real watcher there either.
		os.Args = []string{"agent", "update-watch"}
		Main()
		os.Exit(0)
	}
	cmd := exec.Command(os.Args[0], "-test.run=^TestMainDispatchesUpdateWatch$")
	cmd.Env = append(os.Environ(), "SENHUB_UPDATE_WATCH_HELPER=1")
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	err := cmd.Run()
	var exitErr *exec.ExitError
	if !errors.As(err, &exitErr) || exitErr.ExitCode() != 2 {
		t.Fatalf("expected exit code 2, got err=%v stderr=%q", err, stderr.String())
	}
	out := stderr.String()
	if runtime.GOOS != "windows" && !bytes.Contains([]byte(out), []byte("update-watch is only used on Windows")) {
		t.Errorf("stderr %q missing %q", out, "update-watch is only used on Windows")
	}
	if bytes.Contains([]byte(out), []byte("unknown command")) {
		t.Errorf("stderr %q must not contain %q", out, "unknown command")
	}
}
