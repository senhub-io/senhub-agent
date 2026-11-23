//go:build linux

package app

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"
)

const serviceUnitName = "senhub-agent"

// restartServiceAfterUpdate restarts the systemd service after update has
// installed the new binary (#975).
func restartServiceAfterUpdate(startIfStopped bool) (string, error) {
	return newServiceRestarter(startIfStopped, func(pid string) (string, error) { return os.Readlink("/proc/" + pid + "/exe") }).run()
}

// restartServiceAfterRootUpdate is the restart of the root update unit. That
// unit holds no capability, and reading /proc/<pid>/exe of a process of
// another user needs CAP_SYS_PTRACE, which is more than an update is worth. The
// command line of the service is world-readable and names the binary it was
// started from; the restart check still demands a new process that stays up.
func restartServiceAfterRootUpdate(startIfStopped bool) (string, error) {
	return newServiceRestarter(startIfStopped, func(pid string) (string, error) {
		raw, err := os.ReadFile("/proc/" + pid + "/cmdline")
		if err != nil {
			return "", err
		}
		exe, _, _ := strings.Cut(string(raw), "\x00")
		if exe == "" {
			return "", fmt.Errorf("process %s has no command line", pid)
		}
		return exe, nil
	}).run()
}

func newServiceRestarter(startIfStopped bool, exePath func(pid string) (string, error)) serviceRestarter {
	return serviceRestarter{
		startIfStopped: startIfStopped,
		installed: func() bool {
			return resolveUnitPath("", func(p string) bool { _, err := os.Stat(p); return err == nil }) != ""
		},
		isActive: func() bool {
			out, _ := exec.Command("systemctl", "is-active", serviceUnitName).Output()
			return strings.TrimSpace(string(out)) == "active"
		},
		mainPID: func() string {
			out, _ := exec.Command("systemctl", "show", "-p", "MainPID", "--value", serviceUnitName).Output()
			return strings.TrimSpace(string(out))
		},
		restart: func() error {
			if out, err := exec.Command("systemctl", "restart", serviceUnitName).CombinedOutput(); err != nil {
				return fmt.Errorf("%w: %s", err, strings.TrimSpace(string(out)))
			}
			return nil
		},
		exePath: exePath,
		version: func(exe string) string {
			out, err := exec.Command(exe, "version").Output()
			if err != nil {
				return ""
			}
			first, _, _ := strings.Cut(string(out), "\n")
			return first
		},
		wait:       30 * time.Second,
		poll:       500 * time.Millisecond,
		settle:     10 * time.Second,
		statusHint: "check 'systemctl status senhub-agent'",
	}
}
