package netconn

import (
	"errors"
	"net/netip"
	"time"
)

// Conn describes a TCP connection as reported by the kernel.
type Conn struct {
	Family        uint8 // unix.AF_INET or unix.AF_INET6
	State         uint8 // TCP state (kernel numbering)
	LocalAddr     netip.AddrPort
	RemoteAddr    netip.AddrPort
	UID           uint32
	Inode         uint32
	HasInfo       bool          // tcp_info was present
	RTT           time.Duration // tcpi_rtt (microseconds)
	Retransmits   uint32        // tcpi_total_retrans
	BytesSent     uint64        // tcpi_bytes_sent
	BytesReceived uint64        // tcpi_bytes_received
}

const (
	StateEstablished uint8 = 1
	StateListen      uint8 = 10
)

var ErrUnsupported = errors.New("netconn: socket diagnostics are not available on this platform")
