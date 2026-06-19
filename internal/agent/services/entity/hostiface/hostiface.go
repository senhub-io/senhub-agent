// Package hostiface is the host-side interface-inventory entity source (#458):
// it emits the host's own network interfaces and their IP addresses so a
// consumer can resolve a connection peer back to this host.
//
// Contract (topology-as-entities, ADR 0022; identity frozen with Toise, see
// docs/data-model/otel-mapping.md): a host interface is a network.interface
// entity identified by {host.id, interface.name} (the host-owner analogue of a
// device port's {network.device.id, interface.name}); each unicast IP is a
// network.address entity identified by {network.address} (the bare IP).
// Attachment: network.address --bound_to--> network.interface, and the host
// --has_interface--> network.interface (the host endpoint comes from the
// foundation entity merged into the same cycle, so it is referenced, not
// re-emitted — mirroring hostnet's has_route and hostsvc's runs_on).
//
// This is what makes connection topology resolve: a depends_on edge to a
// network.endpoint{server.address=IP} resolves to "this host's listener" only
// when the host's own IP is in the graph as a network.address bound to an
// interface the host has (Toise #184 / senhub-agent #457).
//
// Loopback and link-local addresses are skipped: they never identify a host to
// a remote peer and would only add churn.
package hostiface

import (
	"net"
	"sync"
	"time"

	gnet "github.com/shirou/gopsutil/v3/net"

	"senhub-agent.go/internal/agent/services/entity"
)

const (
	entityTypeHost             = "host"
	entityTypeNetworkInterface = "network.interface"
	entityTypeNetworkAddress   = "network.address"
	idKeyHost                  = "host.id"
	idKeyInterfaceName         = "interface.name"
	idKeyNetworkAddress        = "network.address"
	relBoundTo                 = "bound_to"
	relHasInterface            = "has_interface"

	// attrKeyOperState is the interface operational state. It is one of Toise's
	// stateKeys (ADR 0006: oper_state/admin_state/status), so a link flip on a
	// host interface classifies as entity.state_changed, not a silent update.
	attrKeyOperState = "oper_state"

	// Descriptive attributes (AT13). Dotted-lowercase casing per the contract.
	attrKeyMAC    = "mac"
	attrKeyMTU    = "mtu"
	attrKeyType   = "interface.type"
	attrKeyDuplex = "duplex"
	attrKeySpeed  = "speed" // bit/s, negotiated

	// Interfaces and their addresses change rarely; re-enumerate on a slow
	// cadence and serve the cache in between, like hostsvc.
	defaultRefresh = 60 * time.Second
)

// linkMeta is the per-interface descriptive metadata the /sys layer resolves
// (oper_state plus the AT13 type/duplex/speed). MAC and MTU come straight from
// gopsutil and are not part of this struct.
type linkMeta struct {
	OperState string // up/down
	Type      string // physical/virtual/wireless
	Duplex    string // full/half/unknown
	Speed     int64  // bit/s, 0 = unknown
}

// ifaceAddrs is one interface with its retained unicast IPs and descriptive
// metadata. An interface is emitted even with no IPs (AT13: a down/IP-less NIC
// is still a real entity, so a link going down is a clean state_changed).
type ifaceAddrs struct {
	Name      string
	IPs       []string
	MAC       string
	MTU       int64
	OperState string
	Type      string
	Duplex    string
	Speed     int64
}

// Source implements entity.Source for the host's own interfaces/addresses.
type Source struct {
	hostID     func() string
	interfaces func() (gnet.InterfaceStatList, error)     // nil → gnet.Interfaces
	link       func(name string, flags []string) linkMeta // nil → resolveLinkMeta
	refresh    time.Duration

	mu    sync.Mutex
	cache entity.Observation
	last  time.Time
}

// New builds the host-interface source. hostID returns the host's stable id
// (the same id the foundation host entity uses), so the interfaces hang off
// the same host node.
func New(hostID func() string) *Source {
	return &Source{hostID: hostID, refresh: defaultRefresh}
}

// Observe returns the host's interfaces and addresses. Non-blocking between
// refreshes. A failed enumeration keeps the previous cache and reports
// ok=false rather than replacing the interfaces with an empty set — a
// transient read error must not delete every network.address in the consumer
// (audit D3, mirroring hostsvc).
func (s *Source) Observe() (entity.Observation, bool) {
	s.mu.Lock()
	stale := s.last.IsZero() || time.Since(s.last) >= s.refresh
	if !stale {
		obs := s.cache
		s.mu.Unlock()
		return obs, true
	}
	s.mu.Unlock()

	ias, err := s.enumerate()
	if err != nil {
		return entity.Observation{}, false
	}
	obs := buildObservation(s.hostID(), ias)

	s.mu.Lock()
	s.cache = obs
	s.last = time.Now()
	s.mu.Unlock()
	return obs, true
}

