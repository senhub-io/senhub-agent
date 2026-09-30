package hostnet

import (
	"encoding/binary"
	"net"

	"senhub-agent.go/internal/agent/services/entity"
)

// Layout of MIB_IPFORWARDTABLE as GetIpForwardTable returns it: a DWORD
// entry count, then MIB_IPFORWARDROW records of 14 DWORDs each. The
// address fields hold the address bytes in network order.
const (
	forwardRowSize  = 14 * 4
	offDest         = 0
	offMask         = 4
	offNextHop      = 12
	offIfIndex      = 16
	offType         = 20
	offMetric1      = 36
	routeIndirect   = 4 // MIB_IPROUTE_TYPE_INDIRECT: the route has a next hop
	forwardHeadSize = 4
)

// parseForwardTable decodes a MIB_IPFORWARDTABLE buffer into next-hop
// routes. Only indirect routes are kept: a direct route's "next hop" is
// the host's own interface address, and linking a route to it would tie
// the host to itself. ifName resolves an interface index to its name.
func parseForwardTable(buf []byte, ifName func(int) string) []hostRoute {
	if len(buf) < forwardHeadSize {
		return nil
	}
	n := int(binary.LittleEndian.Uint32(buf[0:4]))
	var out []hostRoute
	seen := map[string]bool{}
	for i := 0; i < n; i++ {
		off := forwardHeadSize + i*forwardRowSize
		if off+forwardRowSize > len(buf) {
			break
		}
		row := buf[off : off+forwardRowSize]
		if binary.LittleEndian.Uint32(row[offType:]) != routeIndirect {
			continue
		}
		gw := net.IP(append([]byte(nil), row[offNextHop:offNextHop+4]...))
		if gw.IsUnspecified() {
			continue
		}
		dst := net.IP(append([]byte(nil), row[offDest:offDest+4]...)).String()
		ones, bits := net.IPMask(append([]byte(nil), row[offMask:offMask+4]...)).Size()
		if bits == 0 {
			continue
		}
		cidr, ok := entity.CanonicalCIDR(dst, ones)
		if !ok || seen[cidr] {
			continue
		}
		seen[cidr] = true
		r := hostRoute{
			Destination: cidr,
			NextHop:     gw.String(),
			Metric:      int64(binary.LittleEndian.Uint32(row[offMetric1:])),
		}
		if ifName != nil {
			r.Iface = ifName(int(binary.LittleEndian.Uint32(row[offIfIndex:])))
		}
		out = append(out, r)
	}
	return out
}
