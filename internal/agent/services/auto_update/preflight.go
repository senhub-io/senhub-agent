package auto_update

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
)

// WritabilityPreflight reports a problem with the running binary's writability,
// or "" when the layout is correct. What counts as a problem is the opposite on
// the two platforms, which is the whole reason this function reads the way it
// does.
//
// On Linux the daemon must NOT be able to write its own executable (#794). A
// writable binary is the finding, not the fix: it means the agent was installed
// somewhere the service account owns, so whoever compromises the daemon — the
// process that parses OTLP, SNMP traps, syslog and probe responses — can
// rewrite what systemd executes and persist across restarts. In-process update
// is refused there regardless (see selfApplyRefusal), so a writable binary buys
// nothing and costs that.
//
// Everywhere else the in-process updater still replaces the running binary via
// a sibling temp file and a rename, so it needs write access to BOTH the
// executable and its directory, and the absence of it is what breaks updates
// silently, one permission error per cycle (#377).
//
// canWrite is injected so the logic is unit-testable without depending on the
// test process's uid or permissions.
func WritabilityPreflight(exePath string, canWrite func(path string) bool) string {
	return writabilityPreflightFor(runtime.GOOS, exePath, canWrite)
}

// writabilityPreflightFor is the decision with the OS injected, so both
// branches are exercised on every runner rather than only on the platform each
// one guards.
func writabilityPreflightFor(goos, exePath string, canWrite func(path string) bool) string {
	if exePath == "" {
		return ""
	}
	dir := filepath.Dir(exePath)
	writable := canWrite(exePath) && canWrite(dir)

	if goos == "linux" {
		if !writable {
			return ""
		}
		return fmt.Sprintf(
			"the running binary %s is writable by this service account; a compromised agent could "+
				"replace what systemd executes and survive a restart. Re-install so the binary is root-owned "+
				"(sudo senhub-agent install), or install it through the package manager.",
			exePath)
	}

	if writable {
		return ""
	}
	return fmt.Sprintf(
		"auto_update.enabled is set but the running binary %s is not writable by this process; "+
			"in-process self-update will fail every cycle. Re-install so the binary is staged in a "+
			"writable directory, or use operator-driven 'senhub-agent update'.",
		exePath)
}

// CheckBinaryReplaceable runs WritabilityPreflight against the current
// executable using the real OS writability probe. Returns "" when the layout is
// correct for this platform.
func CheckBinaryReplaceable() string {
	exe, err := os.Executable()
	if err != nil {
		return ""
	}
	return WritabilityPreflight(exe, pathWritable)
}
