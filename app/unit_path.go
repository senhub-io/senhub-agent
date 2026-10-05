package app

import "strings"

// unitSearchPaths are the places a senhub-agent.service unit lives, in the
// order systemd itself prefers them: /etc (written by `senhub-agent install`)
// shadows the vendor directories the .deb/.rpm packages use.
var unitSearchPaths = []string{
	"/etc/systemd/system/senhub-agent.service",
	"/usr/lib/systemd/system/senhub-agent.service",
	"/lib/systemd/system/senhub-agent.service",
}

// resolveUnitPath returns the unit file systemd loads for the service.
// fragmentPath is what `systemctl show -p FragmentPath` reported ("" when
// systemd could not be asked or the unit is unknown); exists tells whether a
// file is there. A reported path wins when the file is readable, because it
// is the one systemd actually runs; otherwise the first known location that
// exists. "" when no unit is found.
func resolveUnitPath(fragmentPath string, exists func(string) bool) string {
	if fragmentPath != "" && exists(fragmentPath) {
		return fragmentPath
	}
	for _, p := range unitSearchPaths {
		if exists(p) {
			return p
		}
	}
	return ""
}

// unitOwnedByPackage reports whether the unit sits in a vendor directory the
// package manager owns. Rewriting it by hand would be undone by the next
// package upgrade, so the package is what updates it.
func unitOwnedByPackage(path string) bool {
	return strings.HasPrefix(path, "/usr/lib/systemd/") || strings.HasPrefix(path, "/lib/systemd/")
}
