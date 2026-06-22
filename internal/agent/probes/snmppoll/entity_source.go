package snmppoll

import (
	"senhub-agent.go/internal/agent/services/snmpcore"
	"strings"
	"sync"
	"time"

	"senhub-agent.go/internal/agent/services/entity"
	"senhub-agent.go/internal/agent/services/governance"
	"senhub-agent.go/internal/agent/services/logger"
)

// Entity rail (#185, #156) — the polled device as a network.device entity, its
// IF-MIB ports as network.interface entities (has_interface) and its routing
// table as network.route entities (has_route, next hop a scalar next_hop.ip) —
// topology-as-entities, ADR 0022, frozen with Toise #222/#87. Wire shapes
// (network.device.id, interface.name, route.destination) are the Toise-frozen
// contract — see SNMP-OTEL-MAPPING.md Layer 2′; id-format decisions live in
// resolveDeviceID (lldp.go). LLDP adjacency is emitted as bare connected_to
// between the port entities. The legacy device-to-device edges (adjacent_to,
// routes_via, forwards_to) are gone — all replaced by the entity forms.

const (
	entityTypeNetworkDevice    = "network.device"
	entityTypeNetworkRoute     = "network.route"
	entityTypeNetworkInterface = "network.interface"
	entityTypeNetworkAddress   = "network.address"
	idKeyNetworkDevice         = "network.device.id"
	idKeyRouteDestination      = "route.destination"
	idKeyInterfaceName         = "interface.name"
	idKeyNetworkAddress        = "network.address"
	attrNextHopIP              = "next_hop.ip"
	attrRouteMetric            = "metric"
	attrOperState              = "oper.state"
	attrSpeed                  = "speed"
	relConnectedTo             = "connected_to"
	relHasRoute                = "has_route"
	relHasInterface            = "has_interface"
	relBoundTo                 = "bound_to"

	// Polled-device identity OIDs (dotted, no leading dot).
	oidEntPhysicalSerialNum = "1.3.6.1.2.1.47.1.1.1.1.11" // ENTITY-MIB (per physical entity)
	oidEntPhysicalClass     = "1.3.6.1.2.1.47.1.1.1.1.5"  // entPhysicalClass (3 = chassis)
	entPhysicalClassChassis = 3
	oidSnmpEngineIDBase     = "1.3.6.1.6.3.10.2.1.1"
	oidSnmpEngineID         = "1.3.6.1.6.3.10.2.1.1.0" // SNMP-FRAMEWORK-MIB
	oidSysNameBase          = "1.3.6.1.2.1.1.5"
	oidSysName              = "1.3.6.1.2.1.1.5.0"
	oidSysObjectIDBase      = "1.3.6.1.2.1.1.2"
	oidSysObjectID          = "1.3.6.1.2.1.1.2.0" // → vendor PEN
	oidSysServicesBase      = "1.3.6.1.2.1.1.7"
	oidSysServices          = "1.3.6.1.2.1.1.7.0" // OSI-layer bitmask → device.role
)

// snmpEntitySource feeds the entity rail. Observe() never blocks: it returns
// the last cached snapshot. The SNMP topology walks run inside the probe's
// poll cycle (maybeSweep), rate-limited to topologyInterval so topology is
// swept far slower than metrics (Toise: ~5-15 min). The cache is re-emitted by
// the detector at its own faster cadence, so a device does not expire between
// sweeps.
type snmpEntitySource struct {
	cfg          *config
	interval     time.Duration
	moduleLogger *logger.ModuleLogger

	mu        sync.Mutex
	cache     entity.Observation
	lastSweep time.Time
	// swept marks that at least one sweep succeeded; before that the
	// empty cache is reported with ok=false.
	swept bool
	// deviceID + ifNames are the resolved identity of the polled device and its
	// ifIndex→ifName map, cached from the last sweep so the METRIC collector can
	// tag SNMP metrics with network.device.id / interface.name — the same
	// identity as the topology entities, so a backend joins device/interface
	// metrics to their entities. Replaced wholesale each sweep (never mutated in
	// place), so a reader holding the returned map sees a stable snapshot.
	deviceID string
	ifNames  map[string]string
}

func newEntitySource(cfg *config, log *logger.ModuleLogger) *snmpEntitySource {
	iv := cfg.TopologyInterval
	if iv <= 0 {
		iv = defaultTopologyInterval
	}
	return &snmpEntitySource{cfg: cfg, interval: iv, moduleLogger: log}
}

