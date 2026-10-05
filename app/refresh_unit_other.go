//go:build !linux

package app

import (
	"fmt"
	"os"

	"senhub-agent.go/internal/cliexit"
)

func runRefreshUnit() {
	fmt.Fprintln(os.Stderr, "refresh-unit is only supported on Linux (systemd)")
	os.Exit(cliexit.Failure)
}
