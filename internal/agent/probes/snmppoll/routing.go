package snmppoll

import (
	"fmt"
	"net"
	"strconv"
	"strings"

	"senhub-agent.go/internal/agent/services/entity"
	"senhub-agent.go/internal/agent/services/snmpcore"
)

// IPv4 routing table — ipCidrRouteTable (RFC 2096), the most widely
// implemented one. Entry index = dest(4).mask(4).tos(1).nextHop(4).
const (
	ipCidrRouteEntry = "1.3.6.1.2.1.4.24.4.1"
	colRouteNextHop  = "4"
	colRouteIfIndex  = "5"
	colRouteType     = "6"
	colRouteMetric1  = "11"

	routeTypeRemote = 4 // ipCidrRouteType: 4 = remote (indirect — a real next-hop)
)

// Address-family-agnostic routing table — inetCidrRouteTable (RFC 4292),
// the successor to ipCidrRouteTable and the only one carrying IPv6 routes.
// Its columns sit at different sub-identifiers, and its index encodes
// variable-length addresses, so it needs its own spec rather than a
// different OID prefix on the same one.
//
// Devices that implement both list their IPv4 routes twice. That is
// harmless here: the consumer keeps the first entity per destination CIDR,
// and this table is walked second, so IPv4 identities stay exactly what
// they were before IPv6 collection existed.
const (
	inetCidrRouteEntry  = "1.3.6.1.2.1.4.24.7.1"
	colInetRouteNextHop = "6"
	colInetRouteIfIndex = "7"
	colInetRouteType    = "8"
	colInetRouteMetric1 = "12"
)

// InetAddressType values used in the inetCidrRouteTable index (RFC 4001).
// The zoned variants (ipv4z=3, ipv6z=4) append a zone index to the address
// and describe a destination that is only meaningful inside one scope; they
// would canonicalize onto the same CIDR as their unzoned counterpart and
// collide, so they are skipped rather than flattened.
const (
	inetAddrTypeIPv4 = 1
	inetAddrTypeIPv6 = 2
)

// routeTableSpec locates the fields of one routing table. Both tables are
// parsed through it so a column added to one cannot silently diverge from
// the other.
type routeTableSpec struct {
	entry    string
	nextHop  string
	ifIndex  string
	typeCol  string
	metric1  string
	destFrom func(rowKey string) string
}

var (
	ipCidrRouteSpec = routeTableSpec{
		entry:    ipCidrRouteEntry,
		nextHop:  colRouteNextHop,
		ifIndex:  colRouteIfIndex,
		typeCol:  colRouteType,
		metric1:  colRouteMetric1,
		destFrom: routeDestFromIndex,
	}
	inetCidrRouteSpec = routeTableSpec{
		entry:    inetCidrRouteEntry,
		nextHop:  colInetRouteNextHop,
		ifIndex:  colInetRouteIfIndex,
		typeCol:  colInetRouteType,
		metric1:  colInetRouteMetric1,
		destFrom: inetRouteDestFromIndex,
	}
)

// routeRow is one decoded ipCidrRouteTable entry (only the fields we use).
type routeRow struct {
	Destination string // canonical CIDR from the entry index, "" when unparseable
	NextHop     string // canonicalized IP, or "" when unusable
	Type        int
	IfIndex     string
	Metric      int
}

// collectRoutes walks both routing tables. A device implementing only one
// of them is the norm, not a failure: ipCidrRouteTable is absent from
// IPv6-only stacks and inetCidrRouteTable from older IPv4-only agents. Only
// when BOTH walks fail is there nothing to report, and the error then names
// both causes rather than hiding one behind the other.
func collectRoutes(client snmpClient) ([]routeRow, error) {
	v4Binds, v4Err := client.WalkRaw(ipCidrRouteEntry)
	inetBinds, inetErr := client.WalkRaw(inetCidrRouteEntry)

	if v4Err != nil && inetErr != nil {
		return nil, fmt.Errorf("route walks failed — ipCidrRoute: %w; inetCidrRoute: %v", v4Err, inetErr)
	}

	var rows []routeRow
	if v4Err == nil {
		rows = append(rows, parseRouteTable(ipCidrRouteSpec, v4Binds)...)
	}
	if inetErr == nil {
		rows = append(rows, parseRouteTable(inetCidrRouteSpec, inetBinds)...)
	}
	return rows, nil
}