// Observe returns the last cached topology snapshot. Non-blocking; safe to
// call from the detector goroutine.
// Observe serves the cached snapshot. ok=false before the first
// successful sweep (nothing to report yet is not "everything deleted")
// — afterwards the cache always holds the last good sweep, because
// maybeSweep refuses to replace it with a failed one (audit D3).
func (s *snmpEntitySource) Observe() (entity.Observation, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.cache, s.swept
}

// DeviceID returns the resolved network.device.id of the polled device from the
// last sweep ("" before the first sweep). The metric collector tags every
// datapoint with it so device metrics join to the network.device entity.
func (s *snmpEntitySource) DeviceID() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.deviceID
}

// InterfaceNames returns the ifIndex→ifName map from the last sweep (nil before
// the first sweep). The metric collector resolves an interface metric's
// if_index to interface.name so per-port metrics join to the network.interface
// entity. The returned map is a read-only snapshot (replaced, never mutated).
func (s *snmpEntitySource) InterfaceNames() map[string]string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.ifNames
}

// maybeSweep refreshes the cached snapshot when topologyInterval has elapsed,
// reusing the probe's already-connected client. Called from Collect.
func (s *snmpEntitySource) maybeSweep(client snmpClient, now time.Time) {
	s.mu.Lock()
	due := s.lastSweep.IsZero() || now.Sub(s.lastSweep) >= s.interval
	s.mu.Unlock()
	if !due {
		return
	}

	obs, deviceID, ifNames, ok := s.sweep(client)
	if !ok {
		// Identity unresolved (device unreachable or rejecting us):
		// keep the last good snapshot and do NOT stamp lastSweep, so
		// the next Collect retries instead of waiting a full topology
		// interval while the consumer's view decays (audit D3).
		s.moduleLogger.Warn().Str("target", s.cfg.Target).
			Msg("topology sweep failed; keeping previous snapshot and retrying next cycle")
		return
	}

	s.mu.Lock()
	s.cache = obs
	s.deviceID = deviceID
	s.ifNames = ifNames
	s.lastSweep = now
	s.swept = true
	s.mu.Unlock()
}

// sweep performs the SNMP reads and builds the observation. Best-effort: a
// failed LLDP walk still yields the polled device itself (identity from
// serial/engine/sysName, no neighbours). It also returns the resolved device id
// and the ifIndex→ifName map for the metric collector's correlation tags.
func (s *snmpEntitySource) sweep(client snmpClient) (entity.Observation, string, map[string]string, bool) {
	topo, err := collectLLDP(client)
	if err != nil {
		s.moduleLogger.Debug().Err(err).Str("target", s.cfg.Target).
			Msg("LLDP walk failed; emitting device without neighbours")
		topo = lldpTopology{}
	}
	routes, err := collectRoutes(client)
	if err != nil {
		s.moduleLogger.Debug().Err(err).Str("target", s.cfg.Target).
			Msg("routing walk failed; emitting device without routes")
		routes = nil
	}
	ifaces, err := collectInterfaces(client)
	if err != nil {
		s.moduleLogger.Debug().Err(err).Str("target", s.cfg.Target).
			Msg("ifTable walk failed; emitting device without interfaces")
		ifaces = nil
	}
	addrs, err := collectIPAddrs(client)
	if err != nil {
		s.moduleLogger.Debug().Err(err).Str("target", s.cfg.Target).
			Msg("ipAddrTable walk failed; emitting device without addresses")
		addrs = nil
	}
	self := readSelfIdentity(client, s.cfg.Target, topo.Local)

	deviceID := resolveDeviceID(self)
	var ifNames map[string]string
	if len(ifaces) > 0 {
		ifNames = make(map[string]string, len(ifaces))
		for _, ifc := range ifaces {
			if ifc.Name != "" {
				ifNames[ifc.Index] = ifc.Name
			}
		}
	}
	obs := buildObservation(self, topo, routes, ifaces, addrs)
	applyGovernance(obs, s.cfg, self, deviceID)
	// An empty observation here means the device identity could not be
	// resolved — a failed sweep, not an empty network.
	return obs, deviceID, ifNames, len(obs.Entities) > 0
}

