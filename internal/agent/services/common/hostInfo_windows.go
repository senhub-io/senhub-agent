//go:build windows

package common

import (
	"strings"

	"golang.org/x/sys/windows/registry"
)

// readVirtualizationFallback has no extra source on Windows; gopsutil's
// detection stands.
func readVirtualizationFallback() string { return "" }

// readChassisType: the SMBIOS chassis type is exposed via WMI
// (Win32_SystemEnclosure.ChassisTypes) on Windows, not the registry, so it
// degrades to 0 here — the caller then derives "vm" (when virtualized) or
// "other". WMI population is a follow-up validated on a real Windows host.
func readChassisType() int { return 0 }

// readHardwareNameplate reads the host's SMBIOS system identity from the
// registry mirror of DMI (HKLM\HARDWARE\DESCRIPTION\System\BIOS). vendor and
// model are reliably present there; the SMBIOS serial is WMI-only
// (Win32_BIOS.SerialNumber) and is left empty rather than guessed.
func readHardwareNameplate() hardwareNameplate {
	return hardwareNameplate{
		vendor: readBIOSRegistry("SystemManufacturer"),
		model:  readBIOSRegistry("SystemProductName"),
	}
}

func readBIOSRegistry(value string) string {
	k, err := registry.OpenKey(registry.LOCAL_MACHINE, `HARDWARE\DESCRIPTION\System\BIOS`, registry.QUERY_VALUE)
	if err != nil {
		return ""
	}
	defer k.Close()
	v, _, err := k.GetStringValue(value)
	if err != nil {
		return ""
	}
	v = strings.TrimSpace(v)
	if isDMIPlaceholder(v) {
		return ""
	}
	return v
}
