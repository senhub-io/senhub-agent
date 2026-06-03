package snmppoll

import "senhub-agent.go/internal/agent/services/entity"

// snmpEntitySource is the snmp_poll probe's hook into the entity rail
// (#185). The metric rail (interfaces, hardware) flows as datapoints; the
// topology class — ARP, routing, bridge-FDB and LLDP — is relationships,
// not numbers, and rides this rail instead: the polled device as a
// network.device entity plus adjacent_to / routes_via / forwards_to
// relations, emitted through the frozen entity emitter on the OTLP log
// signal (see docs/.../ENTITY-DETECTION.md and SNMP-OTEL-MAPPING.md).
//
// Lot 1b scaffolds the seam only. Observe returns an empty snapshot until
// Lot 5 walks the topology MIBs; network.device.id (LLDP chassis-id,
// fallback management IP) is frozen with the Toise team at Lot 5, so no
// network.device entity is emitted before then.
type snmpEntitySource struct {
	cfg *config
}

func newEntitySource(cfg *config) *snmpEntitySource {
	return &snmpEntitySource{cfg: cfg}
}

// Observe returns the complete set of entities and relations this source
// currently sees. Empty until the Lot 5 topology walks land.
func (s *snmpEntitySource) Observe() entity.Observation {
	return entity.Observation{}
}
