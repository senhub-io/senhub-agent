// Package entity is the public mirror of the agent's entity-detection API
// (senhub-agent.go/internal/agent/services/entity).
//
// It re-exports the surface a probe needs to contribute to the OTel
// entity-event graph: declare the entities and relations it observes by
// implementing Source and registering it. Probe packages — both the
// free-tier probes here and the paid probes from the separate
// senhub-agent-enterprise module — use this mirror because Go forbids
// importing senhub-agent.go/internal/... across module boundaries.
//
// A probe builds Entity / Relation values (identity + descriptive
// attributes), returns them from Source.Observe(), and calls RegisterSource
// from an init(). The detector stamps event_time + the liveness interval and
// drives state/delete emission — the probe never deals with timestamps.
package entity

import ientity "senhub-agent.go/internal/agent/services/entity"

// Entity is a node in the graph: a thing the agent reports on. ID is the
// identifying attribute set — exact and immutable for the entity's lifetime
// (never put a mutable value like a leased IP in ID; that is a descriptive
// Attributes entry). Attributes are descriptive and may change.
type Entity = ientity.Entity

// Relation is a directed edge between two entities, resolved by the exact
// identity of each endpoint.
type Relation = ientity.Relation

// Observation is the complete current set of entities + relations a Source
// sees this cycle. Returning a smaller set than last cycle signals that
// something is gone (the consumer-side delete is emitted for it).
type Observation = ientity.Observation

// Source observes a slice of the infrastructure graph — typically a probe
// reporting the systems it monitors. The detector calls Observe once per
// cycle; it must return the COMPLETE current set (not a delta) and must not
// block.
//
// The boolean reports whether the observation is trustworthy this cycle.
// Return ok=false on a transient collection failure: the detector then
// keeps serving your last good observation (bounded by a staleness TTL)
// instead of treating everything you used to report as deleted. An EMPTY
// observation with ok=true is the legitimate "everything I watched is
// gone".
type Source = ientity.Source

// RegisterSource adds a Source the detector polls every cycle and returns
// the function that unregisters it. Call RegisterSource when the probe
// starts and the returned function when it shuts down — a source that is
// never unregistered keeps heartbeating its cached topology after the
// probe stops, and a reloaded probe duplicates it.
func RegisterSource(s Source) (unregister func()) {
	return ientity.RegisterSource(s)
}
