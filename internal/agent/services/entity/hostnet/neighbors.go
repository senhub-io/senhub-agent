package hostnet

import (
	"io"
	"net"
	"net/netip"
	"sort"
	"strconv"
	"strings"

	"senhub-agent.go/internal/agent/services/entity"
)

// hostNeighbor is one host neighbour entry (ARP for IPv4, NDP for IPv6).
type hostNeighbor struct {
	IP    string // canonical textual IP
	MAC   string // canonical OTel form "AA-BB-CC-DD-EE-FF" once returned by platformNeighbors
	Iface string // interface name when known
	State string // "reachable", "stale", "permanent", "incomplete", "failed", "delay", "probe" or "" when unknown
}

func keepNeighbor(n hostNeighbor) bool {
	if n.State == "incomplete" || n.State == "failed" {
		return false
	}
	ip, err := netip.ParseAddr(n.IP)
	if err != nil || ip.IsMulticast() || ip.IsUnspecified() || ip.IsLoopback() {
		return false
	}
	hw, err := net.ParseMAC(n.MAC)
	if err != nil || len(hw) == 0 {
		return false
	}
	zero := true
	broadcast := true
	for _, b := range hw {
		if b != 0 {
			zero = false
		}
		if b != 0xff {
			broadcast = false
		}
	}
	if zero || broadcast {
		return false
	}
	return hw[0]&1 == 0
}

func dedupeAndSortNeighbors(ns []hostNeighbor) []hostNeighbor {
	out := make([]hostNeighbor, 0, len(ns))
	for _, n := range ns {
		if !keepNeighbor(n) {
			continue
		}
		// Every observer renders a MAC byte-identically, so a neighbour seen
		// here and on a switch port carries the same value.
		if mac, ok := entity.CanonicalMACString(n.MAC); ok {
			n.MAC = mac
		}
		out = append(out, n)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Iface != out[j].Iface {
			return out[i].Iface < out[j].Iface
		}
		if out[i].IP != out[j].IP {
			return out[i].IP < out[j].IP
		}
		if out[i].MAC != out[j].MAC {
			return out[i].MAC < out[j].MAC
		}
		return out[i].State < out[j].State
	})
	seen := make(map[[2]string]bool, len(out))
	res := make([]hostNeighbor, 0, len(out))
	for _, n := range out {
		key := [2]string{n.Iface, n.IP}
		if seen[key] {
			continue
		}
		seen[key] = true
		res = append(res, n)
	}
	return res
}

// parseProcARP decodes /proc/net/arp and keeps only neighbours the entity
// model will accept. It is pure so it can be tested without a Linux host.
func parseProcARP(r io.Reader) []hostNeighbor {
	data, err := io.ReadAll(r)
	if err != nil {
		return nil
	}
	var out []hostNeighbor
	for i, line := range strings.Split(string(data), "\n") {
		if i == 0 { // header
			continue
		}
		f := strings.Fields(line)
		if len(f) < 6 {
			continue
		}
		ip := net.ParseIP(f[0])
		if ip == nil {
			continue
		}
		hw, err := net.ParseMAC(f[3])
		if err != nil {
			continue
		}
		flags, err := strconv.ParseUint(f[2], 0, 32)
		if err != nil {
			continue
		}
		var state string
		switch {
		case flags&0x4 != 0: // ATF_PERM
			state = "permanent"
		case flags&0x2 != 0: // ATF_COM
			state = "reachable"
		case flags == 0:
			state = "incomplete"
		}
		n := hostNeighbor{
			IP:    ip.String(),
			MAC:   hw.String(),
			Iface: f[5],
			State: state,
		}
		if keepNeighbor(n) {
			out = append(out, n)
		}
	}
	return out
}
