package snmppoll

import (
	"fmt"
	"net"
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

func isRouteAlwaysKept(r routeRow) bool {
	if r.Type == routeTypeLocal || r.Type == routeTypeReject || r.Type == routeTypeBlackhole {
		return true
	}
	return r.Destination == "0.0.0.0/0" || r.Destination == "::/0"
}

// selectRoutes applies the per-device route bound. Connected, default,
// reject and blackhole routes are always kept; the rest are sorted by
// (destination, next hop) and kept until maxRoutes. It returns the kept
// routes and how many of the remaining routes were left out.
// The bound is not a performance knob: a device carrying a full Internet
// table must not be inventoried route by route, and the counter on its
// network.device says so instead of pretending the table is complete.
func selectRoutes(routes []routeRow, selfMgmt string, maxRoutes int) ([]routeRow, int) {
	if maxRoutes <= 0 {
		maxRoutes = defaultMaxRoutes
	}

	seen := map[string]bool{}
	protected := make([]routeRow, 0, len(routes))
	rest := make([]routeRow, 0, len(routes))

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
	return kept, omitted
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
			idKeyNetworkDevice:         selfID,
			entity.KeyRouteDestination: nr.Destination,
			entity.KeyRouteNextHop:     nr.NextHop,
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

func applyRouteCounters(obs *entity.Observation, deviceID string, omitted, limit int) {
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
		gw, _ := e.ID[entity.KeyRouteNextHop].(string)
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
