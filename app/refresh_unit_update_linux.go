//go:build linux

package app

import (
	"os"
	"os/exec"
)

// refreshUnitAfterUpdate is the update hook that refreshes the unit; see
// unitRefresher.
func refreshUnitAfterUpdate(newBinary string) (string, error) {
	return unitRefresher{
		unitPath: loadedUnitPath,
		read:     os.ReadFile,
		run: func(bin string) ([]byte, error) {
			return exec.Command(bin, "refresh-unit", "--yes").CombinedOutput() // #nosec G204 - the binary update just installed
		},
	}.refresh(newBinary)
}
