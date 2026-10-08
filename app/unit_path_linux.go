//go:build linux

package app

import (
	"os"
	"os/exec"
	"strings"
)

// loadedUnitPath is the unit file systemd loads for senhub-agent, or
// installedUnitPath when none is found (so messages and writes keep a
// concrete target on a host with no unit yet).
func loadedUnitPath() string {
	out, _ := exec.Command("systemctl", "show", "-p", "FragmentPath", "--value", serviceUnitName).Output()
	p := resolveUnitPath(strings.TrimSpace(string(out)), func(path string) bool {
		info, err := os.Stat(path)
		return err == nil && !info.IsDir()
	})
	if p == "" {
		return installedUnitPath
	}
	return p
}
