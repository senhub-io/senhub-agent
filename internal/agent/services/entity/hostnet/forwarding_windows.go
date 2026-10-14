//go:build windows

package hostnet

import (
	"unsafe"

	"golang.org/x/sys/windows"
)

var procGetIpStatisticsEx = windows.NewLazySystemDLL("iphlpapi.dll").NewProc("GetIpStatisticsEx")

// mibIPForwarding is MIB_IPSTATS.dwForwarding: 1 forwarding, 2 not.
const mibIPForwarding = 1

// platformForwarding asks the IP helper API, per address family, whether
// the stack forwards packets between interfaces.
func platformForwarding() (v4, v6 string) {
	return ipStatsForwarding(windows.AF_INET), ipStatsForwarding(windows.AF_INET6)
}

func ipStatsForwarding(family uint16) string {
	// MIB_IPSTATS is 23 DWORDs; dwForwarding is the first.
	var stats [23]uint32
	r, _, _ := procGetIpStatisticsEx.Call(uintptr(unsafe.Pointer(&stats[0])), uintptr(family))
	if r != 0 {
		return ""
	}
	if stats[0] == mibIPForwarding {
		return "1"
	}
	return "0"
}
