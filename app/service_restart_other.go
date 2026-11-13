//go:build !linux && !windows

package app

// restartServiceAfterUpdate has nothing to do outside Linux and Windows:
// the MSI restarts the Windows service itself, and darwin is a dev target.
func restartServiceAfterUpdate(bool) (string, error) { return "", nil }
