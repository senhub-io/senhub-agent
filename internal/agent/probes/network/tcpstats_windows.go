//go:build windows

package network

import (
	"fmt"
	"syscall"
	"unsafe"

	"github.com/shirou/gopsutil/v3/net"
	"golang.org/x/sys/windows"

	"senhub-agent.go/internal/agent/services/netconn"
)

// mibTCPStats mirrors the Windows MIB_TCPSTATS structure.
// All fields are 32-bit, with the exception of MaxConn which is int32.
type mibTCPStats struct {
	RtoAlgorithm uint32
	RtoMin       uint32
	RtoMax       uint32
	MaxConn      int32
	ActiveOpens  uint32
	PassiveOpens uint32
	AttemptFails uint32
	EstabResets  uint32
	CurrEstab    uint32
	InSegs       uint32
	OutSegs      uint32
	RetransSegs  uint32
	InErrs       uint32
	OutRsts      uint32
	NumConns     uint32
}

var (
	iphlpapi       = windows.NewLazySystemDLL("iphlpapi.dll")
	procGetTCPStat = iphlpapi.NewProc("GetTcpStatisticsEx")
)

// AF_INET and AF_INET6 constants for GetTcpStatisticsEx.
const (
	afInet  = 2
	afInet6 = 23
)

// Windows error codes that indicate the protocol family is not supported.
const (
	errorNotSupported     = syscall.Errno(50) // ERROR_NOT_SUPPORTED
	errorInvalidParameter = syscall.Errno(87) // ERROR_INVALID_PARAMETER
)

// readTCPStatsFunc is the function variable that should be called to get TCP stats.
var readTCPStatsFunc = readTCPStatsWindows

// tcpConnsFunc is the function variable that should be called to get TCP connections.
var tcpConnsFunc = windowsTCPConns

// readTCPStatsWindows retrieves TCP statistics for IPv4 and IPv6.
// It calls GetTcpStatisticsEx for both families and sums the results.
// If IPv6 fails with ERROR_NOT_SUPPORTED or ERROR_INVALID_PARAMETER,
// only IPv4 statistics are used. IPv4 failure always results in an error.
func readTCPStatsWindows() (TCPStats, error) {
	var stats TCPStats

	// Retrieve IPv4 statistics (mandatory)
	var v4 mibTCPStats
	// GetTcpStatisticsEx returns its error code; it does not set the last error.
	r1, _, _ := procGetTCPStat.Call(
		uintptr(unsafe.Pointer(&v4)),
		uintptr(afInet),
	)
	if r1 != 0 {
		return stats, fmt.Errorf("GetTcpStatisticsEx(AF_INET): %w", syscall.Errno(r1))
	}
	stats.ActiveOpens = uint64(v4.ActiveOpens)
	stats.PassiveOpens = uint64(v4.PassiveOpens)
	stats.AttemptFails = uint64(v4.AttemptFails)
	stats.EstabResets = uint64(v4.EstabResets)
	stats.RetransSegs = uint64(v4.RetransSegs)
	stats.OutRsts = uint64(v4.OutRsts)

	// Retrieve IPv6 statistics (optional, may be unsupported)
	var v6 mibTCPStats
	r6, _, _ := procGetTCPStat.Call(
		uintptr(unsafe.Pointer(&v6)),
		uintptr(afInet6),
	)
	if r6 != 0 {
		err6 := syscall.Errno(r6)
		// Ignore specific "not supported" errors for IPv6
		if err6 != errorNotSupported && err6 != errorInvalidParameter {
			return stats, fmt.Errorf("GetTcpStatisticsEx(AF_INET6): %w", err6)
		}
		// Otherwise, keep only IPv4 data
	} else {
		stats.ActiveOpens += uint64(v6.ActiveOpens)
		stats.PassiveOpens += uint64(v6.PassiveOpens)
		stats.AttemptFails += uint64(v6.AttemptFails)
		stats.EstabResets += uint64(v6.EstabResets)
		stats.RetransSegs += uint64(v6.RetransSegs)
		stats.OutRsts += uint64(v6.OutRsts)
	}

	// Windows does not provide ListenOverflows.
	stats.HasListenOverflows = false

	return stats, nil
}

// windowsTCPConns returns a slice of TCP connections currently in the system,
// with only the State field populated.
func windowsTCPConns() ([]netconn.Conn, error) {
	conns, err := net.Connections("tcp")
	if err != nil {
		return nil, err
	}

	out := make([]netconn.Conn, 0, len(conns))
	for _, c := range conns {
		state, ok := windowsTCPState(c.Status)
		if !ok {
			// Skip unknown or DELETE states
			continue
		}
		out = append(out, netconn.Conn{
			State: state,
		})
	}
	return out, nil
}

// windowsTCPState maps a Windows TCP connection status string to the
// numeric state used in the netconn package (Linux numbering).
// It returns false if the status is not recognized or is "DELETE".
func windowsTCPState(status string) (uint8, bool) {
	switch status {
	case "ESTABLISHED":
		return 1, true
	case "SYN_SENT":
		return 2, true
	case "SYN_RECEIVED":
		return 3, true
	case "FIN_WAIT_1":
		return 4, true
	case "FIN_WAIT_2":
		return 5, true
	case "TIME_WAIT":
		return 6, true
	case "CLOSED":
		return 7, true
	case "CLOSE_WAIT":
		return 8, true
	case "LAST_ACK":
		return 9, true
	case "LISTEN":
		return 10, true
	case "CLOSING":
		return 11, true
	case "DELETE", "":
		return 0, false
	default:
		return 0, false
	}
}
