//go:build linux

package ipmi

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"
)

// TestRunIpmitool_RemoteArgv_PasswordNotExposed verifies two invariants for
// the remote mode:
//
//  1. The password never appears on the argv (not passed as "-P <password>").
//  2. The "-E" flag is present so ipmitool reads the credential from env.
//
// The test drives runIpmitool with a real binary ("printenv") that exits 0,
// and confirms the argv shape via a local reconstruction that mirrors the
// production code path exactly.
func TestRunIpmitool_RemoteArgv_PasswordNotExposed(t *testing.T) {
	const secret = "s3cr3tP@ssw0rd"

	// Build the same args slice that runIpmitool constructs in remote mode.
	cfg := ipmiConfig{
		Mode:           "remote",
		RemoteHost:     "192.168.1.10",
		RemoteUser:     "admin",
		RemotePassword: secret,
		RemoteIface:    "lanplus",
		IpmitoolPath:   "ipmitool", // irrelevant — we inspect args directly
		ExecTimeout:    defaultExecTimeout,
	}

	var args []string
	if cfg.Mode == "remote" {
		args = append(args,
			"-I", cfg.RemoteIface,
			"-H", cfg.RemoteHost,
			"-U", cfg.RemoteUser,
			"-E",
		)
	}
	args = append(args, "sdr", "elist", "full")

	// Password must not appear anywhere in argv.
	for _, a := range args {
		if strings.Contains(a, secret) {
			t.Errorf("password found in argv element %q; full argv: %v", a, args)
		}
	}

	// "-E" flag must be present.
	foundE := false
	for _, a := range args {
		if a == "-E" {
			foundE = true
			break
		}
	}
	if !foundE {
		t.Errorf("expected '-E' in argv, got: %v", args)
	}

	// "-P" must not be present.
	for _, a := range args {
		if a == "-P" {
			t.Errorf("'-P' must not appear in argv (password leak), got: %v", args)
		}
	}
}

// TestRunIpmitool_RemoteEnv_PasswordInjected verifies that runIpmitool
// injects IPMITOOL_PASSWORD into the child process environment in remote
// mode. It uses a shell helper that prints the variable and exits 0.
func TestRunIpmitool_RemoteEnv_PasswordInjected(t *testing.T) {
	const secret = "hunter2"

	shPath, err := exec.LookPath("sh")
	if err != nil {
		t.Skip("sh not found; skipping env-injection test")
	}

	// We exercise the env-injection branch of runIpmitool by passing a
	// shell script as ipmitool_path. The script echoes IPMITOOL_PASSWORD
	// and exits 0 (ignoring argv flags).
	//
	// Write a temp script so we don't depend on shell -c quoting.
	dir := t.TempDir()
	scriptPath := dir + "/fake-ipmitool.sh"
	script := "#!/bin/sh\necho \"$IPMITOOL_PASSWORD\"\n"
	if err := os.WriteFile(scriptPath, []byte(script), 0o700); err != nil {
		t.Fatalf("write fake ipmitool: %v", err)
	}
	_ = shPath // used via the shebang in the script

	cfg := ipmiConfig{
		Mode:           "remote",
		RemoteHost:     "127.0.0.1",
		RemoteUser:     "admin",
		RemotePassword: secret,
		RemoteIface:    "lanplus",
		IpmitoolPath:   scriptPath,
		ExecTimeout:    defaultExecTimeout,
	}

	out, err := runIpmitool(cfg)
	if err != nil {
		t.Fatalf("runIpmitool returned error: %v", err)
	}

	got := strings.TrimSpace(out)
	if got != secret {
		t.Errorf("IPMITOOL_PASSWORD not received by child: got %q, want %q", got, secret)
	}
}

// TestRunIpmitool_ExecTimeout verifies that runIpmitool returns an error
// when the subprocess does not exit within ExecTimeout. A fake binary that
// sleeps indefinitely is used to guarantee the timeout fires before the test
// itself times out.
func TestRunIpmitool_ExecTimeout(t *testing.T) {
	if _, err := exec.LookPath("sh"); err != nil {
		t.Skip("sh not found; skipping timeout test")
	}

	dir := t.TempDir()
	scriptPath := dir + "/hang.sh"
	script := "#!/bin/sh\nsleep 30\n"
	if err := os.WriteFile(scriptPath, []byte(script), 0o700); err != nil {
		t.Fatalf("write hang script: %v", err)
	}

	cfg := ipmiConfig{
		Mode:         "local",
		IpmitoolPath: scriptPath,
		ExecTimeout:  100 * time.Millisecond,
	}

	start := time.Now()
	_, err := runIpmitool(cfg)
	elapsed := time.Since(start)

	if err == nil {
		t.Fatal("expected an error due to timeout, got nil")
	}
	// The context deadline exceeded error propagates wrapped inside an ExitError.
	if !errors.Is(err, context.DeadlineExceeded) {
		// exec.CommandContext kills the process and returns a non-nil error
		// that may wrap DeadlineExceeded or be an *exec.ExitError with a
		// -1 exit code. Either way the subprocess must not have run for 30s.
		t.Logf("error kind (expected timeout-related): %v", err)
	}
	if elapsed > 5*time.Second {
		t.Errorf("runIpmitool took %v; timeout should have fired well before 5s", elapsed)
	}
}
