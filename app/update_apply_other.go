//go:build !linux

package app

import (
	"fmt"
	"os"
)

func runUpdateApply([]string) int {
	fmt.Fprintln(os.Stderr, "update-apply is only used on Linux deb and rpm installs")
	return 2
}
