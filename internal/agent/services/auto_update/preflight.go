package auto_update

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
)

// ownership describes who owns an executable and how permissive its mode is.
// It is what the Linux check needs, and deliberately not "can this process
// write it".
type ownership struct {
	uid  int
	mode fs.FileMode
}

// exposedToNonRoot reports whether a non-root account could replace this file:
// either because a non-root account owns it, or because its mode grants write
// to group or other.
func (o ownership) exposedToNonRoot() bool {
	return o.uid != 0 || o.mode&0o022 != 0
}

// WritabilityPreflight reports a problem with the running binary, or "" when
// the layout is correct. What counts as a problem is the opposite on the two
// platforms, and — on Linux — is a different question from the one the name
// suggests.
//
// On Linux the daemon must NOT be able to write its own executable (#794): a
// writable binary means whoever compromises the daemon can rewrite what systemd
// executes and persist across restarts.
//
// The test for that is OWNERSHIP, not effective writability. Asking "can this
// process write the file" answers a different question for every caller: the
// daemon runs as `senhub` and gets the right answer, but `sudo senhub-agent
// config check` runs as root, root can write anything, and the check fired on
// every correctly installed host. A security warning that appears when nothing
// is wrong is worse than no warning — it is how operators learn to skip them.
//
// Everywhere else the in-process updater still replaces the running binary via
// a sibling temp file and a rename, so it needs write access to BOTH the
// executable and its directory, and the absence of it is what breaks updates
// silently, one permission error per cycle (#377). There the effective-writability
// question is the right one, because it is literally what the updater will
// attempt.
func WritabilityPreflight(exePath string, canWrite func(path string) bool) string {
	return writabilityPreflightFor(runtime.GOOS, exePath, canWrite, statOwnership)
}

// writabilityPreflightFor is the decision with the OS and both probes injected,
// so every branch is exercised on every runner rather than only on the platform
// it guards.
func writabilityPreflightFor(
	goos, exePath string,
	canWrite func(path string) bool,
	owner func(path string) (ownership, error),
) string {
	if exePath == "" {
		return ""
	}

	if goos == "linux" {
		o, err := owner(exePath)
		if err != nil {
			// Cannot tell. Saying nothing is right: a warning we cannot
			// substantiate is the thing this function was just fixed for.
			return ""
		}
		if !o.exposedToNonRoot() {
			return ""
		}
		return fmt.Sprintf(
			"the agent binary %s can be modified by a non-root account (owner uid %d, mode %04o); "+
				"whoever compromises the agent could replace what systemd executes and survive a restart. "+
				"Re-install so the binary is root-owned (sudo senhub-agent install), or install it through "+
				"the package manager.",
			exePath, o.uid, o.mode.Perm())
	}

	dir := filepath.Dir(exePath)
	if canWrite(exePath) && canWrite(dir) {
		return ""
	}
	return fmt.Sprintf(
		"auto_update.enabled is set but the running binary %s is not writable by this process; "+
			"in-process self-update will fail every cycle. Re-install so the binary is staged in a "+
			"writable directory, or use operator-driven 'senhub-agent update'.",
		exePath)
}

// CheckBinaryReplaceable runs WritabilityPreflight against the current
// executable. Returns "" when the layout is correct for this platform.
func CheckBinaryReplaceable() string {
	exe, err := os.Executable()
	if err != nil {
		return ""
	}
	return WritabilityPreflight(exe, pathWritable)
}