// applyGovernance stamps the operator governance attributes onto the polled
// (self) network.device entity: the single-target governance block plus any
// discovery rule that matches the device's facts (its poll IP, vendor and
// sysName), rules layered last. A no-op when nothing is configured or the device
// was not identified.
func applyGovernance(obs entity.Observation, cfg *config, self deviceIdentity, selfID string) {
	if selfID == "" {
		return
	}
	gov := map[string]any{}
	cfg.Governance.MergeInto(gov)
	if cfg.Discovery != nil && len(cfg.Discovery.GovernanceRules) > 0 {
		facts := governance.DeviceFacts{
			IP:      canonIP(self.MgmtIP),
			Vendor:  vendorName(self.VendorPEN),
			SysName: self.SysName,
		}
		for k, v := range cfg.Discovery.GovernanceRules.Apply(facts) {
			gov[k] = v
		}
	}
	if len(gov) == 0 {
		return
	}
	for i := range obs.Entities {
		if obs.Entities[i].Type == entityTypeNetworkDevice && obs.Entities[i].ID[idKeyNetworkDevice] == selfID {
			if obs.Entities[i].Attributes == nil {
				obs.Entities[i].Attributes = map[string]any{}
			}
			for k, v := range gov {
				obs.Entities[i].Attributes[k] = v
			}
			return
		}
	}
}

// readSelfIdentity reads the polled device's identifiers in the Toise-frozen
// precedence order. Each read is best-effort; resolveDeviceID degrades when a
// more stable id is absent.
func readSelfIdentity(client snmpClient, mgmtIP string, loc lldpLocal) deviceIdentity {
	di := deviceIdentity{MgmtIP: mgmtIP}

	// Chassis serial: take the serial of the single entPhysicalClass=chassis
	// row. A stack exposes N chassis rows — leave Serial empty so the id falls
	// to the stack-scoped snmpEngineID rather than a failover-unstable member
	// serial. Picking the chassis row (not "first serial") also avoids latching
	// onto a module/PSU serial that can change on a part swap.
	di.Serial = chassisSerial(client)
	// Vendor PEN namespaces the serial (serial is vendor-scoped, not global).
	if binds, err := client.WalkRaw(oidSysObjectIDBase); err == nil {
		for _, b := range binds {
			if b.OID == oidSysObjectID {
				di.VendorPEN = vendorPEN(asString(b.Value))
			}
		}
	}

	if binds, err := client.WalkRaw(oidSnmpEngineIDBase); err == nil {
		for _, b := range binds {
			if b.OID == oidSnmpEngineID {
				di.EngineID = snmpcore.AsBytes(b.Value)
			}
		}
	}
	if binds, err := client.WalkRaw(oidSysServicesBase); err == nil {
		for _, b := range binds {
			if b.OID == oidSysServices {
				if v, ok := snmpcore.AsInt(b.Value); ok {
					di.Services = v
				}
			}
		}
	}
	if binds, err := client.WalkRaw(oidSysNameBase); err == nil {
		for _, b := range binds {
			if b.OID == oidSysName {
				di.SysName = snmpcore.OctetText(snmpcore.AsBytes(b.Value))
			}
		}
	}
	if di.SysName == "" {
		di.SysName = loc.SysName
	}
	if loc.ChassisIdSubtype == subtypeMacAddress {
		di.ChassisMAC = loc.ChassisId
	}
	return di
}

// chassisSerial returns the serial of the device's single chassis, or "" when
// there is no chassis row, no serial, or MORE THAN ONE chassis (a stack —
// which is identified by its stack-wide snmpEngineID instead). It correlates
// entPhysicalClass and entPhysicalSerialNum by entPhysical index.
func chassisSerial(client snmpClient) string {
	class := map[string]int{}
	if binds, err := client.WalkRaw(oidEntPhysicalClass); err == nil {
		for _, b := range binds {
			if idx, ok := strings.CutPrefix(b.OID, oidEntPhysicalClass+"."); ok {
				if v, ok := snmpcore.AsInt(b.Value); ok {
					class[idx] = v
				}
			}
		}
	}
	serial := map[string]string{}
	if binds, err := client.WalkRaw(oidEntPhysicalSerialNum); err == nil {
		for _, b := range binds {
			if idx, ok := strings.CutPrefix(b.OID, oidEntPhysicalSerialNum+"."); ok {
				if sn := strings.TrimSpace(snmpcore.OctetText(snmpcore.AsBytes(b.Value))); sn != "" {
					serial[idx] = sn
				}
			}
		}
	}

	var chassis []string
	for idx, cls := range class {
		if cls == entPhysicalClassChassis {
			if sn := serial[idx]; sn != "" {
				chassis = append(chassis, sn)
			}
		}
	}
	if len(chassis) == 1 {
		return chassis[0]
	}
	return "" // 0 → no usable serial; >1 → stack, use engineID
}

func asString(v any) string {
	s, _ := v.(string)
	return s
}

