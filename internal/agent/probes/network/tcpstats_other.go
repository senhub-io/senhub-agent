//go:build !linux && !windows

package network

import "senhub-agent.go/internal/agent/services/netconn"

// readTCPStatsFunc and tcpConnsFunc are nil where host TCP counters are not read.
var (
	readTCPStatsFunc func() (TCPStats, error)
	tcpConnsFunc     func() ([]netconn.Conn, error)
)
