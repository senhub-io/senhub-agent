//go:build linux || windows

package common

import "strings"

// classifyVirtFromDMI maps the SMBIOS/DMI system identity (vendor, product,
// family and BIOS vendor joined and lower-cased) to the AT11 host.virtualization
// enum, the way systemd-detect-virt keys off DMI strings. It backs the
// fallback when gopsutil reports "none" on a cloud guest. Returns "" when
// nothing matches, so the primary (gopsutil) detection keeps the final say.
func classifyVirtFromDMI(sig string) string {
	switch {
	case strings.TrimSpace(sig) == "":
		return ""
	case strings.Contains(sig, "vmware"):
		return "vmware"
	case strings.Contains(sig, "xen"):
		return "xen"
	case strings.Contains(sig, "virtualbox"), strings.Contains(sig, "innotek"):
		return "virtualbox"
	case strings.Contains(sig, "qemu"), strings.Contains(sig, "seabios"),
		strings.Contains(sig, "openstack"), strings.Contains(sig, "nova"),
		strings.Contains(sig, "kvm"), strings.Contains(sig, "bochs"):
		return "kvm"
	case strings.Contains(sig, "microsoft") && strings.Contains(sig, "virtual"):
		return "hyperv"
	case strings.Contains(sig, "virtual machine"), strings.Contains(sig, "virtual platform"):
		return "unknown"
	}
	return ""
}
