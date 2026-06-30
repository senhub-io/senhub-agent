//go:build linux

package common

import (
	"os"
	"strconv"
	"strings"
)

// readChassisType reads the raw SMBIOS chassis-type code from sysfs (0 when
// unreadable). The caller normalizes it to the host.chassis.type enum.
func readChassisType() int {
	b, err := os.ReadFile("/sys/class/dmi/id/chassis_type")
	if err != nil {
		return 0
	}
	n, err := strconv.Atoi(strings.TrimSpace(string(b)))
	if err != nil {
		return 0
	}
	return n
}

// readVirtualizationFallback classifies the hypervisor from the sysfs DMI
// strings when gopsutil reports "none". gopsutil already reads the hypervisor
// signatures so this rarely fires, but it catches KVM/cloud guests gopsutil
// misses — the same classifier the Windows reader uses, for parity.
func readVirtualizationFallback() string {
	sig := strings.ToLower(strings.Join([]string{
		readDMI("/sys/class/dmi/id/sys_vendor"),
		readDMI("/sys/class/dmi/id/product_name"),
		readDMI("/sys/class/dmi/id/product_family"),
		readDMI("/sys/class/dmi/id/bios_vendor"),
	}, " "))
	return classifyVirtFromDMI(sig)
}

// readHardwareNameplate reads the host's DMI system identity from sysfs.
// product_serial requires root (the agent's run path has it); any field that is
// unreadable or carries a well-known firmware placeholder is dropped.
func readHardwareNameplate() hardwareNameplate {
	return hardwareNameplate{
		vendor: readDMI("/sys/class/dmi/id/sys_vendor"),
		model:  readDMI("/sys/class/dmi/id/product_name"),
		serial: readDMI("/sys/class/dmi/id/product_serial"),
	}
}

func readDMI(path string) string {
	b, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	v := strings.TrimSpace(string(b))
	if isDMIPlaceholder(v) {
		return ""
	}
	return v
}
