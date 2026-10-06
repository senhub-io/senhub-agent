//go:build linux

package app

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
)

// serviceEnabledOnHost asks the service manager whether the unit starts at
// boot. systemctl exits 0 for every enabled flavour (enabled, static,
// alias) and non-zero for disabled or absent.
func serviceEnabledOnHost() bool {
	return exec.Command("systemctl", "is-enabled", "--quiet", serviceUnitName).Run() == nil
}

// installedUnitOps wires unitOps to the real host.
func installedUnitOps() unitOps {
	path := loadedUnitPath()
	return unitOps{
		unitPath:     path,
		packageOwned: unitOwnedByPackage(path),
		read:         func() ([]byte, error) { return os.ReadFile(path) },
		write:        func(data []byte) error { return os.WriteFile(path, data, 0o644) },
		reload:       daemonReload,
		enabled:      serviceEnabledOnHost,
		enable: func() error {
			if out, err := exec.Command("systemctl", "enable", serviceUnitName).CombinedOutput(); err != nil {
				return fmt.Errorf("systemctl enable: %w (%s)", err, strings.TrimSpace(string(out)))
			}
			return nil
		},
	}
}

// installUnitCurrent reports whether the unit on disk is what install would
// write for serviceUser and serviceArgs. A unit the package manager owns is
// the package's to update and never counts as drift.
func installUnitCurrent(serviceUser string, serviceArgs []string) bool {
	ops := installedUnitOps()
	if ops.packageOwned {
		return true
	}
	desired, err := desiredInstallUnit(serviceUser, serviceArgs)
	if err != nil {
		return false
	}
	installed, err := ops.read()
	return err == nil && unitMatches(string(installed), desired)
}

// reconcileInstalledServiceOnHost applies the reconciliation to the real
// service manager.
func reconcileInstalledServiceOnHost(serviceUser string, serviceArgs []string) ([]string, error) {
	desired, err := desiredInstallUnit(serviceUser, serviceArgs)
	if err != nil {
		return nil, err
	}
	return reconcileInstalledService(installedUnitOps(), desired)
}
