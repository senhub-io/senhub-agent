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
//   - nodes  → standard OTel entity events (entity_state / entity_delete)
//   - edges  → neutral entity.relation.* extension (relation state / delete)
//   - identity is exact and immutable; attribute values are flat scalars.
package entity

import "time"

// Kind is the kind of event an Event carries. Entity kinds map to the OTel
// entity-event types; relation kinds map to the neutral relation extension.
type Kind uint8

const (
	EntityState Kind = iota
	EntityDelete
	RelationState
	RelationDelete
)

// Entity is a node: a thing in the infrastructure the agent reports on.
//
// ID is the identifying attribute set — it MUST be exact and immutable for
// the lifetime of the entity (a mutable value such as a pid or a leased IP
// belongs in Attributes, never in ID). Attributes are descriptive and may
// change between states. Both maps carry flat scalar values only
// (string / int64 / float64 / bool); the encoder rejects anything else.
type Entity struct {
	Type       string
	ID         map[string]any
	Attributes map[string]any
}

// Relation is a directed edge between two entities, resolved by the exact
// identity of each endpoint. The producer emits both endpoint entities
// before (or with) the relation; the consumer reconciles out-of-order
// arrivals.
type Relation struct {
	Type       string
	FromType   string
	FromID     map[string]any
	ToType     string
	ToID       map[string]any
	Attributes map[string]any
}

// Event is one entity-state/delete or relation-state/delete observation.
// Exactly one of Entity / Relation is set, matching Kind. Time is the
// producer-side event_time. Interval, when non-zero, is the heartbeat
// period a consumer may use as a liveness backstop (Timestamp+Interval) on
// top of explicit deletes.
type Event struct {
	Kind     Kind
	Entity   *Entity
	Relation *Relation
	Time     time.Time
	Interval time.Duration
}
