//go:build !linux && !windows

package common

// readHardwareNameplate has no portable implementation on darwin/other
// platforms (Linux uses sysfs, Windows uses WMI). The host nameplate degrades
// gracefully to the OS/CPU fields gopsutil provides cross-platform.
func readHardwareNameplate() hardwareNameplate {
	return hardwareNameplate{}
}

// readChassisType has no portable source on darwin/other; 0 → the caller
// derives "vm" (when virtualized) or "other".
func readChassisType() int { return 0 }

// readVirtualizationFallback has no source on darwin/other; gopsutil's
// detection stands.
func readVirtualizationFallback() string { return "" }
