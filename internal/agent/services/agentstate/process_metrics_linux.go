//go:build linux

package agentstate

import (
	"bufio"
	"os"
	"strconv"
	"strings"
)

// getResidentMemory reads VmRSS from /proc/self/status. Returns the
// value in bytes, or 0 if the file cannot be read (defensive — the
// caller treats 0 as "unknown").
func getResidentMemory() uint64 {
	f, err := os.Open("/proc/self/status")
	if err != nil {
		return 0
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "VmRSS:") {
			continue
		}
		// Format: "VmRSS:    12345 kB"
		fields := strings.Fields(line)
		if len(fields) < 2 {
			return 0
		}
		kb, err := strconv.ParseUint(fields[1], 10, 64)
		if err != nil {
			return 0
		}
		return kb * 1024
	}
	return 0
}

// getOpenFDs counts entries in /proc/self/fd. Each entry is a symlink
// to an open file/socket/pipe; the count matches what `lsof -p $$`
// reports. Returns 0 on error (caller treats 0 as unknown).
func getOpenFDs() int {
	entries, err := os.ReadDir("/proc/self/fd")
	if err != nil {
		return 0
	}
	return len(entries)
}