// buildObservation maps the polled device, its interfaces, LLDP neighbours,
// routing table and bridge FDB to the frozen wire shapes:
//   - network.interface — one entity per named IF-MIB port the device owns
//     (has_interface), oper.state / speed descriptive; the port inventory that
//     anchors connected_to;
//   - network.route — one entity per distinct destination CIDR the device owns
//     (has_route), next hop a scalar next_hop.ip;
//   - connected_to — bare port-to-port link adjacency from LLDP (supersedes the
//     legacy adjacent_to / forwards_to device-to-device edges, now retired).
//
// Returns empty when the device cannot be identified (no usable id rung).
func buildObservation(self deviceIdentity, topo lldpTopology, routes []routeRow, ifaces []ifaceRow, addrs []ipAddr) entity.Observation {
	selfID := resolveDeviceID(self)
	if selfID == "" {
		return entity.Observation{}
	}

	obs := entity.Observation{}
	emitted := map[string]bool{}
	addEntity := func(id string, attrs map[string]any) {
		if id == "" || emitted[id] {
			return
		}
		emitted[id] = true
		obs.Entities = append(obs.Entities, deviceEntity(id, attrs))
	}
	addEntity(selfID, selfAttrs(self))

	// network.interface — the device's ports as entities it owns. Bounded by
	// the device's port count; notPresent and unnamed rows are skipped, and a
	// duplicate interface.name keeps the first (identity is {device, name}).
	ifaceSeen := map[string]bool{}
	for _, ifc := range ifaces {
		if ifc.Name == "" || ifc.OperStatus == ifOperNotPresent || ifaceSeen[ifc.Name] {
			continue
		}
		ifaceSeen[ifc.Name] = true
		portID := map[string]any{idKeyNetworkDevice: selfID, idKeyInterfaceName: ifc.Name}
		attrs := map[string]any{attrOperState: operStateName(ifc.OperStatus)}
		if ifc.SpeedMbps > 0 {
			attrs[attrSpeed] = ifc.SpeedMbps * 1_000_000 // Mbit/s → bit/s
		}
		obs.Entities = append(obs.Entities, entity.Entity{
			Type: entityTypeNetworkInterface, ID: portID, Attributes: attrs,
		})
		obs.Relations = append(obs.Relations, entity.Relation{
			Type:     relHasInterface,
			FromType: entityTypeNetworkDevice, FromID: deviceKey(selfID),
			ToType: entityTypeNetworkInterface, ToID: portID,
		})
	}

	// interface.name lookups for connected_to: prefer the IF-MIB ifName so the
	// local port matches the network.interface entity emitted above (most gear
	// numbers lldpLocPortNum as ifIndex, so this hits); fall back to the LLDP
	// local-port table.
	ifIndexName := make(map[string]string, len(ifaces))
	for _, ifc := range ifaces {
		ifIndexName[ifc.Index] = ifc.Name
	}

	// connected_to — bare port-to-port link adjacency (supersedes adjacent_to).
	// Both endpoints are network.interface entities; the local one was emitted
	// above. The neighbour device is still emitted as a discovered network.device.
	// The link is skipped when either port cannot be named by exact identity (no
	// phantom port — point 7): an unnamed local port, an unresolvable neighbour,
	// or a MAC-only remote port.
	for _, n := range topo.Neighbors {
		nID := resolveDeviceID(neighborIdentity(n))
		if nID == "" || nID == selfID {
			continue
		}
		addEntity(nID, neighborAttrs(n))

		localIf := ifIndexName[n.LocalPortNum]
		if localIf == "" {
			localIf = topo.Local.Ports[n.LocalPortNum]
		}
		remoteIf := namedPortID(n.PortIdSubtype, n.PortId)
		if localIf == "" || remoteIf == "" {
			continue
		}
		obs.Relations = append(obs.Relations, entity.Relation{
			Type:     relConnectedTo,
			FromType: entityTypeNetworkInterface, FromID: interfacePortKey(selfID, localIf),
			ToType: entityTypeNetworkInterface, ToID: interfacePortKey(nID, remoteIf),
		})
	}

	// network.address — the device's interface IPs as entities, bound to their
	// interface. The SAME network.address {ip} node is referenced by a host's
	// next_hop_via when this device is that host's gateway, so the shared
	// address joins the host and device topology graphs. Keyed by IP alone
	// (a flat address space, the frozen contract's choice). Skipped when the
	// interface cannot be named.
	addrSeen := map[string]bool{}
	for _, a := range addrs {
		ifName := ifIndexName[a.IfIndex]
		if ifName == "" || addrSeen[a.IP] {
			continue
		}
		addrSeen[a.IP] = true
		addrID := map[string]any{idKeyNetworkAddress: a.IP}
		obs.Entities = append(obs.Entities, entity.Entity{Type: entityTypeNetworkAddress, ID: addrID})
		obs.Relations = append(obs.Relations, entity.Relation{
			Type:     relBoundTo,
			FromType: entityTypeNetworkAddress, FromID: addrID,
			ToType: entityTypeNetworkInterface, ToID: interfacePortKey(selfID, ifName),
		})
	}

	// network.route — the polled device's remote routes as entities it owns
	// (has_route, mirror of has_interface), the next hop carried as a scalar
	// next_hop.ip (network.address — the gateway as its own node — is deferred,
	// so no provisional mgmt:/mac: device for it). One entity per destination
	// CIDR, first-seen order; ECMP (a destination with several next-hops) keeps
	// the first. This supersedes the legacy routes_via device-to-device edge.
	routeSeen := map[string]bool{}
	for _, r := range routes {
		if r.Type != routeTypeRemote || r.Destination == "" {
			continue
		}
		if !usableNextHop(r.NextHop, self.MgmtIP) {
			continue
		}
		if routeSeen[r.Destination] {
			continue
		}
		routeSeen[r.Destination] = true

		routeID := map[string]any{idKeyNetworkDevice: selfID, idKeyRouteDestination: r.Destination}
		attrs := map[string]any{attrNextHopIP: r.NextHop}
		if r.Metric > 0 {
			attrs[attrRouteMetric] = int64(r.Metric)
		}
		obs.Entities = append(obs.Entities, entity.Entity{
			Type: entityTypeNetworkRoute, ID: routeID, Attributes: attrs,
		})
		obs.Relations = append(obs.Relations, entity.Relation{
			Type:     relHasRoute,
			FromType: entityTypeNetworkDevice, FromID: deviceKey(selfID),
			ToType: entityTypeNetworkRoute, ToID: routeID,
		})
	}

	return obs
}

