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
	"fmt"
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
	attrNextHopIP            = "next_hop.ip"
	attrMetric               = "metric"
	relHasRoute              = "has_route"
	relNextHopVia            = "next_hop_via"

	procRoute = "/proc/net/route"
)

// hostRoute is one next-hop route parsed from the kernel routing table.
type hostRoute struct {
	Destination string // canonical CIDR, e.g. "0.0.0.0/0"
	NextHop     string // gateway IP (dotted)
	Metric      int64
}

// Source implements entity.Source for the host routing table.
type Source struct {
	hostID    func() string
	readRoute func() ([]byte, error)
}

// New builds the host-route source. hostID returns the host's stable id
// (gopsutil HostID) used as the route entity's owning host.id.
func New(hostID func() string) *Source {
	return &Source{
		hostID:    hostID,
		readRoute: func() ([]byte, error) { return os.ReadFile(procRoute) },
	}
}

// Observe reads the routing table and builds the snapshot. Reading the local
// /proc file is fast and non-blocking; on non-Linux (or unreadable /proc) it
// degrades to an empty observation.
func (s *Source) Observe() entity.Observation {
	var routeData []byte
	if b, err := s.readRoute(); err == nil {
		routeData = b
	}
	return buildObservation(s.hostID(), parseProcRoute(routeData))
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
		routeID := map[string]any{idKeyHost: hostID, idKeyRouteDestination: r.Destination}
		attrs := map[string]any{attrNextHopIP: r.NextHop}
		if r.Metric > 0 {
			attrs[attrMetric] = r.Metric
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

		// The gateway IP as a shared network.address node + next_hop_via edge.
		addrID := map[string]any{idKeyNetworkAddress: r.NextHop}
		if !addrSeen[r.NextHop] {
			addrSeen[r.NextHop] = true
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
		cidr := fmt.Sprintf("%s/%d", dst, prefix)
		if seen[cidr] {
			continue
		}
		seen[cidr] = true
		var metric int64
		if m, err := strconv.ParseInt(f[6], 10, 64); err == nil {
			metric = m
		}
		out = append(out, hostRoute{Destination: cidr, NextHop: gw, Metric: metric})
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
