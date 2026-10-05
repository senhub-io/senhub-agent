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
func restartServiceAfterUpdate() (string, error) {
	return serviceRestarter{
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
		exePath: func(pid string) (string, error) { return os.Readlink("/proc/" + pid + "/exe") },
		version: func(exe string) string {
			out, err := exec.Command(exe, "version").Output()
			if err != nil {
				return ""
			}
			first, _, _ := strings.Cut(string(out), "\n")
			return first
		},
		wait: 30 * time.Second,
		poll: 500 * time.Millisecond,
	}.run()
}
