package snmppoll

import (
	"fmt"
	"net"
	"net/netip"
	"sort"
	"strings"

	"senhub-agent.go/internal/agent/services/entity"
)

const (
	attrRouteProtocol     = "route.protocol"
	attrRouteProtocolCode = "network.route.protocol.code"
	attrRouteType         = "route.type"
	attrRoutesOmitted     = "senhub.routes.omitted"
	attrRoutesLimit       = "senhub.routes.limit"
	attrRoutesFiltered    = "senhub.routes.filtered"
)

func routeTypeName(v int) string {
	switch v {
	case routeTypeOther:
		return "other"
	case routeTypeReject:
		return "reject"
	case routeTypeLocal:
		return "local"
	case routeTypeRemote:
		return "remote"
	case routeTypeBlackhole:
		return "blackhole"
	default:
		return ""
	}
}

// unspecifiedNextHop returns the IP-forwarding "no next hop" address for the
// address family of a destination CIDR: 0.0.0.0 for IPv4, :: for IPv6.
func unspecifiedNextHop(destCIDR string) string {
	ip, _, err := net.ParseCIDR(destCIDR)
	if err != nil {
		return "0.0.0.0"
	}
	if ip.To4() != nil {
		return "0.0.0.0"
	}
	return "::"
}

func eligibleRoute(r routeRow, selfMgmt string) (routeRow, bool) {
	if r.Destination == "" {
		return routeRow{}, false
	}

	switch r.Type {
	case routeTypeLocal, routeTypeReject, routeTypeBlackhole:
		r.NextHop = unspecifiedNextHop(r.Destination)
		return r, true
	case routeTypeRemote, routeTypeOther:
		if !usableNextHop(r.NextHop, selfMgmt) {
			return routeRow{}, false
		}
		return r, true
	default:
		return routeRow{}, false
	}
}

// systemRoutePrefixes are destination ranges that are not topology:
// multicast, loopback and link-local.
var systemRoutePrefixes = []netip.Prefix{
	netip.MustParsePrefix("224.0.0.0/4"),
	netip.MustParsePrefix("ff00::/8"),
	netip.MustParsePrefix("127.0.0.0/8"),
	netip.MustParsePrefix("::1/128"),
	netip.MustParsePrefix("169.254.0.0/16"),
	netip.MustParsePrefix("fe80::/10"),
}

// routeFilterContext is what the system-route filter needs from the rest of
// the sweep. A local host route is excluded only when its address is
// published as a network.address by the same observation: many devices list
// host routes for addresses the observation does not carry (an interface
// left out, an IPv6 address), and those routes are kept.
type routeFilterContext struct {
	ownAddrs   map[string]bool
	broadcasts map[string]bool
}

// newRouteFilterContext collects the addresses the observation publishes and
// the broadcast address of every connected IPv4 prefix the device reports,
// from its route table and from its address masks.
func newRouteFilterContext(routes []routeRow, addrs []ipAddr, ifaces []ifaceRow) routeFilterContext {
	fc := routeFilterContext{ownAddrs: publishedAddresses(addrs, ifaces), broadcasts: map[string]bool{}}
	for _, r := range routes {
		if r.Type != routeTypeLocal {
			continue
		}
		if p, err := netip.ParsePrefix(r.Destination); err == nil {
			addIPv4Broadcast(fc.broadcasts, p)
		}
	}
	for _, a := range addrs {
		if a.Prefix < 0 {
			continue
		}
		if ip, err := netip.ParseAddr(a.IP); err == nil {
			if p, err := ip.Prefix(a.Prefix); err == nil {
				addIPv4Broadcast(fc.broadcasts, p)
			}
		}
	}
	return fc
}

func addIPv4Broadcast(set map[string]bool, p netip.Prefix) {
	if !p.Addr().Is4() || p.Bits() < 1 || p.Bits() > 30 {
		return
	}
	b := p.Masked().Addr().As4()
	host := uint32(1)<<(32-p.Bits()) - 1
	v := uint32(b[0])<<24 | uint32(b[1])<<16 | uint32(b[2])<<8 | uint32(b[3])
	v |= host
	set[netip.AddrFrom4([4]byte{byte(v >> 24), byte(v >> 16), byte(v >> 8), byte(v)}).String()] = true
}

// systemRoute reports whether a route normalized by eligibleRoute is left out
// of the topology by default: reject and blackhole routes, multicast,
// loopback and link-local destinations, 0.0.0.0/32 and 255.255.255.255/32,
// a local host route to an address this observation publishes, and a local
// route to the broadcast address of a connected prefix (nothing is
// forwarded there). Default routes are kept.
func systemRoute(r routeRow, fc routeFilterContext) bool {
	if r.Type == routeTypeReject || r.Type == routeTypeBlackhole {
		return true
	}
	p, err := netip.ParsePrefix(r.Destination)
	if err != nil {
		return false
	}
	p = p.Masked()
	if p.Addr().Is4() && p.Bits() == 32 {
		if s := p.Addr().String(); s == "0.0.0.0" || s == "255.255.255.255" {
			return true
		}
	}
	for _, sys := range systemRoutePrefixes {
		if sys.Contains(p.Addr()) && p.Bits() >= sys.Bits() {
			return true
		}
	}
	if r.Type == routeTypeLocal && p.Bits() == p.Addr().BitLen() {
		addr := p.Addr().String()
		return fc.ownAddrs[addr] || fc.broadcasts[addr]
	}
	return false
}

