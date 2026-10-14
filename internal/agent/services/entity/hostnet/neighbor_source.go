package hostnet

import (
	"net/netip"
	"sort"

	"senhub-agent.go/internal/agent/services/entity"
	"senhub-agent.go/internal/agent/services/entity/macindex"
)

const (
	entityTypeNetworkNeighbor            = "network.neighbor"
	entityTypeNetworkInterface           = "network.interface"
	idKeyNeighborMAC                     = "mac"
	idKeyInterfaceName                   = "network.interface.name"
	idKeyNetworkDevice                   = "network.device.id"
	attrNetworkNeighborAddresses         = "network.neighbor.addresses"
	attrNetworkNeighborObservedInterface = "network.neighbor.observed.interface"
	relNeighborOf                        = "neighbor_of"
	relResolvesTo                        = "resolves_to"
	attrBasis                            = "basis"
	attrConfidence                       = "confidence"

	resolveConfidenceARPNDP = 0.9
)

// neighborDropPrefixes are CIDRs reused by convention on unrelated hosts:
// link-local ranges and the Docker/CNI default bridge networks. This is a
// floor, not a test: more such conventions may need adding as deployments
// bring them to light.
var neighborDropPrefixes = []netip.Prefix{
	netip.MustParsePrefix("169.254.0.0/16"),
	netip.MustParsePrefix("fe80::/10"),
	netip.MustParsePrefix("172.17.0.0/16"),
	netip.MustParsePrefix("10.88.0.0/16"),
}

// NeighborSource implements entity.Source for the host's ARP/NDP neighbours.
type NeighborSource struct {
	hostID        func() string
	readNeighbors func() ([]hostNeighbor, error)
	macIndex      *macindex.Index
}

// NewNeighborSource builds the host-neighbour source. hostID returns the
// host's stable id, used as the host-owner half of the neighbour identity.
func NewNeighborSource(hostID func() string) *NeighborSource {
	return &NeighborSource{
		hostID:        hostID,
		readNeighbors: platformNeighbors,
		macIndex:      macindex.Default,
	}
}

// Observe reads the host neighbour table and builds the current snapshot.
// A failed read reports ok=false so the detector keeps the last good
// observation instead of deleting neighbours on a transient error.
func (s *NeighborSource) Observe() (entity.Observation, bool) {
	if s.hostID == nil {
		return entity.Observation{}, false
	}
	host := s.hostID()
	if host == "" {
		return entity.Observation{}, false
	}
	neighbors, err := s.readNeighbors()
	if err != nil {
		return entity.Observation{}, false
	}
	return buildNeighborObservation(host, neighbors, s.macIndex).WithScope(entity.ScopeHostARP), true
}

type neighborGroup struct {
	MAC     string
	ips     map[string]bool
	ifaces  map[string]bool
	hasIPv4 bool
	hasIPv6 bool
}

func buildNeighborObservation(hostID string, neighbors []hostNeighbor, macIndex *macindex.Index) entity.Observation {
	if hostID == "" || len(neighbors) == 0 {
		return entity.Observation{}
	}
	if macIndex == nil {
		macIndex = macindex.Default
	}
	neighbors = dedupeAndSortNeighbors(neighbors)

	groups := make(map[string]*neighborGroup)
	for _, n := range neighbors {
		if n.MAC == "" || n.Iface == "" {
			continue
		}
		g := groups[n.MAC]
		if g == nil {
			g = &neighborGroup{MAC: n.MAC, ips: make(map[string]bool), ifaces: make(map[string]bool)}
			groups[n.MAC] = g
		}
		g.ips[n.IP] = true
		g.ifaces[n.Iface] = true
		if ip, err := netip.ParseAddr(n.IP); err == nil {
			if ip.Is4() {
				g.hasIPv4 = true
			} else if ip.Is6() {
				g.hasIPv6 = true
			}
		}
	}

	macs := make([]string, 0, len(groups))
	for mac := range groups {
		macs = append(macs, mac)
	}
	sort.Strings(macs)

	obs := entity.Observation{}
	for _, mac := range macs {
		g := groups[mac]
		addrs := sortedSet(g.ips)
		if len(addrs) == 0 || allAddrsInDropRanges(addrs) {
			continue
		}
		ifaces := sortedSet(g.ifaces)
		if len(ifaces) == 0 {
			continue
		}

		basis := "ndp"
		if g.hasIPv4 {
			basis = "arp"
		}

		id := map[string]any{idKeyHost: hostID, idKeyNeighborMAC: mac}
		attrs := map[string]any{
			attrNetworkNeighborAddresses:         addrs,
			attrNetworkNeighborObservedInterface: ifaces[0],
		}
		obs.Entities = append(obs.Entities, entity.Entity{
			Type:       entityTypeNetworkNeighbor,
			ID:         id,
			Attributes: attrs,
		})

		for _, iface := range ifaces {
			obs.Relations = append(obs.Relations, entity.Relation{
				Type:     relNeighborOf,
				FromType: entityTypeNetworkNeighbor,
				FromID:   id,
				ToType:   entityTypeNetworkInterface,
				ToID:     map[string]any{idKeyHost: hostID, idKeyInterfaceName: iface},
			})
		}

		if macIndex.Enabled() {
			if target, ok := macIndex.Lookup(mac); ok && target.DeviceID != "" && target.Name != "" {
				obs.Relations = append(obs.Relations, entity.Relation{
					Type:     relResolvesTo,
					FromType: entityTypeNetworkNeighbor,
					FromID:   id,
					ToType:   entityTypeNetworkInterface,
					ToID:     map[string]any{idKeyNetworkDevice: target.DeviceID, idKeyInterfaceName: target.Name},
					Attributes: map[string]any{
						attrBasis:      basis,
						attrConfidence: resolveConfidenceARPNDP,
					},
				})
			}
		}
	}
	return obs
}

func allAddrsInDropRanges(addrs []string) bool {
	if len(addrs) == 0 {
		return true
	}
	for _, addr := range addrs {
		ip, err := netip.ParseAddr(addr)
		if err != nil {
			continue
		}
		inRange := false
		for _, p := range neighborDropPrefixes {
			if p.Contains(ip) {
				inRange = true
				break
			}
		}
		if !inRange {
			return false
		}
	}
	return true
}

func sortedSet(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for s := range m {
		if s != "" {
			out = append(out, s)
		}
	}
	sort.Strings(out)
	return out
}

// DepartureReason implements entity.DepartureSource: the source reads the
// kernel's neighbour cache, so a neighbour it stops listing was forgotten by
// the cache, not seen to leave.
func (s *NeighborSource) DepartureReason(entity.Entity) string { return entity.ReasonAgedOut }

var _ entity.DepartureSource = (*NeighborSource)(nil)