func parseRoutes(binds []snmpRawBind) []routeRow {
	return parseRouteTable(ipCidrRouteSpec, binds)
}

func parseRouteTable(spec routeTableSpec, binds []snmpRawBind) []routeRow {
	rows := map[string]*routeRow{}
	var order []string
	prefix := spec.entry + "."

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
			r = &routeRow{Destination: spec.destFrom(rowKey)}
			rows[rowKey] = r
			order = append(order, rowKey)
		}
		switch col {
		case spec.nextHop:
			r.NextHop = canonIP(asIPString(b.Value))
		case spec.typeCol:
			if v, ok := snmpcore.AsInt(b.Value); ok {
				r.Type = v
			}
		case spec.ifIndex:
			if v, ok := snmpcore.AsInt(b.Value); ok {
				r.IfIndex = strconv.Itoa(v)
			}
		case spec.metric1:
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
// entry index: dest(4).mask(4).tos(1).nextHop(4), octets in decimal. The
// result is the canonical route.destination identity (entity.CanonicalCIDR —
// explicit prefix, host bits zeroed even when the device reports them set).
// Returns "" when the index is short or the mask is non-canonical.
func routeDestFromIndex(rowKey string) string {
	p := strings.Split(rowKey, ".")
	if len(p) < 13 {
		return ""
	}
	mask := net.ParseIP(strings.Join(p[4:8], ".")).To4()
	if mask == nil {
		return ""
	}
	ones, bits := net.IPMask(mask).Size()
	if bits == 0 { // non-canonical mask
		return ""
	}
	dest, ok := entity.CanonicalCIDR(strings.Join(p[0:4], "."), ones)
	if !ok {
		return ""
	}
	return dest
}

// inetRouteDestFromIndex extracts the destination CIDR from an
// inetCidrRouteTable entry index (RFC 4292):
//
//	destType.destLen.dest[destLen].pfxLen.policyLen.policy[…].nextHopType.nextHopLen.nextHop[…]
//
// Unlike the fixed IPv4 index, addresses here are InetAddress values carried
// with an explicit length prefix, so the destination cannot be read at a
// fixed offset — the length must be consumed to find where the prefix length
// sits. Everything past pfxLen (policy OID, next hop) is index padding we do
// not need: the next hop arrives as a column value.
//
// Returns "" for anything it cannot read exactly: a truncated index, a
// length that disagrees with the address family, or a zoned address family.
func inetRouteDestFromIndex(rowKey string) string {
	p := strings.Split(rowKey, ".")
	if len(p) < 3 {
		return ""
	}
	addrType, err := strconv.Atoi(p[0])
	if err != nil {
		return ""
	}
	addrLen, err := strconv.Atoi(p[1])
	if err != nil || addrLen <= 0 {
		return ""
	}
	// dest occupies addrLen sub-identifiers, pfxLen is the one right after.
	if len(p) < 2+addrLen+1 {
		return ""
	}
	pfxLen, err := strconv.Atoi(p[2+addrLen])
	if err != nil {
		return ""
	}

	ip := inetAddrFromIndex(addrType, p[2:2+addrLen])
	if ip == "" {
		return ""
	}
	dest, ok := entity.CanonicalCIDR(ip, pfxLen)
	if !ok {
		return ""
	}
	return dest
}

// inetAddrFromIndex renders the address octets of an inetCidrRouteTable
// index. The declared length must match the family: a 16-octet "IPv4"
// address is a malformed row, not an address to guess at.
func inetAddrFromIndex(addrType int, octets []string) string {
	switch {
	case addrType == inetAddrTypeIPv4 && len(octets) == 4:
		return strings.Join(octets, ".")
	case addrType == inetAddrTypeIPv6 && len(octets) == 16:
		raw := make(net.IP, 16)
		for i, o := range octets {
			v, err := strconv.Atoi(o)
			if err != nil || v < 0 || v > 255 {
				return ""
			}
			raw[i] = byte(v)
		}
		return raw.String()
	default:
		return ""
	}
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
