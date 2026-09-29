package app

import (
	"fmt"
	"os"
)

// installedHint tells the operator how to call the agent from now on. On
// Linux that is the installed system binary by its full path: sudo on the
// RHEL family (RHEL, AlmaLinux, Rocky) keeps /usr/local/bin out of its
// secure_path, so `sudo senhub-agent ...` is "command not found" there, and
// the installer's own path may be a download directory about to be removed.
func installedHint(installed string) string {
	if installed == "" {
		return fmt.Sprintf("\nYou can now start the service with:\n    %s start\n", os.Args[0])
	}
	return fmt.Sprintf("\nYou can now start the service with:\n    sudo %[1]s start\n"+
		"Call the agent by this path from now on (sudo does not search /usr/local/bin on every distribution):\n"+
		"    sudo %[1]s status\n", installed)
}
