//go:build linux

package app

import "os"

// installedServiceBinary returns the binary the installed unit execs when
// it is a distinct, existing file from the running one — the case where
// the two copies can drift. Empty otherwise, and on any read failure: a
// missing or unreadable unit means there is no managed service to skew
// against, and `--version` must stay silent rather than guess.
func installedServiceBinary() string {
	unit, err := os.ReadFile(installedUnitPath)
	if err != nil {
		return ""
	}
	self, err := os.Executable()
	if err != nil {
		return ""
	}
	target, _ := serviceBinaryTarget(string(unit), self, os.Stat)
	return target
}

// syncServiceBinary copies the freshly installed release at newBinary over
// the binary the systemd unit execs, and returns the path it wrote ("" when
// there was nothing to reconcile).
//
// newBinary is the path `update` resolved BEFORE applying the release, not
// os.Executable(): the updater renames the running file out of the way and
// writes the new one at the original path, so by the time this runs
// /proc/self/exe points at the old (already deleted) inode.
//
// Ownership is restored to the unit's User= afterwards: `update` runs as
// root, so the fresh copy would otherwise be root-owned and the daemon
// (running unprivileged) could never self-update again — the #377/#571
// failure mode, silently reintroduced by the very command meant to fix
// the skew.
func syncServiceBinary(newBinary string) (string, error) {
	unit, err := os.ReadFile(installedUnitPath)
	if err != nil {
		return "", nil
	}
	return reconcileServiceBinary(string(unit), newBinary, binaryFS{
		stat:  os.Stat,
		copy:  copyExecutable,
		chown: chownToUser,
	}, os.Stdout)
}
