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

// runRefreshUnit compares the refreshed unit (see refreshedUnit) with the
// installed one, shows the diff, and — after confirmation unless --yes is
// passed — overwrites the installed file and runs systemctl daemon-reload.
//
// The refreshed unit brings the hardening directives up to date while
// preserving what the install already validated: the User=/Group= it runs
// as (#575) and a CLI-rendered ExecStart whose binary still exists (#396).
// An ExecStart whose binary vanished (installer invoked from /tmp, #576)
// is repointed at the staged managed binary.
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

	serviceUser := installedServiceUser(string(installed))
	refreshed := refreshedUnit(string(installed), func(path string) bool {
		info, statErr := os.Stat(path)
		return statErr == nil && !info.IsDir()
	})

	// A non-root target user must exist before systemd validates the
	// rewritten unit — mirror what `install` does so refresh-unit never
	// produces a unit whose User= cannot be resolved (#575).
	if serviceUser != rootServiceUser {
		if userErr := ensureServiceUser(serviceUser); userErr != nil {
			fmt.Fprintf(os.Stderr, "ensuring service user %q exists: %v\n", serviceUser, userErr)
			os.Exit(1)
		}
	}

	if string(installed) == refreshed {
		fmt.Println("Unit is up to date — no changes.")
		return
	}

	lines := diffLines(string(installed), refreshed)
	fmt.Println("--- installed")
	fmt.Println("+++ refreshed (this binary)")
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

	if err := os.WriteFile(installedUnitPath, []byte(refreshed), 0644); err != nil {
		fmt.Fprintf(os.Stderr, "writing unit file: %v\n", err)
		os.Exit(1)
	}

	if out, err := exec.Command("systemctl", "daemon-reload").CombinedOutput(); err != nil {
		fmt.Fprintf(os.Stderr, "systemctl daemon-reload: %v (%s)\n", err, strings.TrimSpace(string(out)))
		os.Exit(1)
	}

	fmt.Println("Unit updated. Run 'senhub-agent restart' to apply the new unit to the running service.")
}
