package dbcommon

import (
	"sync/atomic"

	"senhub-agent.go/internal/agent/services/entity"
)

// Announcing the db identity re-key of 0.5.4.
//
// Host-scoping the loopback fallback (#740) changed db.instance.id from
// "127.0.0.1:3306" to "mysql:3306@<host.id>". For the topology consumer that is
// not a corrected value, it is a different object: the old node stops being fed
// and a new one appears, with nothing linking them.
//
// Left alone, the old node dies of liveness expiry, which their change feed
// reports as delete_source=liveness_expiry — literally "the agent went quiet".
// A fleet-wide re-key therefore produces the exact signature of a mass outage,
// on a feed people use to triage incidents. Agreed with them (2026-08-13): the
// producer says it explicitly instead.
//
// Two announcements, both unconditional:
//
//   - an entity.delete on the OLD identity, so the retirement carries
//     delete_source=producer — "someone decided" rather than "someone stopped
//     answering";
//   - a same_as edge from the new identity to the old one, so both timelines
//     stay joinable forever without anything being rewritten. Nothing is
//     merged: the old entity keeps its history, and an as_of read at the
//     cutover shows both and the link between them.
//
// Unconditional on the consumer's own instruction, including on a fresh install
// where the old entity never existed. Their engine resolves the identity first
// and returns without doing anything when nothing matches, so the delete is a
// non-event; the orphan same_as parks in their reconciliation buffer and is
// dropped after a few minutes with one warning line, which they preferred to
// absorb rather than have us persist state for it. State would have to be
// maintained, migrated, and re-justified every six months; two log lines once
// in an agent's life would not.
type RekeyAnnouncer struct {
	legacyID string
	newID    string
	// announced counts emissions. The delete is repeated for the first few
	// cycles rather than fired once at startup: PublishEvent is best-effort and
	// drops under backpressure, and at process start the entity pump may not
	// have subscribed yet, so a single shot can vanish silently. Repetition is
	// free because the consumer's delete is idempotent by contract — written
	// that way precisely for a producer that restarts.
	announced atomic.Int32
}

// rekeyAnnounceCycles is how many observation cycles carry the announcement.
// Three covers a late subscriber and a dropped event without turning a one-off
// migration signal into a recurring one.
const rekeyAnnounceCycles = 3

// NewRekeyAnnouncer returns an announcer for a db whose identity was
// host-scoped, or nil when nothing was re-keyed.
//
// Nil for a remote target: its identity is still address:port, so there is no
// old node to retire and no alias to draw. Returning nil rather than an inert
// object keeps the "did anything change" question answerable at the call site.
func NewRekeyAnnouncer(system, address string, port int, hostID string) *RekeyAnnouncer {
	newID := FallbackInstanceID(system, address, port, hostID)
	legacyID := legacyFallbackInstanceID(address, port)
	if newID == legacyID {
		return nil
	}
	return &RekeyAnnouncer{legacyID: legacyID, newID: newID}
}

// Announce publishes the delete of the old identity. Call it once per
// observation cycle; it stops on its own after rekeyAnnounceCycles.
func (r *RekeyAnnouncer) Announce() {
	if r == nil {
		return
	}
	if r.announced.Add(1) > rekeyAnnounceCycles {
		return
	}
	entity.PublishEvent(entity.Event{
		Kind: entity.EntityDelete,
		Entity: &entity.Entity{
			Type: entity.TypeDB,
			ID:   map[string]any{"db.instance.id": r.legacyID},
		},
	})
}

// SameAs returns the alias edge from the new identity to the retired one, or
// ok=false once the announcement window has closed.
//
// The edge carries basis and confidence because the consumer treats a same_as
// WITHOUT a valid confidence as inert — it collapses nothing (ADR 0020). An
// alias edge missing them travels the whole wire and then does nothing, which
// is the worst of both: the cost of sending it and none of the effect.
func (r *RekeyAnnouncer) SameAs() (entity.Relation, bool) {
	if r == nil || r.announced.Load() > rekeyAnnounceCycles {
		return entity.Relation{}, false
	}
	return entity.Relation{
		Type:     entity.RelSameAs,
		FromType: entity.TypeDB,
		FromID:   map[string]any{"db.instance.id": r.newID},
		ToType:   entity.TypeDB,
		ToID:     map[string]any{"db.instance.id": r.legacyID},
		Attributes: map[string]any{
			"basis":      "rekey",
			"confidence": 1.0,
		},
	}, true
}
