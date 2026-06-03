package snmppoll

import (
	"strings"
	"sync"
	"time"

	"senhub-agent.go/internal/agent/services/entity"
	"senhub-agent.go/internal/agent/services/logger"
)

// Entity rail (#185) — Lot 5a: the polled device + its LLDP neighbours as
// network.device entities and adjacent_to relations, on the frozen entity
// emitter. The wire shapes (network.device.id, relation endpoints/attrs) are
// the Toise-frozen contract — see SNMP-OTEL-MAPPING.md Layer 2′; all id-format
// decisions live in resolveDeviceID (lldp.go).

const (
	entityTypeNetworkDevice = "network.device"
	idKeyNetworkDevice      = "network.device.id"
	relAdjacentTo           = "adjacent_to"
	relRoutesVia            = "routes_via"
	relForwardsTo           = "forwards_to"

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
func (s *snmpEntitySource) Observe() entity.Observation {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.cache
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

	obs := s.sweep(client)

	s.mu.Lock()
	s.cache = obs
	s.lastSweep = now
	s.mu.Unlock()
}

// sweep performs the SNMP reads and builds the observation. Best-effort: a
// failed LLDP walk still yields the polled device itself (identity from
// serial/engine/sysName, no neighbours).
func (s *snmpEntitySource) sweep(client snmpClient) entity.Observation {
	topo, err := collectLLDP(client)
	if err != nil {
		s.moduleLogger.Debug().Err(err).Str("target", s.cfg.Target).
			Msg("LLDP walk failed; emitting device without neighbours")
		topo = lldpTopology{}
	}
	routes, err := collectRoutes(client)
	if err != nil {
		s.moduleLogger.Debug().Err(err).Str("target", s.cfg.Target).
			Msg("routing walk failed; emitting device without routes_via")
		routes = nil
	}
	fdb, err := collectFDB(client)
	if err != nil {
		s.moduleLogger.Debug().Err(err).Str("target", s.cfg.Target).
			Msg("FDB walk failed; emitting device without forwards_to")
		fdb = nil
	}
	arp, err := collectARP(client)
	if err != nil {
		s.moduleLogger.Debug().Err(err).Str("target", s.cfg.Target).
			Msg("ARP walk failed; next-hops stay provisional mgmt:")
		arp = nil
	}
	self := readSelfIdentity(client, s.cfg.Target, topo.Local)
	return buildObservation(self, topo, routes, fdb, arp)
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
				di.EngineID = asBytes(b.Value)
			}
		}
	}
	if binds, err := client.WalkRaw(oidSysNameBase); err == nil {
		for _, b := range binds {
			if b.OID == oidSysName {
				di.SysName = octetText(asBytes(b.Value))
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
				if v, ok := asIntVal(b.Value); ok {
					class[idx] = v
				}
			}
		}
	}
	serial := map[string]string{}
	if binds, err := client.WalkRaw(oidEntPhysicalSerialNum); err == nil {
		for _, b := range binds {
			if idx, ok := strings.CutPrefix(b.OID, oidEntPhysicalSerialNum+"."); ok {
				if sn := strings.TrimSpace(octetText(asBytes(b.Value))); sn != "" {
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

// buildObservation maps the polled device, its LLDP neighbours, routing
// next-hops and bridge FDB to the frozen wire shapes (all
// network.device↔network.device):
//   - adjacent_to — one directed edge polled→neighbour (no reciprocal);
//   - routes_via  — one edge per distinct next-hop device (deduped). The
//     next-hop is known as an IP → mgmt:<ip>, UNLESS ARP converges it to a
//     confirmed device's canonical mac:<addr> (Toise D3: resolve before emit);
//   - forwards_to — bridge FDB, restricted to MACs confirmed to be network
//     devices (LLDP neighbour chassis MACs); host MACs are out of scope and
//     would flood the graph (Toise: no card entity yet).
//
// Returns empty when the device cannot be identified (no usable id rung).
func buildObservation(self deviceIdentity, topo lldpTopology, routes []routeRow, fdb []fdbEntry, arp []arpEntry) entity.Observation {
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

	// Confirmed network devices (LLDP neighbour chassis MACs) — gates
	// forwards_to and the ARP next-hop convergence.
	deviceMACs := map[string]bool{}
	for _, n := range topo.Neighbors {
		if n.ChassisIdSubtype == subtypeMacAddress {
			deviceMACs["mac:"+macHex(n.ChassisId)] = true
		}
	}

	// adjacent_to — one directed edge polled→neighbour.
	for _, n := range topo.Neighbors {
		nID := resolveDeviceID(neighborIdentity(n))
		if nID == "" || nID == selfID {
			continue
		}
		addEntity(nID, neighborAttrs(n))
		obs.Relations = append(obs.Relations, entity.Relation{
			Type:     relAdjacentTo,
			FromType: entityTypeNetworkDevice, FromID: deviceKey(selfID),
			ToType: entityTypeNetworkDevice, ToID: deviceKey(nID),
			Attributes: map[string]any{
				"local_port":  n.LocalPortNum,
				"remote_port": renderPortID(n.PortIdSubtype, n.PortId),
			},
		})
	}

	// ARP convergence (Toise D3): an IP bound to a confirmed device MAC lets a
	// provisional mgmt:<ip> next-hop resolve to the device's canonical mac:.
	arpDevice := map[string]string{}
	for _, a := range arp {
		id := "mac:" + a.MAC
		if deviceMACs[id] {
			arpDevice[a.IP] = id
		}
	}
	nextHopID := func(ip string) string {
		if id, ok := arpDevice[ip]; ok {
			return id
		}
		return "mgmt:" + ip
	}

	// routes_via — one edge per distinct (resolved) next-hop, first-seen order.
	var nhOrder []string
	nhCount := map[string]int{}
	for _, r := range routes {
		if r.Type != routeTypeRemote || !usableNextHop(r.NextHop, self.MgmtIP) {
			continue
		}
		id := nextHopID(r.NextHop)
		if id == selfID {
			continue
		}
		if _, seen := nhCount[id]; !seen {
			nhOrder = append(nhOrder, id)
		}
		nhCount[id]++
	}
	for _, nhID := range nhOrder {
		addEntity(nhID, map[string]any{}) // mgmt: new node; converged mac: already emitted
		obs.Relations = append(obs.Relations, entity.Relation{
			Type:     relRoutesVia,
			FromType: entityTypeNetworkDevice, FromID: deviceKey(selfID),
			ToType: entityTypeNetworkDevice, ToID: deviceKey(nhID),
			// One structural edge per next-hop; per-destination dest/mask would
			// flap across routes sharing it, so we carry the destination count.
			// (Attribute shape flagged back to Toise.)
			Attributes: map[string]any{"destinations": int64(nhCount[nhID])},
		})
	}

	// forwards_to — FDB entries whose MAC is a confirmed device (endpoint
	// entity already emitted by adjacent_to). Host MACs are filtered out.
	emittedFwd := map[string]bool{}
	for _, e := range fdb {
		id := "mac:" + e.MAC
		if id == selfID || !deviceMACs[id] || emittedFwd[id] {
			continue
		}
		emittedFwd[id] = true
		obs.Relations = append(obs.Relations, entity.Relation{
			Type:     relForwardsTo,
			FromType: entityTypeNetworkDevice, FromID: deviceKey(selfID),
			ToType: entityTypeNetworkDevice, ToID: deviceKey(id),
			Attributes: map[string]any{"bridge_port": e.BridgePort},
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

// selfAttrs / neighborAttrs carry only observer-independent descriptive
// attributes (ENTITY-DETECTION.md §6b): the same device seen by two agents
// must not flap on last-writer-wins.
func selfAttrs(self deviceIdentity) map[string]any {
	attrs := map[string]any{}
	if self.SysName != "" {
		attrs["sys_name"] = self.SysName
	}
	return attrs
}

func neighborAttrs(n lldpNeighbor) map[string]any {
	attrs := map[string]any{}
	if n.SysName != "" {
		attrs["sys_name"] = n.SysName
	}
	return attrs
}
