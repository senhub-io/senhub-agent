//go:build linux

package network

import (
	"os"
)

// readTCPStats reads host TCP statistics from /proc/net/snmp and optionally /proc/net/netstat.
// The snmp file is required; netstat file is optional.
func readTCPStats() (TCPStats, error) {
	snmpFile, err := os.Open("/proc/net/snmp")
	if err != nil {
		return TCPStats{}, err
	}
	defer snmpFile.Close()

	stats, err := parseProcNetSNMP(snmpFile)
	if err != nil {
		return stats, err
	}

	netstatFile, err := os.Open("/proc/net/netstat")
	if err != nil {
		return stats, nil
	}
	defer netstatFile.Close()

	err = parseProcNetNetstat(netstatFile, &stats)
	if err != nil {
		return stats, err
	}
	return stats, nil
}
