// Pure rendering logic for the refresh-unit command. Kept free of build
// tags so the unit-file contract (User/ExecStart preservation, section
// placement) is testable on every development platform; the Linux-only
// side effects (reading /etc, daemon-reload) live in refresh_unit_linux.go.
package app

import (
	"strings"
)

// managedBinaryUnitPath is the staged managed-binary path as it must appear
// in a systemd ExecStart line: always a forward-slash Linux path, independent
// of the build platform's separator. filepath.Join would yield backslashes on
// a Windows test runner and corrupt the unit contract.
func managedBinaryUnitPath() string { return managedBinaryDir + "/senhub-agent" }

// refreshedUnit renders the unit refresh-unit writes over the installed
// one. It is the packaged hardened unit with three things reconciled
// against what is already on disk:
//
//   - User=/Group= are preserved: a legacy root install stays root
//     instead of being switched to a possibly missing senhub user
//     (217/USER crash loop, #575).
//   - A non-canonical ExecStart whose binary still exists is preserved
//     verbatim (with its WorkingDirectory=): CLI installs render their
//     own ExecStart (custom binary path, --config-path, flags) and a
//     refresh must not silently repoint them at the packaging path
//     (#396).
//   - A non-canonical ExecStart whose binary is gone (e.g. an installer
//     invoked from /tmp, #576) is repointed at the staged managed
//     binary while keeping its arguments, so refresh-unit remains the
//     documented repair for a 203/EXEC unit.
//
// binaryExists abstracts the filesystem check so the decision logic is
// unit-testable.
func refreshedUnit(installed string, binaryExists func(string) bool) string {
	unit := canonicalUnitForUser(installedServiceUser(installed))

	execLine, workDir := installedExecStart(installed)
	canonicalExec := packagedExecStartLine()
	if execLine == "" || execLine == canonicalExec {
		return unit
	}

	binPath, args := splitExecStartLine(execLine)
	if !binaryExists(unescapeSystemdPath(binPath)) {
		if args == "" {
			return unit
		}
		execLine = "ExecStart=" + managedBinaryUnitPath() + " " + args
		workDir = ""
	}

	lines := strings.Split(unit, "\n")
	out := make([]string, 0, len(lines)+1)
	for _, line := range lines {
		if strings.HasPrefix(line, "ExecStart=") {
			out = append(out, execLine)
			if workDir != "" {
				out = append(out, workDir)
			}
			continue
		}
		out = append(out, line)
	}
	return strings.Join(out, "\n")
}

// installedServiceUser reads the User= directive from the installed unit
// and returns the user the refreshed unit must keep running as. A unit
// with no User= (or User=root) is a legacy root install: refreshing it
// must NOT switch it to the senhub user, because that user may not exist
// and the root-owned binary in /usr/local/bin cannot be re-staged by a
// unit rewrite alone (#575). Anything else is preserved verbatim.
func installedServiceUser(unit string) string {
	for _, line := range strings.Split(unit, "\n") {
		trimmed := strings.TrimSpace(line)
		if v, ok := strings.CutPrefix(trimmed, "User="); ok {
			user := strings.TrimSpace(v)
			if user == "" {
				return rootServiceUser
			}
			return user
		}
	}
	return rootServiceUser
}

// canonicalUnitForUser returns the packaged hardened unit with its
// User=/Group= directives set to the given service user. For the default
// senhub user this is packagedSystemdUnit unchanged; for a root install
// it yields the same hardened directives but User=root/Group=root, so a
// legacy root agent keeps starting after a refresh.
func canonicalUnitForUser(serviceUser string) string {
	if serviceUser == rootServiceUser {
		return canonicalRootUnit()
	}
	if serviceUser == defaultServiceUser {
		return packagedSystemdUnit
	}
	lines := strings.Split(packagedSystemdUnit, "\n")
	out := make([]string, len(lines))
	for i, line := range lines {
		switch {
		case strings.HasPrefix(line, "User="):
			out[i] = "User=" + serviceUser
		case strings.HasPrefix(line, "Group="):
			out[i] = "Group=" + serviceUser
		default:
			out[i] = line
		}
	}
	return strings.Join(out, "\n")
}

