//go:build !linux

package main

import (
	"fmt"
	"os"
)

func main() {
	fmt.Fprintln(os.Stderr, "senhub-ebpf runs on Linux only")
	os.Exit(3)
}
