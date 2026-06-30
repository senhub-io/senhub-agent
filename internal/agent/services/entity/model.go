// Package entity holds the agent's neutral model for OpenTelemetry entity
// events and relation events, plus the detectors that build them from what
// the agent knows and discovers about infrastructure.
//
// The model is deliberately free of any OTel SDK dependency: detectors
// produce neutral Entity/Relation/Event values, and a sink (today the OTLP
// strategy) encodes them onto the wire. This mirrors the probe → datastore
// → strategy split and keeps the agent vendor-neutral — Toise is one
// consumer of the standard OTel entity events, not a coupling.
//
// Wire contract (frozen with the Toise team, see
// docs/developer-guide/engineering/ENTITY-DETECTION.md):
//   - nodes  → standard OTel entity events (entity.state / entity.delete)
//   - edges  → embedded in the source entity's state via entity.relationships
//     (each descriptor names the target only; the source is the emitting
//     entity). A relation the source stops listing is retired by absence —
//     there is no separate edge state/delete on the wire.
//   - identity is exact and immutable; attribute values are flat scalars.
package entity

import "time"

// Kind is the kind of event an Event carries — an entity state or an entity
// delete. Relations are not their own events: they ride embedded on the source
// entity's state (see Entity.Relationships).
type Kind uint8

const (
	EntityState Kind = iota
	EntityDelete
)

// Entity is a node: a thing in the infrastructure the agent reports on.
//
// ID is the identifying attribute set — it MUST be exact and immutable for
// the lifetime of the entity (a mutable value such as a pid or a leased IP
// belongs in Attributes, never in ID). Attributes are descriptive and may
// change between states. Both maps carry flat scalar values only
// (string / int64 / float64 / bool); the encoder rejects anything else.
//
// Relationships are the entity's outgoing edges, embedded in its state event
// (entity.relationships on the wire). The set is full each state — a relation
// dropped from one heartbeat to the next is retired by absence, so there is no
// separate edge delete. The detector folds the relations a Source reports onto
// their source entity here; producers themselves return flat Relations.
//
// Scope carries the provenance of the entity — the discovery METHOD that
// produced it (LLDP vs routing vs IF-MIB vs the host routing table vs the host
// listener inventory). Per the frozen contract (#87) provenance rides the OTel
// instrumentation scope, not an edge attribute, so the sink emits one ScopeLogs
// per method per batch (#253). The detector preserves Scope through the
// relationship fold and the delete (so a delete rides the same scope as its
// state); an empty Scope falls back to the generic entities scope. Values are
// the Scope* constants below, frozen with Toise.
type Entity struct {
	Type          string
	ID            map[string]any
	Attributes    map[string]any
	Relationships []Relationship
	Scope         string
}

// Scope* are the frozen instrumentation-scope names that carry an entity's
// discovery provenance on the wire (#253, contract #87). Each names the METHOD
// a fact was discovered by, not the probe: snmp_poll alone produces three
// (LLDP adjacency, the routing table, the IF-MIB inventory). A sink maps each
// to one OTel instrumentation scope (provider.Logger(name)), so a backend can
// attribute a fact to its method. Names are aligned with the Toise team; an
// entity that leaves Scope empty rides the generic entities scope.
const (
	ScopeSNMPLLDP  = "senhub-agent/snmp-lldp"  // LLDP adjacency (connected_to + discovered neighbours)
	ScopeSNMPRoute = "senhub-agent/snmp-route" // device routing table (network.route)
	ScopeSNMPIFMIB = "senhub-agent/snmp-ifmib" // device + IF-MIB ports/addresses
	ScopeHostRoute = "senhub-agent/host-route" // host kernel routing table
	ScopeHostSvc   = "senhub-agent/host-svc"   // host listening-service inventory
	ScopeHostIface = "senhub-agent/host-iface" // host interface/address inventory
	ScopeHostDep   = "senhub-agent/host-dep"   // host outbound dependency flows
)

// Relationship is one embedded outgoing edge: its type plus the exact identity
// of the target entity. The source is implicit — the Entity that carries it.
// The embedded descriptor is bare (no edge attributes): a fact that must
// survive belongs on an entity, never on the edge.
type Relationship struct {
	Type       string         // relationship.type (runs_on, monitors, …)
	TargetType string         // target entity.type
	TargetID   map[string]any // target entity.id (exact identity)
}

// Relation is a directed edge a Source reports, resolved by the exact identity
// of each endpoint. The producer emits the source-endpoint entity in the same
// observation; the detector folds the relation onto that entity as an embedded
// Relationship before it reaches the wire. From* names the source endpoint,
// To* the target.
type Relation struct {
	Type       string
	FromType   string
	FromID     map[string]any
	ToType     string
	ToID       map[string]any
	Attributes map[string]any
}

// Event is one entity-state or entity-delete observation. Entity is always
// set. Time is the producer-side event_time. Interval, when non-zero, is the
// heartbeat period a consumer may use as a liveness backstop
// (Timestamp+Interval) on top of explicit deletes.
type Event struct {
	Kind     Kind
	Entity   *Entity
	Time     time.Time
	Interval time.Duration
}
