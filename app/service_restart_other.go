//go:build !linux

package app

// restartServiceAfterUpdate has nothing to do outside Linux: the MSI
// restarts the Windows service itself.
func restartServiceAfterUpdate() (string, error) { return "", nil }