// isRouteAlwaysKept protects connected and default routes from the bound.
// Reject, blackhole and host routes reach it only with include_system_routes,
// which restores the behaviour that kept them all.
func isRouteAlwaysKept(r routeRow) bool {
	if r.Type == routeTypeLocal || r.Type == routeTypeReject || r.Type == routeTypeBlackhole {
		return true
	}
	return r.Destination == "0.0.0.0/0" || r.Destination == "::/0"
}

// routeSelection is the outcome of selectRoutes.
type routeSelection struct {
	kept    []routeRow
	omitted int
	// filtered holds the routes the system-route filter left out, normalized
	// and deduplicated as kept ones are.
	filtered []routeRow
}

// selectRoutes applies the system-route filter, then the per-device route
// bound: the filter runs first so the bound's slots go to routes a topology
// uses. Connected and default routes are always kept; the rest are sorted by
// (destination, next hop) and kept until maxRoutes. omitted counts the
// routes the bound left out, filtered the ones the filter did.
// The bound is not a performance knob: a device carrying a full Internet
// table must not be inventoried route by route, and the counter on its
// network.device says so instead of pretending the table is complete.
func selectRoutes(routes []routeRow, selfMgmt string, maxRoutes int, includeSystem bool, fc routeFilterContext) routeSelection {
	if maxRoutes <= 0 {
		maxRoutes = defaultMaxRoutes
	}

	seen := map[string]bool{}
	protected := make([]routeRow, 0, len(routes))
	rest := make([]routeRow, 0, len(routes))
	var filtered []routeRow

	for _, r := range routes {
		nr, ok := eligibleRoute(r, selfMgmt)
		if !ok {
			continue
		}
		key := nr.Destination + " " + nr.NextHop
		if seen[key] {
			continue
		}
		seen[key] = true
		if !includeSystem && systemRoute(nr, fc) {
			filtered = append(filtered, nr)
			continue
		}
		if isRouteAlwaysKept(nr) {
			protected = append(protected, nr)
		} else {
			rest = append(rest, nr)
		}
	}

	sort.Slice(rest, func(i, j int) bool {
		if rest[i].Destination != rest[j].Destination {
			return rest[i].Destination < rest[j].Destination
		}
		return rest[i].NextHop < rest[j].NextHop
	})

	kept := make([]routeRow, 0, maxRoutes)
	kept = append(kept, protected...)
	omitted := 0
	if len(kept) < maxRoutes {
		slots := maxRoutes - len(kept)
		if len(rest) > slots {
			kept = append(kept, rest[:slots]...)
			omitted = len(rest) - slots
		} else {
			kept = append(kept, rest...)
		}
	} else {
		omitted = len(rest)
	}
	return routeSelection{kept: kept, omitted: omitted, filtered: filtered}
}

// appendRouteEntities emits the device's routes as network.route entities it
// owns (has_route, mirror of has_interface). Identity after IP-FORWARD-MIB
// (RFC 4292), which indexes a route on its destination and its next hop:
// {device, destination, next hop}, so ECMP is several routes rather than the
// first one. A route without a next hop (local, reject, blackhole) carries the
// MIB's own "no next hop" address and no next_hop_via; route.type says which
// of the three it is, so a dropped destination never reads as connected.
func appendRouteEntities(obs *entity.Observation, selfID string, self deviceIdentity, routes []routeRow, ifaces []ifaceRow, addrSeen map[string]bool) {
	if selfID == "" {
		return
	}
	if addrSeen == nil {
		addrSeen = map[string]bool{}
	}

	ifNameByIndex := make(map[string]string, len(ifaces))
	for _, ifc := range ifaces {
		if ifc.Name != "" {
			ifNameByIndex[ifc.Index] = ifc.Name
		}
	}

	routeSeen := map[string]bool{}
	for _, r := range routes {
		nr, ok := eligibleRoute(r, self.MgmtIP)
		if !ok {
			continue
		}
		key := nr.Destination + " " + nr.NextHop
		if routeSeen[key] {
			continue
		}
		routeSeen[key] = true

		routeID := map[string]any{
			idKeyNetworkDevice:    selfID,
			idKeyRouteDestination: nr.Destination,
			attrNextHopIP:         nr.NextHop,
		}
		attrs := map[string]any{
			attrRouteType: routeTypeName(nr.Type),
		}
		if r.Metric > 0 {
			attrs[attrRouteMetric] = int64(r.Metric)
		}
		if r.Protocol > 0 {
			attrs[attrRouteProtocolCode] = int64(r.Protocol)
			if name, ok := ianaRouteProtocolNames[r.Protocol]; ok {
				attrs[attrRouteProtocol] = name
			}
		}
		// A reject or blackhole route ends by design: the interface a source
		// attaches to it (Linux often says lo) is how the stack represents
		// the drop, not where packets leave, and drawn as an egress it would
		// put a hop to loopback on the path.
		if nr.Type != routeTypeReject && nr.Type != routeTypeBlackhole {
			if ifName := ifNameByIndex[nr.IfIndex]; ifName != "" {
				attrs[idKeyInterfaceName] = ifName
			}
		}
		obs.Entities = append(obs.Entities, entity.Entity{
			Type: entityTypeNetworkRoute, ID: routeID, Attributes: attrs, Scope: entity.ScopeSNMPRoute,
		})
		obs.Relations = append(obs.Relations, entity.Relation{
			Type:     relHasRoute,
			FromType: entityTypeNetworkDevice, FromID: deviceKey(selfID),
			ToType: entityTypeNetworkRoute, ToID: routeID,
		})

		if nr.Type != routeTypeRemote && nr.Type != routeTypeOther {
			continue
		}

		// The gateway as the same network.address node a host route reaches
		// via next_hop_via, but only for a globally unique address: a switch
		// has no interface-class filter, and a private gateway shared by
		// unrelated switches would merge them. next_hop.ip stays on the
		// route either way.
		gw := nr.NextHop
		if c, ok := entity.CanonicalIP(gw); ok {
			gw = c
		}
		if !entity.AddressIsGloballyUnique(gw) {
			continue
		}
		gwID := map[string]any{idKeyNetworkAddress: gw}
		if !addrSeen[gw] {
			addrSeen[gw] = true
			obs.Entities = append(obs.Entities, entity.Entity{Type: entityTypeNetworkAddress, ID: gwID, Scope: entity.ScopeSNMPRoute})
		}
		obs.Relations = append(obs.Relations, entity.Relation{
			Type:     relNextHopVia,
			FromType: entityTypeNetworkRoute, FromID: routeID,
			ToType: entityTypeNetworkAddress, ToID: gwID,
		})
	}
}

