//go:build !linux

package app

import (
	"fmt"
	"os"
)

func runRefreshUnit() {
	fmt.Fprintln(os.Stderr, "refresh-unit is only supported on Linux (systemd)")
	os.Exit(1)
}