// canonicalRootUnit renders rootSystemdScript — the very template
// `install --user root` hands to kardianos/service — as a concrete unit
// file, so a refresh produces what the install produced instead of a
// second, disagreeing "canonical root unit" (#689).
//
// Refreshing a root install used to yield the hardened template with
// User=root, which carries CapabilityBoundingSet=/AmbientCapabilities=
// (all capabilities dropped). A refresh therefore stripped a --user root
// install of the capabilities that are the whole reason to choose it —
// raw ICMP sockets, privileged ports — and did it silently, since the
// unit still started.
//
// Only two lines of the template are not literal unit syntax: ExecStart
// carries kardianos placeholders, replaced here by the packaged
// ExecStart so refreshedUnit can splice the installed one over it as it
// does for every other user; and the {{if}}-wrapped WorkingDirectory,
// dropped for the same reason (refreshedUnit re-inserts the installed
// one alongside a non-canonical ExecStart).
func canonicalRootUnit() string {
	lines := strings.Split(rootSystemdScript, "\n")
	out := make([]string, 0, len(lines))
	for _, line := range lines {
		switch {
		case strings.HasPrefix(line, "ExecStart="):
			out = append(out, packagedExecStartLine())
		case strings.HasPrefix(line, "{{if .WorkingDirectory}}"):
			continue
		default:
			out = append(out, line)
		}
	}
	return strings.Join(out, "\n")
}

// installedExecStart returns the first ExecStart= and WorkingDirectory=
// lines of the installed unit (trimmed, prefix included), empty when
// absent.
func installedExecStart(unit string) (execStart, workingDirectory string) {
	for _, line := range strings.Split(unit, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "ExecStart=") && execStart == "" {
			execStart = trimmed
		}
		if strings.HasPrefix(trimmed, "WorkingDirectory=") && workingDirectory == "" {
			workingDirectory = trimmed
		}
	}
	return execStart, workingDirectory
}

// packagedExecStartLine returns the ExecStart= line of the packaged unit.
func packagedExecStartLine() string {
	for _, line := range strings.Split(packagedSystemdUnit, "\n") {
		if strings.HasPrefix(line, "ExecStart=") {
			// TrimSpace so a CRLF-checked-out unit (Windows runner) matches
			// installedExecStart, which trims each line — otherwise the stray
			// \r makes a canonical unit look non-canonical.
			return strings.TrimSpace(line)
		}
	}
	return ""
}

// splitExecStartLine splits an ExecStart= line into the binary path (its
// first token) and the remaining argument string.
func splitExecStartLine(line string) (binPath, args string) {
	cmd := strings.TrimPrefix(line, "ExecStart=")
	binPath, args, _ = strings.Cut(strings.TrimSpace(cmd), " ")
	return binPath, strings.TrimSpace(args)
}

// unescapeSystemdPath reverses the \x20 space escaping
// kardianos/service's cmdEscape applies to the ExecStart binary path, so
// the existence check sees the real filesystem path.
func unescapeSystemdPath(path string) string {
	return strings.ReplaceAll(path, `\x20`, " ")
}

// diffLines returns a slice of display lines representing the diff between
// old and new. Each line is prefixed with "  " (unchanged), "- " (removed),
// or "+ " (added). The algorithm is a simple O(n) prefix/suffix trim + block
// diff, adequate for the ~50-line unit files this command handles.
func diffLines(old, new string) []string {
	oldLines := strings.Split(old, "\n")
	newLines := strings.Split(new, "\n")

	// Find the common prefix length.
	prefix := 0
	for prefix < len(oldLines) && prefix < len(newLines) && oldLines[prefix] == newLines[prefix] {
		prefix++
	}

	// Find the common suffix length (not overlapping the prefix).
	suffix := 0
	for suffix < len(oldLines)-prefix && suffix < len(newLines)-prefix &&
		oldLines[len(oldLines)-1-suffix] == newLines[len(newLines)-1-suffix] {
		suffix++
	}

	// The differing regions.
	oldMid := oldLines[prefix : len(oldLines)-suffix]
	newMid := newLines[prefix : len(newLines)-suffix]

	if len(oldMid) == 0 && len(newMid) == 0 {
		return nil
	}

	var out []string
	// Leading context (up to 3 lines).
	ctxStart := prefix - 3
	if ctxStart < 0 {
		ctxStart = 0
	}
	for _, l := range oldLines[ctxStart:prefix] {
		out = append(out, "  "+l)
	}
	for _, l := range oldMid {
		out = append(out, "- "+l)
	}
	for _, l := range newMid {
		out = append(out, "+ "+l)
	}
	// Trailing context (up to 3 lines).
	ctxEnd := len(oldLines) - suffix + 3
	if ctxEnd > len(oldLines) {
		ctxEnd = len(oldLines)
	}
	for _, l := range oldLines[len(oldLines)-suffix : ctxEnd] {
		out = append(out, "  "+l)
	}
	return out
}