func applyRouteCounters(obs *entity.Observation, deviceID string, omitted, filtered, limit int) {
	if deviceID == "" {
		return
	}
	for i := range obs.Entities {
		if obs.Entities[i].Type == entityTypeNetworkDevice && obs.Entities[i].ID[idKeyNetworkDevice] == deviceID {
			if obs.Entities[i].Attributes == nil {
				obs.Entities[i].Attributes = map[string]any{}
			}
			obs.Entities[i].Attributes[attrRoutesOmitted] = int64(omitted)
			obs.Entities[i].Attributes[attrRoutesLimit] = int64(limit)
			obs.Entities[i].Attributes[attrRoutesFiltered] = int64(filtered)
			return
		}
	}
}

// linkPrivateNextHops adds the next_hop_via a route to a private gateway
// lacks when this same observation holds that address bound to an
// interface of a device the agent polled. Router-to-router links are
// almost always private, and without the edge a path stopped at every
// router; but a private address is not a unique identity, so the edge is
// only asserted when the agent holds both ends itself, as for the host's
// own gateway. Two sites reusing one range behind one agent remain a
// known limit: the consumer refuses to cross an address entered and left
// by bound_to, so the collision reads as a named convergence, not a path.
func linkPrivateNextHops(obs *entity.Observation) {
	bound := map[string]bool{}
	for _, r := range obs.Relations {
		if r.Type == relBoundTo && r.FromType == entityTypeNetworkAddress && r.ToType == entityTypeNetworkInterface {
			if _, onDevice := r.ToID[idKeyNetworkDevice]; !onDevice {
				continue
			}
			if ip, ok := r.FromID[idKeyNetworkAddress].(string); ok {
				bound[ip] = true
			}
		}
	}
	if len(bound) == 0 {
		return
	}
	linked := map[string]bool{}
	for _, r := range obs.Relations {
		if r.Type == relNextHopVia && r.FromType == entityTypeNetworkRoute {
			linked[entityKeyString(r.FromID)] = true
		}
	}
	var add []entity.Relation
	for _, e := range obs.Entities {
		if e.Type != entityTypeNetworkRoute || linked[entityKeyString(e.ID)] {
			continue
		}
		rt, _ := e.Attributes[attrRouteType].(string)
		if rt != "remote" && rt != "other" {
			continue
		}
		gw, _ := e.ID[attrNextHopIP].(string)
		if c, ok := entity.CanonicalIP(gw); ok {
			gw = c
		}
		if !bound[gw] {
			continue
		}
		add = append(add, entity.Relation{
			Type:     relNextHopVia,
			FromType: entityTypeNetworkRoute, FromID: e.ID,
			ToType: entityTypeNetworkAddress, ToID: map[string]any{idKeyNetworkAddress: gw},
		})
	}
	obs.Relations = append(obs.Relations, add...)
}

// entityKeyString renders an identity map in a stable order for set keys.
func entityKeyString(id map[string]any) string {
	keys := make([]string, 0, len(id))
	for k := range id {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var b strings.Builder
	for _, k := range keys {
		fmt.Fprintf(&b, "%s=%v;", k, id[k])
	}
	return b.String()
}
