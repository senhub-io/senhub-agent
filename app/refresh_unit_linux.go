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

	// Move the binary BEFORE rewriting the unit. The refreshed unit points at
	// the system path, so if that file is missing the service would come back
	// as 203/EXEC — a refresh that takes a working host down. Doing it first
	// means a failure here aborts with the old, working unit still in place.
	if err := migrateLegacyBinary(string(installed)); err != nil {
		fmt.Fprintf(os.Stderr, "migrating the binary to %s: %v\n", systemBinaryDir, err)
		fmt.Fprintln(os.Stderr, "The unit was NOT changed; the service is untouched.")
		os.Exit(1)
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

// migrateLegacyBinary brings a pre-0.5.4 host to the single-binary layout so
// the refreshed unit has something to exec (#794).
//
// Such a host runs ExecStart=/var/lib/senhub-agent/bin/senhub-agent, a
// service-user-owned copy. The refreshed unit points at the root-owned system
// path instead — and on a host installed by running the installer from /tmp
// (#576) that path may hold nothing at all, because the copy the operator ran
// is long gone. Repointing blindly would turn a working service into 203/EXEC.
//
// So the legacy binary is promoted to the system path first, root-owned, and
// only then is the old directory removed. A no-op on any host that is not in
// the legacy layout.
func migrateLegacyBinary(installedUnit string) error {
	execLine, _ := installedExecStart(installedUnit)
	binPath, _ := splitExecStartLine(execLine)
	if unescapeSystemdPath(binPath) != legacyManagedBinaryPath() {
		return nil
	}

	legacy := legacyManagedBinaryPath()
	if _, err := os.Stat(legacy); err != nil {
		// The unit names it but it is gone: nothing to promote. The refreshed
		// unit still repoints, which is the documented 203/EXEC repair.
		return nil
	}

	target := systemBinaryUnitPath()
	if _, err := os.Stat(target); err != nil {
		fmt.Printf("Installing the agent binary at %s (it was only present under %s)\n", target, legacyManagedBinaryDir)
		if _, err := installSystemBinary(legacy); err != nil {
			return err
		}
	}

	if err := removeLegacyManagedBinary(); err != nil {
		// Not fatal: the host is correct once the unit points at the system
		// binary. Say so rather than failing a migration over cleanup.
		fmt.Fprintf(os.Stderr, "Warning: %v\n", err)
		return nil
	}
	fmt.Printf("Removed the previous service-owned copy under %s\n", legacyManagedBinaryDir)
	return nil
}
