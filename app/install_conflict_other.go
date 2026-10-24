//go:build !windows

package app

// registeredServiceImagePath is Windows-only: on Linux a second install
// reconciles the one systemd unit in place.
func registeredServiceImagePath() string { return "" }

func hostMsiManaged() bool { return false }
