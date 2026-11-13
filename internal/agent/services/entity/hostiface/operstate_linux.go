//go:build linux

package hostiface

import (
	"os"
	"strconv"
	"strings"

	"senhub-agent.go/internal/agent/services/entity/operstate"
)

// readSysLink reads the interface's descriptive metadata from sysfs: the
// operational state plus the AT13 type/duplex/speed. A missing/unreadable or
// out-of-vocabulary operstate leaves OperState empty so the caller omits the
// attribute (absence is distinct from "unknown"); there is no fallback to the
// IFF_UP flag here — the administrative state is reported separately.
func readSysLink(name string) linkMeta {
	base := "/sys/class/net/" + name + "/"
	oper, _ := operstate.FromLinuxSysfs(readTrim(base + "operstate"))
	return linkMeta{
		OperState: oper,
		Duplex:    normDuplex(readTrim(base + "duplex")),
		Speed:     parseSpeedMbit(readTrim(base + "speed")),
		Type:      ifaceType(name),
	}
}

func readTrim(path string) string {
	b, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(b))
}

func normDuplex(v string) string {
	switch v {
	case "full", "half", "unknown":
		return v
	default:
		return ""
	}
}

// parseSpeedMbit converts the sysfs speed (Mbit/s, often -1 when the link is
// down) to bit/s. Non-positive or unparseable → 0 (omitted).
func parseSpeedMbit(v string) int64 {
	n, err := strconv.ParseInt(v, 10, 64)
	if err != nil || n <= 0 {
		return 0
	}
	return n * 1_000_000
}

// ifaceType classifies the interface as wireless / virtual / physical from
// sysfs. Loopback is already filtered out before this is called.
func ifaceType(name string) string {
	if dirExists("/sys/class/net/"+name+"/wireless") || pathExists("/sys/class/net/"+name+"/phy80211") {
		return "wireless"
	}
	if pathExists("/sys/devices/virtual/net/" + name) {
		return "virtual"
	}
	return "physical"
}

func pathExists(p string) bool {
	_, err := os.Lstat(p)
	return err == nil
}

func dirExists(p string) bool {
	fi, err := os.Stat(p)
	return err == nil && fi.IsDir()
}
