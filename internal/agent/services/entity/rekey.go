package entity

import (
	"sync/atomic"
)

// Announcing an identity change so it does not read as an outage.
//
// When a producer corrects an identity, the consumer does not see a corrected
// value: it sees the old node stop being fed and a new one appear, with nothing
// linking them. Left alone the old one dies of liveness expiry, which their
// change feed reports as "the producer went quiet" — so a fleet-wide re-key
// produces the exact signature of a mass outage, on the feed people use to
// triage incidents.
//
// Agreed with the topology consumer (2026-08-13): the producer says it
// explicitly instead. Two announcements, both unconditional:
//
//   - an entity.delete on the OLD identity, so the retirement carries
//     delete_source=producer — someone decided, rather than someone stopped
//     answering;
//   - a same_as edge from the new identity to the old one, so both timelines
//     stay joinable forever without anything being rewritten.
//
// Unconditional includes a fresh install where the old entity never existed:
// their engine resolves the identity first and returns without doing anything
// when nothing matches, and an orphan alias parks in their reconciliation
// buffer then drops with one log line. They preferred absorbing that to having
// a producer persist state that would need maintaining, migrating and
// re-justifying every six months.
//
// Generalised out of the db-specific version written for #740, because the
// second re-key (#742) needed exactly the same thing for a different type.
type RekeyAnnouncer struct {
	entityType string
	idKey      string
	legacyID   string
	newID      string
	// announced counts emissions. The delete repeats for the first few cycles
	// rather than firing once at startup: PublishEvent is best-effort and drops
	// under backpressure, and at process start the entity pump may not have
	// subscribed yet, so a single shot can vanish in silence. Repetition is free
	// because the consumer's delete is idempotent by contract — written that way
	// for a producer that restarts.
	announced atomic.Int32
}

// RekeyAnnounceCycles is how many observation cycles carry the announcement.
// Three covers a late subscriber and a dropped event without turning a one-off
// migration signal into a recurring one.
const RekeyAnnounceCycles = 3

// NewRekeyAnnouncer returns an announcer for an identity that changed, or nil
// when the two spellings are equal — nothing was re-keyed, so there is no node
// to retire and no alias to draw. Returning nil rather than an inert object
// keeps "did anything change" answerable at the call site.
func NewRekeyAnnouncer(entityType, idKey, legacyID, newID string) *RekeyAnnouncer {
	if legacyID == "" || newID == "" || legacyID == newID {
		return nil
	}
	return &RekeyAnnouncer{entityType: entityType, idKey: idKey, legacyID: legacyID, newID: newID}
}

// Announce publishes the delete of the old identity. Call it once per
// observation cycle; it stops on its own after RekeyAnnounceCycles.
func (r *RekeyAnnouncer) Announce() {
	if r == nil {
		return
	}
	if r.announced.Add(1) > RekeyAnnounceCycles {
		return
	}
	PublishEvent(Event{
		Kind: EntityDelete,
		Entity: &Entity{
			Type: r.entityType,
			ID:   map[string]any{r.idKey: r.legacyID},
		},
	})
}

// SameAs returns the alias edge from the new identity to the retired one, or
// ok=false once the announcement window has closed.
//
// The edge carries basis and confidence because the consumer treats a same_as
// WITHOUT a valid confidence as inert — it collapses nothing (ADR 0020). An
// alias missing them travels the whole wire and then does nothing, which is the
// worst of both: the cost of sending it and none of the effect.
func (r *RekeyAnnouncer) SameAs() (Relation, bool) {
	if r == nil || r.announced.Load() > RekeyAnnounceCycles {
		return Relation{}, false
	}
	return Relation{
		Type:     RelSameAs,
		FromType: r.entityType,
		FromID:   map[string]any{r.idKey: r.newID},
		ToType:   r.entityType,
		ToID:     map[string]any{r.idKey: r.legacyID},
		Attributes: map[string]any{
			"basis":      "rekey",
			"confidence": 1.0,
		},
	}, true
}
