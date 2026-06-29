//go:build linux

package app

import (
	"bufio"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"strings"
)

// installedUnitPath is where kardianos/service and the .deb/.rpm packages
// both place the systemd unit for a system (non-user) service named
// "senhub-agent". Writing here requires root.
const installedUnitPath = "/etc/systemd/system/senhub-agent.service"

// runRefreshUnit compares the embedded packaged unit with the installed unit,
// shows the diff, and — after confirmation unless --yes is passed — overwrites
// the installed file and runs systemctl daemon-reload.
//
// Note: this command writes the packaging-canonical unit (hardcoded
// /usr/bin/senhub-agent ExecStart) but keeps the User=/Group= the
// installed unit already runs as — a legacy root install is refreshed
// as User=root, not silently switched to User=senhub (which would
// 217/USER crash-loop when that user does not exist, #575). For
// CLI-installed agents (kardianos/service renders ExecStart from the
// binary path) the diff will show the packaging ExecStart replacing the
// CLI-rendered one. That is intentional for .deb/.rpm installs.
// CLI-install-aware behaviour is tracked in #396.
func runRefreshUnit() {
	fs := flag.NewFlagSet("refresh-unit", flag.ExitOnError)
	yes := fs.Bool("yes", false, "apply without confirmation prompt")
	if err := fs.Parse(os.Args[2:]); err != nil {
		fmt.Fprintf(os.Stderr, "refresh-unit: %v\n", err)
		os.Exit(1)
	}

	installed, err := os.ReadFile(installedUnitPath)
	if err != nil {
		if os.IsNotExist(err) {
			fmt.Fprintf(os.Stderr, "no unit found at %s; run 'senhub-agent install' first\n", installedUnitPath)
			os.Exit(1)
		}
		fmt.Fprintf(os.Stderr, "reading installed unit: %v\n", err)
		os.Exit(1)
	}

	// Preserve the installed unit's service identity. The packaged unit
	// hardcodes User=senhub/Group=senhub; blindly writing it over a
	// legacy root install (no senhub user) makes systemd fail with
	// 217/USER and crash-loop (#575). Refresh the hardening directives
	// while keeping the User=/Group= the install already validated.
	serviceUser := installedServiceUser(string(installed))
	canonical := canonicalUnitForUser(serviceUser)

	// A non-root target user must exist before systemd validates the
	// rewritten unit — mirror what `install` does so refresh-unit never
	// produces a unit whose User= cannot be resolved.
	if serviceUser != rootServiceUser {
		if userErr := ensureServiceUser(serviceUser); userErr != nil {
			fmt.Fprintf(os.Stderr, "ensuring service user %q exists: %v\n", serviceUser, userErr)
			os.Exit(1)
		}
	}

	if string(installed) == canonical {
		fmt.Println("Unit is up to date — no changes.")
		return
	}

	lines := diffLines(string(installed), canonical)
	fmt.Println("--- installed")
	fmt.Println("+++ packaged (this binary)")
	for _, l := range lines {
		fmt.Println(l)
	}
	fmt.Println()

	if !*yes {
		fmt.Print("Apply changes? [y/N] ")
		scanner := bufio.NewScanner(os.Stdin)
		if !scanner.Scan() {
			fmt.Println("aborted")
			return
		}
		answer := strings.TrimSpace(scanner.Text())
		if answer != "y" && answer != "Y" {
			fmt.Println("aborted")
			return
		}
	}

	if err := os.WriteFile(installedUnitPath, []byte(canonical), 0644); err != nil {
		fmt.Fprintf(os.Stderr, "writing unit file: %v\n", err)
		os.Exit(1)
	}

	if out, err := exec.Command("systemctl", "daemon-reload").CombinedOutput(); err != nil {
		fmt.Fprintf(os.Stderr, "systemctl daemon-reload: %v (%s)\n", err, strings.TrimSpace(string(out)))
		os.Exit(1)
	}

	fmt.Println("Unit updated. Run 'senhub-agent restart' to apply the new unit to the running service.")
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
