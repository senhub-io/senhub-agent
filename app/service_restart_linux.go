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

// rootUpdateBinary is the binary the root update unit installs and the
// packaged service runs.
const rootUpdateBinary = "/usr/bin/senhub-agent"

// restartServiceAfterRootUpdate is the restart of the root update unit. It
// takes no string from the service, which runs as an unprivileged user and
// controls its own /proc/<pid>/cmdline: the binary it names for the version
// line, and the one it checks is on disk, is the fixed path just installed.
// Reading /proc/<pid>/exe instead would need CAP_SYS_PTRACE. The restart still
// demands a new process that stays up.
func restartServiceAfterRootUpdate(startIfStopped bool) (string, error) {
	return newRootServiceRestarter(startIfStopped, rootUpdateBinary).run()
}

func newRootServiceRestarter(startIfStopped bool, binary string) serviceRestarter {
	return newServiceRestarter(startIfStopped, func(string) (string, error) { return binary, nil })
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
