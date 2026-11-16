//go:build linux

package hostdep

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"senhub-agent.go/internal/agent/services/netconn"
)

// buildUIDView builds the socket ownership and uid-name view for this scrape.
// It returns nil when the socket table cannot be read; an error reading /proc
// still yields the socket uid table without per-uid names.
func buildUIDView() *uidView {
	conns, err := netconn.DumpTCP()
	if err != nil {
		return nil
	}
	uidByTuple := make(map[tupleKey]uint32, len(conns))
	for _, c := range conns {
		uidByTuple[tupleKey{
			laddr: normalizeIP(c.LocalAddr.Addr().String()),
			lport: uint32(c.LocalAddr.Port()),
			raddr: normalizeIP(c.RemoteAddr.Addr().String()),
			rport: uint32(c.RemoteAddr.Port()),
		}] = c.UID
	}

	entries, err := os.ReadDir("/proc")
	if err != nil {
		return &uidView{uidByTuple: uidByTuple}
	}
	var procs []procOwner
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		pid, err := strconv.Atoi(e.Name())
		if err != nil {
			continue
		}
		status, err := os.ReadFile(filepath.Join("/proc", e.Name(), "status"))
		if err != nil {
			continue
		}
		uid, ok := parseUID(status)
		if !ok {
			continue
		}
		name := processName(int32(pid))
		procs = append(procs, procOwner{UID: uid, Name: name})
	}
	return &uidView{uidByTuple: uidByTuple, nameByUID: univoqueNames(procs)}
}

func parseUID(status []byte) (uint32, bool) {
	for _, line := range strings.Split(string(status), "\n") {
		if strings.HasPrefix(line, "Uid:") {
			fields := strings.Fields(line)
			if len(fields) < 2 {
				return 0, false
			}
			uid, err := strconv.ParseUint(fields[1], 10, 32)
			if err != nil {
				return 0, false
			}
			return uint32(uid), true
		}
	}
	return 0, false
}
