// Package hostnet is the host-side routing entity source (entity Lot 4, #212):
// it reads the local kernel routing table and emits the host's routes as
// network.route entities on the frozen entity rail — the host-side equivalent
// of snmp_poll's device routing, with no SNMP needed.
//
// Contract (topology-as-entities, ADR 0022, frozen with Toise #222/#87): a
// host route is a network.route entity identified by {host.id, route.destination}
// (route.destination a canonical CIDR), carrying the next hop as a scalar
// next_hop.ip attribute — network.address (and the gateway as its own entity)
// is deferred, so the next hop stays an IP, not a node. The route attaches to
// the host by has_route (host → network.route), mirroring has_interface.
//
// This supersedes the earlier gateway-as-network.device + routes_via model:
// the host does not discover its gateway as a managed device (that is what an
// SNMP poll does); it records where its traffic egresses. Scope: routes with a
// next hop (default + gateway'd routes); link-local/connected routes are
// skipped (no next hop, low value, would flood).
package hostnet

import (
	"net"
	"os"
	"strconv"
	"strings"

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
	relHasRoute              = "has_route"
	relNextHopVia            = "next_hop_via"

	procRoute = "/proc/net/route"
)

// hostRoute is one next-hop route parsed from the kernel routing table.
type hostRoute struct {
	Destination string // canonical CIDR, e.g. "0.0.0.0/0"
	NextHop     string // gateway IP (dotted)
	Metric      int64
	// Iface names the interface the route leaves by, when the platform
	// says so; a container bridge's gateway is the same on every host.
	Iface string
}

// Source implements entity.Source for the host routing table.
type Source struct {
	hostID     func() string
	readRoutes func() ([]hostRoute, error)
}

// New builds the host-route source. hostID returns the host's stable id
// (gopsutil HostID) used as the route entity's owning host.id.
func New(hostID func() string) *Source {
	return &Source{
		hostID:     hostID,
		readRoutes: platformRoutes,
	}
}

// Observe reads the routing table and builds the snapshot: /proc/net/route
// on Linux, the IP helper API on Windows. Both are fast and non-blocking.
// A read failure reports ok=false so the detector keeps the last good
// snapshot instead of deleting the host's routes on a transient error
// (audit D3); where no reader exists the source never contributes.
func (s *Source) Observe() (entity.Observation, bool) {
	routes, err := s.readRoutes()
	if err != nil {
		return entity.Observation{}, false
	}
	return buildObservation(s.hostID(), routes).WithScope(entity.ScopeHostRoute), true
}

// procRoutes reads the Linux kernel routing table.
func procRoutes() ([]hostRoute, error) {
	b, err := os.ReadFile(procRoute)
	if err != nil {
		return nil, err
	}
	return parseProcRoute(b), nil
}

