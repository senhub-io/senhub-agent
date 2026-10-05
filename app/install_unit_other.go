//go:build !linux

package app

// Outside Linux the service is the Windows registration (or a macOS
// launchd entry): there is no unit file to compare, and the registration
// carries the one binary that was invoked.

func serviceEnabledOnHost() bool { return true }

func installUnitCurrent(string, []string) bool { return true }

func reconcileInstalledServiceOnHost(string, []string) ([]string, error) { return nil, nil }
