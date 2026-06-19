//go:build linux

package hostiface

import (
	"os"
	"strings"
)

// sysOperState reads the interface's carrier/operational state from sysfs and
// normalizes it to the up/down state-key vocabulary. "" means the precise state
// is unknown (the caller falls back to the administrative IFF_UP flag).
func sysOperState(name string) string {
	b, err := os.ReadFile("/sys/class/net/" + name + "/operstate")
	if err != nil {
		return ""
	}
	switch strings.TrimSpace(string(b)) {
	case "up":
		return "up"
	case "down", "lowerlayerdown", "dormant":
		return "down"
	default: // unknown, notpresent, testing, empty → defer to flags
		return ""
	}
}
