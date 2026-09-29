//go:build linux

package network

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// sysClassNet is where the kernel publishes what a link negotiated. The
// native Zabbix agent's Linux template reads the same files through
// vfs.file.contents, which is why a host monitored by it shows an
// interface speed and one monitored by us did not.
const sysClassNet = "/sys/class/net"

// interfaceLink reads the negotiated speed and the operational state of
// one interface. A virtual or unplugged link reports no speed, which is
// absence rather than zero, so it is reported as such.
func interfaceLink(name string) linkState {
	out := linkState{}
	if raw, err := os.ReadFile(filepath.Join(sysClassNet, name, "speed")); err == nil { // #nosec G304 - kernel path, name comes from the counter enumeration
		if mbits, convErr := strconv.ParseFloat(strings.TrimSpace(string(raw)), 64); convErr == nil && mbits > 0 {
			out.SpeedBits, out.HaveSpeed = mbits*1e6, true
		}
	}
	if raw, err := os.ReadFile(filepath.Join(sysClassNet, name, "operstate")); err == nil { // #nosec G304 - kernel path
		state := strings.TrimSpace(string(raw))
		if state != "" && state != "unknown" {
			out.HaveUp = true
			if state == "up" {
				out.Up = 1
			}
		}
	}
	if !out.HaveUp {
		return fallbackLinkState(name, out)
	}
	return out
}
