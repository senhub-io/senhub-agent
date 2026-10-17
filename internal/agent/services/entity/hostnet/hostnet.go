package hostnet

import (
	"net/netip"
	"sort"

	"senhub-agent.go/internal/agent/services/entity"
)

const (
	entityTypeHost           = "host"
	entityTypeNetworkRoute   = "network.route"
	entityTypeNetworkAddress = "network.address"
	idKeyHost                = "host.id"
	idKeyRouteDestination    = "route.destination"
	idKeyNetworkAddress      = "network.address"
	idKeyNextHopIP           = "next_hop.ip"
	attrMetric               = "metric"
	attrEgressInterface      = "network.interface.name"
	attrRouteType            = "route.type"
	attrRouteProtocol        = "route.protocol"
	attrRouteProtocolCode    = "network.route.protocol.code"
	attrRoutesOmitted        = "senhub.routes.omitted"
	attrRoutesLimit          = "senhub.routes.limit"
	relHasRoute              = "has_route"
	relNextHopVia            = "next_hop_via"

	maxHostRoutes = 500

	routeTypeOther     = "other"
	routeTypeReject    = "reject"
	routeTypeLocal     = "local"
	routeTypeRemote    = "remote"
	routeTypeBlackhole = "blackhole"
	// routeTypeInvalid marks a row the source listed but that is not a
	// route; it is never emitted, only counted as omitted.
	routeTypeInvalid = "invalid"
)

// hostRoute is one route parsed from the kernel routing table.
type hostRoute struct {
	Destination  string // canonical CIDR, e.g. "0.0.0.0/0"
	NextHop      string // gateway IP; zero for local/reject/blackhole
	Metric       int64
	Iface        string
	RouteType    string // route.type value
	ProtocolName string // route.protocol value; empty when absent
	ProtocolCode *int   // raw source protocol code; nil when unknown
}

// Source implements entity.Source for the host routing table.
type Source struct {
	hostID     func() string
	readRoutes func() ([]hostRoute, error)
	forwarding func() (v4, v6 string)
}

// New builds the host-route source. hostID returns the host's stable id
// used as the route entity's owning host.id.
func New(hostID func() string) *Source {
	return &Source{
		hostID:     hostID,
		readRoutes: platformRoutes,
		forwarding: platformForwarding,
	}
}

// Observe reads the routing table and builds the snapshot. A read failure
// reports ok=false so the detector keeps the last good snapshot instead of
// deleting the host's routes on a transient error.
func (s *Source) Observe() (entity.Observation, bool) {
	routes, err := s.readRoutes()
	if err != nil {
		return entity.Observation{}, false
	}
	hostID := s.hostID()
	obs := buildObservation(hostID, routes)
	if s.forwarding != nil {
		v4, v6 := s.forwarding()
		obs = withForwarding(obs, hostID, v4, v6)
	}
	return obs.WithScope(entity.ScopeHostRoute), true
}

