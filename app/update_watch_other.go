//go:build !windows

package app

import (
	"fmt"
	"os"
)

func runUpdateWatch(args []string) int {
	fmt.Fprintln(os.Stderr, "update-watch is only used on Windows")
	return 2
}
