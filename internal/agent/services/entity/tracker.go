package entity

import (
	"fmt"
	"sort"
	"strings"
	"time"
)

// Tracker turns a per-cycle snapshot of observed entities/relations into the
// event stream the consumer expects: it re-emits every current item as a
// state event (the heartbeat — coalesced consumer-side) and emits a delete
// for anything that was present last cycle but is absent now.
//
// It holds the last-seen set across cycles, so one Tracker lives for the
// lifetime of a Detector. Foundation entities (host, the agent) never
// disappear while the agent runs, so Lot 1 sees no deletes; the machinery
// exists for Lot 2+, where probe targets come and go.
type Tracker struct {
	publish func(Event)
	// seen maps an item's identity key to the delete event that would
	// retire it, captured from the last state we emitted for that key.
	seen map[string]Event
}

// NewTracker builds a Tracker that emits via publish.
func NewTracker(publish func(Event)) *Tracker {
	return &Tracker{publish: publish, seen: map[string]Event{}}
}

// Reconcile publishes a state event for every item in current (the full set
// observed this cycle, all state-kind), then a delete for every previously
// seen item absent from current. Deletes are stamped with now. current is
// expected to carry only EntityState / RelationState events.
func (t *Tracker) Reconcile(current []Event, now time.Time) {
	cur := make(map[string]bool, len(current))
	for _, ev := range current {
		k := eventKey(ev)
		cur[k] = true
		t.publish(ev)
		t.seen[k] = deleteFor(ev)
	}
	for k, del := range t.seen {
		if cur[k] {
			continue
		}
		del.Time = now
		t.publish(del)
		delete(t.seen, k)
	}
}

// eventKey is the stable identity key of an entity or relation event. It is
// built from the immutable identity only (type + id set, and for relations
// the endpoints), never from mutable descriptive attributes — so a heartbeat
// with changed attributes keeps the same key.
func eventKey(ev Event) string {
	if ev.Relation != nil {
		r := ev.Relation
		return "R\x00" + r.Type +
			"\x00F" + r.FromType + "\x00" + canonicalID(r.FromID) +
			"\x00T" + r.ToType + "\x00" + canonicalID(r.ToID)
	}
	if ev.Entity != nil {
		return "E\x00" + ev.Entity.Type + "\x00" + canonicalID(ev.Entity.ID)
	}
	return ""
}

// canonicalID renders an identity map as a stable, sorted string.
func canonicalID(id map[string]any) string {
	keys := make([]string, 0, len(id))
	for k := range id {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var b strings.Builder
	for i, k := range keys {
		if i > 0 {
			b.WriteByte('\x00')
		}
		b.WriteString(k)
		b.WriteByte('=')
		b.WriteString(scalarString(id[k]))
	}
	return b.String()
}

// deleteFor builds the delete event that retires the item described by a
// state event, carrying only its type + identity (no descriptive attributes).
func deleteFor(ev Event) Event {
	if ev.Relation != nil {
		r := ev.Relation
		return Event{
			Kind: RelationDelete,
			Relation: &Relation{
				Type:     r.Type,
				FromType: r.FromType,
				FromID:   r.FromID,
				ToType:   r.ToType,
				ToID:     r.ToID,
			},
		}
	}
	return Event{
		Kind: EntityDelete,
		Entity: &Entity{
			Type: ev.Entity.Type,
			ID:   ev.Entity.ID,
		},
	}
}

func scalarString(v any) string {
	if s, ok := v.(string); ok {
		return s
	}
	// Identity values are expected to be strings; fall back to a stable
	// rendering for the rare int/bool id component.
	return fmt.Sprintf("%v", v)
}
