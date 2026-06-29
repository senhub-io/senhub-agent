package auto_update

import (
	"fmt"
	"os"
	"path/filepath"
)

// WritabilityPreflight reports why in-process auto-update cannot succeed,
// or "" when it can. selfupdate.Apply replaces the running binary by
// writing a sibling temp file and renaming it over the executable, so it
// needs write access to BOTH the executable file and its directory.
//
// Under the hardened unit the daemon runs as a non-root service user
// (#223); a binary left root-owned in /usr/bin — or anywhere under
// ProtectSystem=full's read-only tree — can never be replaced, and every
// hourly update cycle fails at write time with a permission error (#377).
// The installer stages the binary in a service-user-owned, writable dir
// for exactly this reason (#571). This preflight surfaces one clear
// diagnostic at startup instead of a silent per-cycle failure.
//
// canWrite is injected so the logic is unit-testable without depending on
// the test process's uid/permissions.
func WritabilityPreflight(exePath string, canWrite func(path string) bool) string {
	if exePath == "" {
		return ""
	}
	dir := filepath.Dir(exePath)
	if canWrite(exePath) && canWrite(dir) {
		return ""
	}
	return fmt.Sprintf(
		"auto_update.enabled is set but the running binary %s is not writable by this process; "+
			"in-process self-update will fail every cycle. Re-install so the binary is staged in a "+
			"writable, service-user-owned directory, or use operator-driven 'sudo senhub-agent update'.",
		exePath)
}

// CheckBinaryReplaceable runs WritabilityPreflight against the current
// executable using the real OS writability probe. Returns "" when the
// running binary can be replaced in place by the in-process updater.
func CheckBinaryReplaceable() string {
	exe, err := os.Executable()
	if err != nil {
		return ""
	}
	return WritabilityPreflight(exe, pathWritable)
}
