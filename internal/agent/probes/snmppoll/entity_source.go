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

	// Polled-device identity OIDs (dotted, no leading dot).
	oidEntPhysicalSerialNum = "1.3.6.1.2.1.47.1.1.1.1.11" // ENTITY-MIB (walk; chassis serial)
	oidSnmpEngineIDBase     = "1.3.6.1.6.3.10.2.1.1"
	oidSnmpEngineID         = "1.3.6.1.6.3.10.2.1.1.0" // SNMP-FRAMEWORK-MIB
	oidSysNameBase          = "1.3.6.1.2.1.1.5"
	oidSysName              = "1.3.6.1.2.1.1.5.0"
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
	self := readSelfIdentity(client, s.cfg.Target, topo.Local)
	return buildObservation(self, topo)
}

// readSelfIdentity reads the polled device's identifiers in the Toise-frozen
// precedence order. Each read is best-effort; resolveDeviceID degrades when a
// more stable id is absent.
func readSelfIdentity(client snmpClient, mgmtIP string, loc lldpLocal) deviceIdentity {
	di := deviceIdentity{MgmtIP: mgmtIP}

	if binds, err := client.WalkRaw(oidEntPhysicalSerialNum); err == nil {
		for _, b := range binds {
			if sn := strings.TrimSpace(octetText(asBytes(b.Value))); sn != "" {
				di.Serial = sn
				break
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

// buildObservation maps the polled device + its LLDP neighbours to the frozen
// wire shapes: network.device↔network.device, adjacent_to as a single directed
// edge polled→neighbour (no reciprocal). Returns empty when the device cannot
// be identified (no usable id rung).
func buildObservation(self deviceIdentity, topo lldpTopology) entity.Observation {
	selfID := resolveDeviceID(self)
	if selfID == "" {
		return entity.Observation{}
	}

	obs := entity.Observation{
		Entities: []entity.Entity{deviceEntity(selfID, selfAttrs(self))},
	}
	for _, n := range topo.Neighbors {
		nID := resolveDeviceID(neighborIdentity(n))
		if nID == "" || nID == selfID {
			continue
		}
		obs.Entities = append(obs.Entities, deviceEntity(nID, neighborAttrs(n)))
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
