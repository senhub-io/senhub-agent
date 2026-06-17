package snmppoll

import (
	"fmt"
	"net"
	"senhub-agent.go/internal/agent/services/snmpcore"
	"strconv"
	"strings"
)

// IPv4 routing table — ipCidrRouteTable (RFC 2096). Most widely implemented
// for IPv4; the modern inetCidrRouteTable (RFC 4292, IPv6-capable) is a later
// concern. Entry index = dest(4).mask(4).tos(1).nextHop(4).
const (
	ipCidrRouteEntry = "1.3.6.1.2.1.4.24.4.1"
	colRouteNextHop  = "4"
	colRouteIfIndex  = "5"
	colRouteType     = "6"
	colRouteMetric1  = "11"

	routeTypeRemote = 4 // ipCidrRouteType: 4 = remote (indirect — a real next-hop)
)

// routeRow is one decoded ipCidrRouteTable entry (only the fields we use).
type routeRow struct {
	Destination string // canonical CIDR from the entry index, "" when unparseable
	NextHop     string // canonicalized IP, or "" when unusable
	Type        int
	IfIndex     string
	Metric      int
}

func collectRoutes(client snmpClient) ([]routeRow, error) {
	binds, err := client.WalkRaw(ipCidrRouteEntry)
	if err != nil {
		return nil, fmt.Errorf("ipCidrRoute walk: %w", err)
	}
	return parseRoutes(binds), nil
}

func parseRoutes(binds []snmpRawBind) []routeRow {
	rows := map[string]*routeRow{}
	var order []string
	prefix := ipCidrRouteEntry + "."

	for _, b := range binds {
		rest, ok := strings.CutPrefix(b.OID, prefix)
		if !ok {
			continue
		}
		col, rowKey, ok := strings.Cut(rest, ".")
		if !ok {
			continue
		}
		r := rows[rowKey]
		if r == nil {
			r = &routeRow{Destination: routeDestFromIndex(rowKey)}
			rows[rowKey] = r
			order = append(order, rowKey)
		}
		switch col {
		case colRouteNextHop:
			r.NextHop = canonIP(asIPString(b.Value))
		case colRouteType:
			if v, ok := snmpcore.AsInt(b.Value); ok {
				r.Type = v
			}
		case colRouteIfIndex:
			if v, ok := snmpcore.AsInt(b.Value); ok {
				r.IfIndex = strconv.Itoa(v)
			}
		case colRouteMetric1:
			if v, ok := snmpcore.AsInt(b.Value); ok {
				r.Metric = v
			}
		}
	}

	out := make([]routeRow, 0, len(order))
	for _, k := range order {
		out = append(out, *rows[k])
	}
	return out
}

// routeDestFromIndex extracts the destination CIDR from an ipCidrRouteTable
// entry index: dest(4).mask(4).tos(1).nextHop(4), octets in decimal. Returns
// "" when the index is short or the mask is non-canonical.
func routeDestFromIndex(rowKey string) string {
	p := strings.Split(rowKey, ".")
	if len(p) < 13 {
		return ""
	}
	dest := net.ParseIP(strings.Join(p[0:4], ".")).To4()
	mask := net.ParseIP(strings.Join(p[4:8], ".")).To4()
	if dest == nil || mask == nil {
		return ""
	}
	ones, bits := net.IPMask(mask).Size()
	if bits == 0 { // non-canonical mask
		return ""
	}
	return fmt.Sprintf("%s/%d", dest.String(), ones)
}

// usableNextHop keeps only next-hops that name a distinct remote device: a
// parseable, non-unspecified, non-loopback IP that is not the polled device's
// own management address.
func usableNextHop(nextHop, selfMgmt string) bool {
	ip := net.ParseIP(nextHop)
	if ip == nil || ip.IsUnspecified() || ip.IsLoopback() {
		return false
	}
	return nextHop != canonIP(selfMgmt)
}

// asIPString renders an SNMP value as an IP string. gosnmp decodes IpAddress
// to a string already; the []byte forms are handled defensively.
func asIPString(v any) string {
	switch x := v.(type) {
	case string:
		return strings.TrimSpace(x)
	case []byte:
		switch len(x) {
		case 4:
			return fmt.Sprintf("%d.%d.%d.%d", x[0], x[1], x[2], x[3])
		case 16:
			return net.IP(x).String()
		default:
			return strings.TrimSpace(string(x))
		}
	default:
		return ""
	}
}