// buildObservation maps interfaces+addresses → network.interface and
// network.address entities, the address bound_to the interface, and the host
// has_interface the interface. Each interface and each IP is emitted once.
func buildObservation(hostID string, ias []ifaceAddrs) entity.Observation {
	if hostID == "" || len(ias) == 0 {
		return entity.Observation{}
	}
	hostKey := map[string]any{idKeyHost: hostID}

	obs := entity.Observation{}
	seenAddr := map[string]bool{}
	for _, ia := range ias {
		if ia.Name == "" {
			continue
		}
		ifaceKey := map[string]any{idKeyHost: hostID, idKeyInterfaceName: ia.Name}
		ifaceEntity := entity.Entity{Type: entityTypeNetworkInterface, ID: ifaceKey, Attributes: ifaceAttributes(ia)}
		obs.Entities = append(obs.Entities, ifaceEntity)
		obs.Relations = append(obs.Relations, entity.Relation{
			Type:     relHasInterface,
			FromType: entityTypeHost, FromID: hostKey,
			ToType: entityTypeNetworkInterface, ToID: ifaceKey,
		})
		for _, ip := range ia.IPs {
			addrKey := map[string]any{idKeyNetworkAddress: ip}
			if !seenAddr[ip] {
				seenAddr[ip] = true
				obs.Entities = append(obs.Entities, entity.Entity{
					Type: entityTypeNetworkAddress, ID: addrKey,
				})
			}
			obs.Relations = append(obs.Relations, entity.Relation{
				Type:     relBoundTo,
				FromType: entityTypeNetworkAddress, FromID: addrKey,
				ToType: entityTypeNetworkInterface, ToID: ifaceKey,
			})
		}
	}
	return obs
}

// ifaceAttributes builds the descriptive attribute map for an interface,
// omitting every empty/zero field. Returns nil when nothing is known.
func ifaceAttributes(ia ifaceAddrs) map[string]any {
	attrs := map[string]any{}
	if ia.OperState != "" {
		attrs[attrKeyOperState] = ia.OperState
	}
	if ia.MAC != "" {
		attrs[attrKeyMAC] = ia.MAC
	}
	if ia.MTU > 0 {
		attrs[attrKeyMTU] = ia.MTU
	}
	if ia.Type != "" {
		attrs[attrKeyType] = ia.Type
	}
	if ia.Duplex != "" {
		attrs[attrKeyDuplex] = ia.Duplex
	}
	if ia.Speed > 0 {
		attrs[attrKeySpeed] = ia.Speed
	}
	if len(attrs) == 0 {
		return nil
	}
	return attrs
}

// enumerate returns every non-loopback interface (AT13: emit all NICs, not only
// those with a resolvable IP) with its descriptive metadata and any resolvable
// unicast IPs (loopback/link-local/unspecified/multicast addresses dropped).
func (s *Source) enumerate() ([]ifaceAddrs, error) {
	ifFn := s.interfaces
	if ifFn == nil {
		ifFn = gnet.Interfaces
	}
	ifaces, err := ifFn()
	if err != nil {
		return nil, err
	}
	lmFn := s.link
	if lmFn == nil {
		lmFn = resolveLinkMeta
	}
	out := make([]ifaceAddrs, 0, len(ifaces))
	for _, ifc := range ifaces {
		if isLoopbackIface(ifc.Flags) {
			continue
		}
		ips := make([]string, 0, len(ifc.Addrs))
		for _, a := range ifc.Addrs {
			if ip := resolvableIP(a.Addr); ip != "" {
				ips = append(ips, ip)
			}
		}
		lm := lmFn(ifc.Name, ifc.Flags)
		out = append(out, ifaceAddrs{
			Name: ifc.Name, IPs: ips,
			MAC: ifc.HardwareAddr, MTU: int64(ifc.MTU),
			OperState: lm.OperState, Type: lm.Type, Duplex: lm.Duplex, Speed: lm.Speed,
		})
	}
	return out, nil
}

// resolveLinkMeta returns the interface descriptive metadata: the sysfs-derived
// fields on Linux (type/duplex/speed and the carrier oper_state), with the
// administrative IFF_UP flag as the oper_state fallback when sysfs is
// unavailable (non-Linux, or operstate "unknown").
func resolveLinkMeta(name string, flags []string) linkMeta {
	lm := readSysLink(name)
	if lm.OperState == "" {
		lm.OperState = operStateFromFlags(flags)
	}
	return lm
}

// operStateFromFlags derives up/down from the gopsutil flag set (IFF_UP). This
// is the administrative state — a coarser signal than the carrier state, used
// only when the precise sysfs operstate is unavailable.
func operStateFromFlags(flags []string) string {
	for _, f := range flags {
		if f == "up" {
			return "up"
		}
	}
	return "down"
}

// isLoopbackIface reports whether the gopsutil flag set marks a loopback
// interface.
func isLoopbackIface(flags []string) bool {
	for _, f := range flags {
		if f == "loopback" {
			return true
		}
	}
	return false
}

// resolvableIP returns the bare IP of a gopsutil interface address ("ip/prefix"
// or a bare ip), or "" if it is not an address a remote peer could be resolved
// against (loopback, link-local, unspecified, multicast, or unparseable).
func resolvableIP(addr string) string {
	host := addr
	if ip, _, err := net.ParseCIDR(addr); err == nil {
		host = ip.String()
	}
	ip := net.ParseIP(host)
	if ip == nil {
		return ""
	}
	if ip.IsLoopback() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() ||
		ip.IsUnspecified() || ip.IsMulticast() {
		return ""
	}
	return ip.String()
}
