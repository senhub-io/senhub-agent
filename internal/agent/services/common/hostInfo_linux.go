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

// isDMIPlaceholder rejects the firmware default strings OEMs ship, so the
// nameplate never carries "To Be Filled By O.E.M." as a vendor/model/serial.
func isDMIPlaceholder(v string) bool {
	switch strings.ToLower(v) {
	case "", "to be filled by o.e.m.", "to be filled by o.e.m",
		"system manufacturer", "system product name", "system serial number",
		"default string", "not specified", "not applicable",
		"none", "unknown", "n/a", "o.e.m.", "oem":
		return true
	}
	return false
}