// buildObservation maps routes to network.route entities owned by the host.
// It emits a host entity for owner counters, route entities, and shared
// network.address nodes for usable gateways.
func buildObservation(hostID string, routes []hostRoute) entity.Observation {
	if hostID == "" {
		return entity.Observation{}
	}

	candidates := filterRoutes(routes)
	selected := selectRoutes(candidates)

	// senhub.routes.omitted counts every route the source listed and this
	// producer did not emit, whatever the reason (bound, invalid row,
	// unusable gateway, container bridge): a counter that only saw the
	// bound would read 0 while rows were dropped.
	hostKey := map[string]any{idKeyHost: hostID}
	hostAttrs := map[string]any{attrRoutesLimit: maxHostRoutes}
	obs := entity.Observation{
		Entities: []entity.Entity{{Type: entityTypeHost, ID: hostKey, Attributes: hostAttrs}},
	}
	emitted := 0

	addrSeen := map[string]bool{}
	for _, r := range selected {
		rt := effectiveRouteType(r)
		nextHop := r.NextHop
		switch rt {
		case routeTypeLocal, routeTypeReject, routeTypeBlackhole:
			nextHop = zeroNextHop(r.Destination)
		case routeTypeRemote, routeTypeOther:
			if !usableNextHop(nextHop) {
				continue
			}
		}

		if c, ok := entity.CanonicalIP(nextHop); ok {
			nextHop = c
		}

		routeID := map[string]any{
			idKeyHost:             hostID,
			idKeyRouteDestination: r.Destination,
			idKeyNextHopIP:        nextHop,
		}
		attrs := map[string]any{attrRouteType: rt}
		if r.Metric > 0 {
			attrs[attrMetric] = r.Metric
		}
		// A reject or blackhole route ends by design: the interface the
		// kernel attaches to it (often lo) represents the drop, not an
		// egress, and drawn as one it would put a hop to loopback on the path.
		if r.Iface != "" && rt != routeTypeReject && rt != routeTypeBlackhole {
			attrs[attrEgressInterface] = r.Iface
		}
		if r.ProtocolCode != nil {
			attrs[attrRouteProtocolCode] = *r.ProtocolCode
		}
		if r.ProtocolName != "" {
			attrs[attrRouteProtocol] = r.ProtocolName
		}

		emitted++
		obs.Entities = append(obs.Entities, entity.Entity{
			Type:       entityTypeNetworkRoute,
			ID:         routeID,
			Attributes: attrs,
		})
		obs.Relations = append(obs.Relations, entity.Relation{
			Type:     relHasRoute,
			FromType: entityTypeHost,
			FromID:   hostKey,
			ToType:   entityTypeNetworkRoute,
			ToID:     routeID,
		})

		if rt != routeTypeRemote && rt != routeTypeOther {
			continue
		}
		if !usableNextHop(nextHop) || !entity.AddressEdgeAllowed(nextHop) || entity.IsContainerBridgeIface(r.Iface) {
			continue
		}
		addrID := map[string]any{idKeyNetworkAddress: nextHop}
		if !addrSeen[nextHop] {
			addrSeen[nextHop] = true
			obs.Entities = append(obs.Entities, entity.Entity{Type: entityTypeNetworkAddress, ID: addrID})
		}
		obs.Relations = append(obs.Relations, entity.Relation{
			Type:     relNextHopVia,
			FromType: entityTypeNetworkRoute,
			FromID:   routeID,
			ToType:   entityTypeNetworkAddress,
			ToID:     addrID,
		})
	}
	hostAttrs[attrRoutesOmitted] = len(routes) - emitted
	return obs
}

func filterRoutes(routes []hostRoute) []hostRoute {
	out := make([]hostRoute, 0, len(routes))
	for _, r := range routes {
		rt := effectiveRouteType(r)
		if rt == routeTypeInvalid {
			continue
		}
		if rt == routeTypeLocal && entity.IsContainerBridgeIface(r.Iface) {
			continue
		}
		if (rt == routeTypeRemote || rt == routeTypeOther) && !usableNextHop(r.NextHop) {
			continue
		}
		out = append(out, r)
	}
	return out
}

func selectRoutes(routes []hostRoute) []hostRoute {
	var protected, rest []hostRoute
	for _, r := range routes {
		if isAlwaysKeepRoute(r) {
			protected = append(protected, r)
			continue
		}
		rest = append(rest, r)
	}
	sort.Slice(rest, func(i, j int) bool {
		if rest[i].Destination != rest[j].Destination {
			return rest[i].Destination < rest[j].Destination
		}
		return rest[i].NextHop < rest[j].NextHop
	})

	if len(protected) >= maxHostRoutes {
		return protected
	}
	remaining := maxHostRoutes - len(protected)
	if remaining >= len(rest) {
		return append(protected, rest...)
	}
	return append(protected, rest[:remaining]...)
}

func isAlwaysKeepRoute(r hostRoute) bool {
	if r.Destination == "0.0.0.0/0" || r.Destination == "::/0" {
		return true
	}
	switch effectiveRouteType(r) {
	case routeTypeLocal, routeTypeReject, routeTypeBlackhole:
		return true
	default:
		return false
	}
}

func effectiveRouteType(r hostRoute) string {
	if r.RouteType != "" {
		return r.RouteType
	}
	if usableNextHop(r.NextHop) {
		return routeTypeRemote
	}
	return routeTypeLocal
}

func zeroNextHop(dest string) string {
	if p, err := netip.ParsePrefix(dest); err == nil && p.Addr().Is6() {
		return "::"
	}
	return "0.0.0.0"
}

func usableNextHop(s string) bool {
	if s == "" {
		return false
	}
	addr, err := netip.ParseAddr(s)
	if err != nil {
		return false
	}
	return !addr.Unmap().IsUnspecified()
}
