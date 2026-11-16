package network

import (
	"bufio"
	"fmt"
	"io"
	"strconv"
	"strings"

	"senhub-agent.go/internal/agent/services/netconn"
)

// TCPStats are the host-wide TCP counters, cumulative since boot.
type TCPStats struct {
	ActiveOpens, PassiveOpens, AttemptFails, EstabResets uint64
	RetransSegs, OutRsts                                 uint64
	ListenOverflows                                      uint64
	HasListenOverflows                                   bool // only Linux reports it
}

// tcpStateNames maps the kernel TCP state numbers to the OTel network.connection.state values.
var tcpStateNames = map[uint8]string{
	1:  "established",
	2:  "syn_sent",
	3:  "syn_received",
	4:  "fin_wait_1",
	5:  "fin_wait_2",
	6:  "time_wait",
	7:  "close",
	8:  "close_wait",
	9:  "last_ack",
	10: "listen",
	11: "closing",
}

// countByState counts connections per OTel state name; unknown states are skipped.
// Every state of tcpStateNames is present in the result, at 0 when none is seen,
// so a series does not vanish when its count drops to zero.
func countByState(conns []netconn.Conn) map[string]int {
	counts := make(map[string]int, len(tcpStateNames))
	for _, name := range tcpStateNames {
		counts[name] = 0
	}
	for _, c := range conns {
		if name, ok := tcpStateNames[c.State]; ok {
			counts[name]++
		}
	}
	return counts
}

// parseProcNetSNMP parses the "Tcp:" pair of lines from /proc/net/snmp.
// It fills ActiveOpens, PassiveOpens, AttemptFails, EstabResets, RetransSegs, OutRsts.
// Returns an error if the Tcp: lines are missing or the number of columns differs.
// Negative values (MaxConn = -1) are ignored because only the listed fields are read.
func parseProcNetSNMP(r io.Reader) (TCPStats, error) {
	var s TCPStats
	scanner := bufio.NewScanner(r)
	var headerLine, valueLine string
	foundHeader := false
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "Tcp:") {
			if !foundHeader {
				headerLine = line
				foundHeader = true
				// need to find next line starting with "Tcp:" (value line)
				for scanner.Scan() {
					nextLine := scanner.Text()
					if strings.HasPrefix(nextLine, "Tcp:") {
						valueLine = nextLine
						break
					}
				}
				if valueLine == "" {
					return s, fmt.Errorf("missing Tcp: value line")
				}
				break
			}
		}
	}
	if err := scanner.Err(); err != nil {
		return s, err
	}
	if !foundHeader {
		return s, fmt.Errorf("Tcp: header not found")
	}
	headerFields := strings.Fields(headerLine)
	valueFields := strings.Fields(valueLine)
	if len(headerFields) != len(valueFields) {
		return s, fmt.Errorf("mismatched column count: header %d, values %d", len(headerFields), len(valueFields))
	}
	colIndex := make(map[string]int)
	for i, name := range headerFields {
		colIndex[name] = i
	}
	getField := func(name string) (uint64, error) {
		idx, ok := colIndex[name]
		if !ok {
			return 0, fmt.Errorf("field %s not found", name)
		}
		v, err := strconv.ParseUint(valueFields[idx], 10, 64)
		if err != nil {
			return 0, fmt.Errorf("parsing %s: %w", name, err)
		}
		return v, nil
	}
	var err error
	if s.ActiveOpens, err = getField("ActiveOpens"); err != nil {
		return s, err
	}
	if s.PassiveOpens, err = getField("PassiveOpens"); err != nil {
		return s, err
	}
	if s.AttemptFails, err = getField("AttemptFails"); err != nil {
		return s, err
	}
	if s.EstabResets, err = getField("EstabResets"); err != nil {
		return s, err
	}
	if s.RetransSegs, err = getField("RetransSegs"); err != nil {
		return s, err
	}
	if s.OutRsts, err = getField("OutRsts"); err != nil {
		return s, err
	}
	return s, nil
}

// parseProcNetNetstat parses the "TcpExt:" pair of lines from /proc/net/netstat.
// It fills ListenOverflows and sets HasListenOverflows if found.
// Absence of TcpExt is not an error; HasListenOverflows remains false.
func parseProcNetNetstat(r io.Reader, s *TCPStats) error {
	scanner := bufio.NewScanner(r)
	var headerLine, valueLine string
	foundHeader := false
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "TcpExt:") {
			if !foundHeader {
				headerLine = line
				foundHeader = true
				for scanner.Scan() {
					nextLine := scanner.Text()
					if strings.HasPrefix(nextLine, "TcpExt:") {
						valueLine = nextLine
						break
					}
				}
				if valueLine == "" {
					return fmt.Errorf("missing TcpExt: value line")
				}
				break
			}
		}
	}
	if err := scanner.Err(); err != nil {
		return err
	}
	if !foundHeader {
		return nil
	}
	headerFields := strings.Fields(headerLine)
	valueFields := strings.Fields(valueLine)
	if len(headerFields) != len(valueFields) {
		return fmt.Errorf("mismatched column count: header %d, values %d", len(headerFields), len(valueFields))
	}
	idx := -1
	for i, name := range headerFields {
		if name == "ListenOverflows" {
			idx = i
			break
		}
	}
	if idx == -1 {
		return nil
	}
	v, err := strconv.ParseUint(valueFields[idx], 10, 64)
	if err != nil {
		return fmt.Errorf("parsing ListenOverflows: %w", err)
	}
	s.ListenOverflows = v
	s.HasListenOverflows = true
	return nil
}
