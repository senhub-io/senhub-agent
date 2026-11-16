//go:build !linux

package network

// readTCPStatsFunc is nil on non-Linux platforms.
var readTCPStatsFunc func() (TCPStats, error)