func deviceEntity(id string, attrs map[string]any) entity.Entity {
	return entity.Entity{Type: entityTypeNetworkDevice, ID: deviceKey(id), Attributes: attrs}
}

func deviceKey(id string) map[string]any {
	return map[string]any{idKeyNetworkDevice: id}
}

// interfacePortKey is the exact identity of a network.interface entity: its
// owning device plus the port name.
func interfacePortKey(deviceID, ifName string) map[string]any {
	return map[string]any{idKeyNetworkDevice: deviceID, idKeyInterfaceName: ifName}
}

// selfAttrs / neighborAttrs carry only observer-independent descriptive
// attributes (ENTITY-DETECTION.md §6b): the same device seen by two agents
// must not flap on last-writer-wins.
func selfAttrs(self deviceIdentity) map[string]any {
	attrs := map[string]any{}
	add := func(k, v string) {
		if v != "" {
			attrs[k] = v
		}
	}
	add("sys.name", self.SysName)
	add("mgmt.ip", canonIP(self.MgmtIP))
	add("device.role", deviceRole(self.Services))
	add("vendor", vendorName(self.VendorPEN))
	return attrs
}

func neighborAttrs(n lldpNeighbor) map[string]any {
	attrs := map[string]any{}
	if n.SysName != "" {
		attrs["sys.name"] = n.SysName
	}
	return attrs
}

// deviceRole infers a readable role from the IF-MIB-adjacent sysServices
// bitmask (sum of 2^(layer-1) over the OSI layers the device offers): a device
// that forwards at layer 3 is a router, one that only bridges at layer 2 is a
// switch. Returns "" when sysServices is unread or offers neither.
func deviceRole(services int) string {
	switch {
	case services&0x04 != 0: // layer 3 (internet) → forwards/routes
		return "router"
	case services&0x02 != 0: // layer 2 (datalink) only → bridges/switches
		return "switch"
	default:
		return ""
	}
}

// vendorName maps a vendor's IANA Private Enterprise Number to a readable name
// for the common network vendors; an unknown PEN returns "" (the PEN already
// rides in the serial:<PEN>:… identity, so it is never lost).
func vendorName(pen string) string {
	switch pen {
	case "9":
		return "cisco"
	case "2636":
		return "juniper"
	case "30065":
		return "arista"
	case "2011":
		return "huawei"
	case "11":
		return "hp"
	case "674":
		return "dell"
	case "6027":
		return "dell-force10"
	case "25506":
		return "h3c"
	case "1916":
		return "extreme"
	case "14988":
		return "mikrotik"
	case "8072":
		return "net-snmp"
	default:
		return ""
	}
}
