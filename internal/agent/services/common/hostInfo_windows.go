//go:build windows

package common

import (
	"strings"
	"sync"

	"github.com/digitalocean/go-smbios/smbios"
	"golang.org/x/sys/windows/registry"
)

// winNameplate is the host's SMBIOS system identity, read once from the raw
// firmware table (GetSystemFirmwareTable) so vendor/model/serial/chassis come
// from the same source dmidecode parses on Linux. Windows has no
// /sys/class/dmi mirror, and the registry mirror under HKLM\HARDWARE\...\BIOS
// omits both the serial and the chassis type — only the firmware table has them.
type winNameplate struct {
	vendor, model, serial, family, biosVendor string
	chassisCode                               int
}

var (
	winNPOnce sync.Once
	winNP     winNameplate
)

// windowsNameplate reads and memoizes the SMBIOS nameplate. The values are
// static for the life of the process, so the firmware table is parsed once.
func windowsNameplate() winNameplate {
	winNPOnce.Do(func() { winNP = readWindowsSMBIOS() })
	return winNP
}

// readWindowsSMBIOS parses the SMBIOS BIOS (type 0), System (type 1) and
// Chassis (type 3) structures. It falls back to the registry mirror for the
// vendor/model/family signatures when the firmware table can't be read.
func readWindowsSMBIOS() winNameplate {
	np := winNameplate{}
	if rc, _, err := smbios.Stream(); err == nil {
		defer rc.Close()
		if structs, err := smbios.NewDecoder(rc).Decode(); err == nil {
			for _, s := range structs {
				switch s.Header.Type {
				case 0: // BIOS Information
					np.biosVendor = smbiosString(s, 0)
				case 1: // System Information
					np.vendor = cleanNameplate(smbiosString(s, 0))
					np.model = cleanNameplate(smbiosString(s, 1))
					np.serial = cleanNameplate(smbiosString(s, 3))
					np.family = smbiosString(s, 22)
				case 3: // System Enclosure / Chassis
					if len(s.Formatted) > 1 {
						np.chassisCode = int(s.Formatted[1] & 0x7f)
					}
				}
			}
		}
	}
	if np.vendor == "" {
		np.vendor = readBIOSRegistry("SystemManufacturer")
	}
	if np.model == "" {
		np.model = readBIOSRegistry("SystemProductName")
	}
	if np.family == "" {
		np.family = readBIOSRegistry("SystemFamily")
	}
	if np.biosVendor == "" {
		np.biosVendor = readBIOSRegistry("BIOSVendor")
	}
	return np
}

// smbiosString dereferences the 1-based SMBIOS string index held at
// Formatted[i] (Formatted[0] is the byte at structure offset 0x04). Index 0
// means "no string".
func smbiosString(s *smbios.Structure, i int) string {
	if i >= len(s.Formatted) {
		return ""
	}
	idx := int(s.Formatted[i])
	if idx == 0 || idx > len(s.Strings) {
		return ""
	}
	return strings.TrimSpace(s.Strings[idx-1])
}

func cleanNameplate(v string) string {
	if isDMIPlaceholder(v) {
		return ""
	}
	return v
}

// readHardwareNameplate returns the SMBIOS system identity
// (vendor/model/serial) read from the firmware table.
func readHardwareNameplate() hardwareNameplate {
	np := windowsNameplate()
	return hardwareNameplate{vendor: np.vendor, model: np.model, serial: np.serial}
}

// readChassisType returns the raw SMBIOS chassis-type code (type 3); the caller
// maps it to the host.chassis.type enum and derives "vm" when virtualized.
func readChassisType() int { return windowsNameplate().chassisCode }

// readVirtualizationFallback classifies the hypervisor from the SMBIOS system
// identity when gopsutil reports "none" — its Windows detection misses cloud
// guests (an OpenStack/KVM VM reads "none").
func readVirtualizationFallback() string {
	np := windowsNameplate()
	sig := strings.ToLower(strings.Join([]string{np.vendor, np.model, np.family, np.biosVendor}, " "))
	return classifyVirtFromDMI(sig)
}

// readBIOSRegistry reads a single value from the registry mirror of DMI,
// the fallback source for the vendor/model/family signatures.
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