// buildObservation maps next-hop routes → network.route entities owned by the
// host (has_route). The next hop is carried both as the scalar next_hop.ip
// attribute and as a network.address entity the route reaches via next_hop_via:
// that same network.address {ip} node is bound_to a device's interface by the
// SNMP poll when the gateway is a managed device, so the shared address joins
// the host and device topology graphs.
func buildObservation(hostID string, routes []hostRoute) entity.Observation {
	if hostID == "" || len(routes) == 0 {
		return entity.Observation{}
	}
	hostKey := map[string]any{idKeyHost: hostID}

	obs := entity.Observation{}
	addrSeen := map[string]bool{}
	for _, r := range routes {
		// Identity after IP-FORWARD-MIB (RFC 4292), which indexes a route on
		// its destination AND its next hop: two routes to one destination
		// through two gateways are two routes. The next hop is always
		// present because only indirect routes are emitted; emitting direct
		// (on-link) routes would break this identity. A gateway change is a
		// delete and a create. TOS and policy, also in the MIB index, are
		// not read: routes differing only by them collapse.
		nextHop := r.NextHop
		if c, ok := entity.CanonicalIP(nextHop); ok {
			nextHop = c
		}
		routeID := map[string]any{idKeyHost: hostID, idKeyRouteDestination: r.Destination, idKeyNextHopIP: nextHop}
		attrs := map[string]any{}
		if r.Metric > 0 {
			attrs[attrMetric] = r.Metric
		}
		// The egress interface explains an absent next_hop_via (a
		// container bridge) and tells two routes to one destination
		// apart when a host has two paths.
		if r.Iface != "" {
			attrs[attrEgressInterface] = r.Iface
		}
		if len(attrs) == 0 {
			attrs = nil
		}
		obs.Entities = append(obs.Entities, entity.Entity{
			Type:       entityTypeNetworkRoute,
			ID:         routeID,
			Attributes: attrs,
		})
		obs.Relations = append(obs.Relations, entity.Relation{
			Type:     relHasRoute,
			FromType: entityTypeHost, FromID: hostKey,
			ToType: entityTypeNetworkRoute, ToID: routeID,
		})

		// The gateway IP as a shared network.address node + next_hop_via edge —
		// but only when the gateway is globally unique. A host-local gateway
		// (e.g. a Docker bridge) is the same value on every host, so a shared
		// node would falsely join unrelated hosts (Toise otel-mapping contract);
		// the next hop still rides as the host-scoped next_hop.ip attribute above.
		if entity.IsHostLocalAddressStr(nextHop) || entity.IsContainerBridgeIface(r.Iface) {
			continue
		}
		addrID := map[string]any{idKeyNetworkAddress: nextHop}
		if !addrSeen[nextHop] {
			addrSeen[nextHop] = true
			obs.Entities = append(obs.Entities, entity.Entity{Type: entityTypeNetworkAddress, ID: addrID})
		}
		obs.Relations = append(obs.Relations, entity.Relation{
			Type:     relNextHopVia,
			FromType: entityTypeNetworkRoute, FromID: routeID,
			ToType: entityTypeNetworkAddress, ToID: addrID,
		})
	}
	return obs
}

// parseProcRoute returns the distinct next-hop routes from the Linux
// /proc/net/route table. Destination/Gateway/Mask are hex little-endian;
// connected routes (zero gateway) are skipped — only routes with a next hop
// are topology. Routes are deduped by destination CIDR, first-seen order.
func parseProcRoute(data []byte) []hostRoute {
	var out []hostRoute
	seen := map[string]bool{}
	for i, line := range strings.Split(string(data), "\n") {
		if i == 0 { // header
			continue
		}
		f := strings.Fields(line)
		if len(f) < 8 {
			continue
		}
		gw := hexLEToIP(f[2])
		if gw == "" || gw == "0.0.0.0" {
			continue
		}
		dst := hexLEToIP(f[1])
		if dst == "" {
			continue
		}
		prefix := maskHexToPrefix(f[7])
		if prefix < 0 {
			continue
		}
		// Canonical route.destination identity: explicit prefix, host bits
		// zeroed (entity.CanonicalCIDR) — same rule as the SNMP-side routes, so
		// the host and device views of one route derive byte-identical ids.
		cidr, ok := entity.CanonicalCIDR(dst, prefix)
		if !ok {
			continue
		}
		if seen[cidr+" "+gw] {
			continue
		}
		seen[cidr+" "+gw] = true
		var metric int64
		if m, err := strconv.ParseInt(f[6], 10, 64); err == nil {
			metric = m
		}
		out = append(out, hostRoute{Destination: cidr, NextHop: gw, Metric: metric, Iface: f[0]})
	}
	return out
}

// hexLEToIP decodes an 8-hex-char little-endian IPv4 (as in /proc/net/route)
// to dotted form.
func hexLEToIP(h string) string {
	if len(h) != 8 {
		return ""
	}
	b := make([]byte, 4)
	for i := 0; i < 4; i++ {
		v, err := strconv.ParseUint(h[2*i:2*i+2], 16, 8)
		if err != nil {
			return ""
		}
		b[3-i] = byte(v) // little-endian → network order
	}
	return net.IP(b).String()
}

// maskHexToPrefix decodes an 8-hex-char little-endian IPv4 netmask to its
// prefix length (e.g. "00FFFFFF" → 24, "00000000" → 0). Returns -1 on a
// malformed or non-canonical mask.
func maskHexToPrefix(h string) int {
	dotted := hexLEToIP(h)
	if dotted == "" {
		return -1
	}
	ip := net.ParseIP(dotted).To4()
	if ip == nil {
		return -1
	}
	ones, bits := net.IPMask(ip).Size()
	if bits == 0 { // non-canonical mask
		return -1
	}
	return ones
}
