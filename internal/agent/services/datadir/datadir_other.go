//go:build !windows

package datadir

// Secure is a no-op off Windows, where directory modes are enforced by the kernel.
func Secure(dir string, log Logger) error { return nil }
