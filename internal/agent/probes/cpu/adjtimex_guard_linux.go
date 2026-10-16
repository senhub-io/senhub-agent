//go:build linux

package cpu

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"syscall"
	"time"

	"golang.org/x/sys/unix"
)

// adjtimexChildEnv marks the short-lived child that calls adjtimex once on
// the agent's behalf. RunAdjtimexChild must run before anything else in
// main so the child does nothing but that call.
const adjtimexChildEnv = "SENHUB_ADJTIMEX_CHILD"

const adjtimexChildTimeout = 5 * time.Second

var errAdjtimexForbidden = errors.New("the systemd unit forbids the adjtimex system call and would kill the agent on it " +
	"(a unit older than this binary); run 'sudo senhub-agent refresh-unit' to read clock synchronisation again")

// RunAdjtimexChild is the child side of adjtimexSurvivable: when the marker
// is set it calls adjtimex and exits, and the parent reads how it ended.
func RunAdjtimexChild() {
	if os.Getenv(adjtimexChildEnv) != "1" {
		return
	}
	// A SIGSYS kill would otherwise leave a core dump and a crash report in
	// the journal for an outcome the parent expects and handles.
	if err := unix.Setrlimit(unix.RLIMIT_CORE, &unix.Rlimit{}); err != nil {
		fmt.Fprintln(os.Stderr, "setrlimit core:", err)
	}
	var tx unix.Timex
	if _, err := unix.Adjtimex(&tx); err != nil {
		os.Exit(1)
	}
	os.Exit(0)
}

// adjtimexSurvivable reports whether this process can call adjtimex without
// being killed. A systemd unit's SystemCallFilter or ProtectClock without
// SystemCallErrorNumber kills the whole process on a filtered call, and an
// agent upgraded in place keeps the unit it was installed with; the 0.6.3
// unit filters adjtimex. Under a seccomp filter the call is first made by a
// child, which inherits the filter, so only the child dies.
func adjtimexSurvivable(statusPath string, runChild func() error) error {
	if seccompMode(statusPath) == "0" {
		return nil
	}
	err := runChild()
	if err == nil {
		return nil
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		if ws, ok := exitErr.Sys().(syscall.WaitStatus); ok && ws.Exited() {
			// The call failed with an errno (EPERM under a unit that sets
			// SystemCallErrorNumber): the agent itself would get the same
			// error and report it, not die.
			return nil
		}
		return errAdjtimexForbidden
	}
	return fmt.Errorf("probing adjtimex in a child process: %w", err)
}

// seccompMode returns the Seccomp field of a /proc/<pid>/status file: "0"
// when no filter applies, "" when it cannot be read.
func seccompMode(statusPath string) string {
	f, err := os.Open(statusPath) // #nosec G304 - a /proc status file
	if err != nil {
		return ""
	}
	defer f.Close()
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		if value, ok := strings.CutPrefix(scanner.Text(), "Seccomp:"); ok {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func runAdjtimexChild() error {
	ctx, cancel := context.WithTimeout(context.Background(), adjtimexChildTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "/proc/self/exe") // #nosec G204 - this binary, no arguments
	cmd.Env = append(os.Environ(), adjtimexChildEnv+"=1")
	return cmd.Run()
}
