//go:build !linux

package common

// readHardwareNameplate has no portable non-Linux implementation yet (Windows
// WMI and darwin sysctl/ioreg are a later increment). The host nameplate
// degrades gracefully to the OS/CPU fields gopsutil provides cross-platform.
func readHardwareNameplate() hardwareNameplate {
	return hardwareNameplate{}
}
