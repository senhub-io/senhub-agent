//go:build linux

package hostnet

import (
	"os"
	"strings"
)

// platformForwarding reads the kernel's forwarding switches, "1", "0" or
// "" when unreadable. IPv6 forwarding can also be set per interface; "all"
// is the global switch, so a host forwarding on one interface only reads as
// not routing, which errs on the safe side (a named gap, never a false path).
func platformForwarding() (v4, v6 string) {
	return readSysctl("/proc/sys/net/ipv4/ip_forward"),
		readSysctl("/proc/sys/net/ipv6/conf/all/forwarding")
}

func readSysctl(path string) string {
	b, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(b))
}
