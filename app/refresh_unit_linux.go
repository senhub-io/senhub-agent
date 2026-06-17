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
// Note: this command always writes packagedSystemdUnit (the packaging-canonical
// unit with hardcoded /usr/bin/senhub-agent ExecStart). For CLI-installed agents
// (kardianos/service renders ExecStart from the binary path) the diff will show
// the packaging ExecStart replacing the CLI-rendered one. That is intentional
// for .deb/.rpm installs. CLI-install-aware behaviour is tracked in #396.
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

	canonical := packagedSystemdUnit

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
