package hostdep

import (
	"net/netip"
	"os"
	"runtime"
	"strconv"
	"strings"
)

const (
	linuxPortRangeFile = "/proc/sys/net/ipv4/ip_local_port_range"

	// Linux defaults when the kernel setting cannot be read.
	linuxEphemeralLow  = 32768
	linuxEphemeralHigh = 60999

	// IANA dynamic range, the default on Windows and macOS.
	ianaEphemeralLow  = 49152
	ianaEphemeralHigh = 65535
)

// osEphemeralRange returns the range the OS draws client source ports from.
func osEphemeralRange() (low, high uint32) {
	if runtime.GOOS == "linux" {
		if raw, err := os.ReadFile(linuxPortRangeFile); err == nil {
			if lo, hi, ok := parsePortRange(string(raw)); ok {
				return lo, hi
			}
		}
		return linuxEphemeralLow, linuxEphemeralHigh
	}
	return ianaEphemeralLow, ianaEphemeralHigh
}

func parsePortRange(raw string) (low, high uint32, ok bool) {
	fields := strings.Fields(raw)
	if len(fields) != 2 {
		return 0, 0, false
	}
	lo, err1 := strconv.ParseUint(fields[0], 10, 32)
	hi, err2 := strconv.ParseUint(fields[1], 10, 32)
	if err1 != nil || err2 != nil || lo > hi || hi > 65535 {
		return 0, 0, false
	}
	return uint32(lo), uint32(hi), true
}

// loopbackPeer reports whether addr is host-scoped (127.0.0.0/8 or ::1).
func loopbackPeer(addr string) bool {
	a, err := netip.ParseAddr(addr)
	if err != nil {
		return false
	}
	return a.Unmap().IsLoopback()
}
