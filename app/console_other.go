//go:build !windows

package app

import (
	"os/exec"
	"runtime"
)

// canRelaunchElevated is Windows-only: elsewhere the operator uses sudo.
func canRelaunchElevated() bool { return false }

func elevatedConsoleURL(string) (string, error) { return "", errNoElevation }

// openBrowser uses the desktop opener when one exists; on a headless
// server the printed address is the deliverable and a missing opener is
// not an error worth failing on.
func openBrowser(url string) error {
	opener := "xdg-open"
	if runtime.GOOS == "darwin" {
		opener = "open"
	}
	if _, err := exec.LookPath(opener); err != nil {
		return nil
	}
	return exec.Command(opener, url).Start()
}
