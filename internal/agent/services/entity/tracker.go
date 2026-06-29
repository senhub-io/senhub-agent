package entity

import (
	"fmt"
	"sort"
	"strings"
	"time"
)

// Tracker turns a per-cycle snapshot of observed entities into the event
// stream the consumer expects: it re-emits every current entity as a state
// event (the heartbeat — coalesced consumer-side) and emits a delete for any
// entity that was present last cycle but is absent now.
//
// Relations are not tracked here: they ride embedded on their source entity's
// state and are retired by absence (a heartbeat that stops listing a
// relationship retires it), so only entities have an explicit delete.
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
	// refresh is the change-suppression window: an UNCHANGED state is
	// re-published only when this long has passed since its last emit
	// (audit P6 — full-snapshot heartbeats with no suppression are
	// ~12k records/min at 200 devices). Must stay well under the
	// liveness Interval carried on events (cadence x slack factor) so
	// suppression can never expire a live entity. Zero disables
	// suppression (every state publishes every cycle).
	refresh time.Duration
	// pub maps an identity key to the content hash and emit time of the
	// last published state for that key.
	pub map[string]published
}

type published struct {
	hash string
	at   time.Time
}

// NewTracker builds a Tracker that emits via publish. refresh is the
// change-suppression window (see Tracker.refresh); zero disables it.
func NewTracker(publish func(Event), refresh time.Duration) *Tracker {
	return &Tracker{
		publish: publish,
		seen:    map[string]Event{},
		refresh: refresh,
		pub:     map[string]published{},
	}
}

// Reconcile publishes a state event for every entity in current (the full set
// observed this cycle, all state-kind) whose content changed or whose refresh
// window elapsed, then a delete for every previously seen entity absent from
// current. Deletes are stamped with now. current is expected to carry only
// EntityState events.
func (t *Tracker) Reconcile(current []Event, now time.Time) {
	cur := make(map[string]bool, len(current))
	for _, ev := range current {
		k := eventKey(ev)
		cur[k] = true
		t.seen[k] = deleteFor(ev)

		h := stateHash(ev)
		if p, ok := t.pub[k]; ok && t.refresh > 0 && p.hash == h && now.Sub(p.at) < t.refresh {
			continue // unchanged and fresh: suppressed heartbeat
		}
		t.publish(ev)
		t.pub[k] = published{hash: h, at: now}
	}
	for k, del := range t.seen {
		if cur[k] {
			continue
		}
		del.Time = now
		t.publish(del)
		delete(t.seen, k)
		delete(t.pub, k)
	}
}

// stateHash renders the mutable content of a state event (attributes +
// relationships + liveness interval) into a stable string, so unchanged
// heartbeats are recognizable. Identity is excluded — it is the map key.
func stateHash(ev Event) string {
	if ev.Entity == nil {
		return ""
	}
	var b strings.Builder
	b.WriteString(canonicalID(ev.Entity.Attributes))
	b.WriteByte('\x01')
	rels := make([]string, 0, len(ev.Entity.Relationships))
	for _, r := range ev.Entity.Relationships {
		rels = append(rels, r.Type+"\x02"+r.TargetType+"\x02"+canonicalID(r.TargetID))
	}
	sort.Strings(rels)
	b.WriteString(strings.Join(rels, "\x01"))
	b.WriteByte('\x01')
	b.WriteString(ev.Interval.String())
	return b.String()
}

// eventKey is the stable identity key of an entity event. It is built from the
// immutable identity only (type + id set), never from mutable descriptive
// attributes or the embedded relationships — so a heartbeat with changed
// attributes or relationships keeps the same key.
func eventKey(ev Event) string {
	if ev.Entity != nil {
		return entityKey(ev.Entity.Type, ev.Entity.ID)
	}
	return ""
}

// entityKey is the stable identity key of an entity from its type + id set.
// Shared by the tracker (heartbeat/delete diffing) and the relationship fold
// (matching a relation's source endpoint to an entity).
func entityKey(typ string, id map[string]any) string {
	return "E\x00" + typ + "\x00" + canonicalID(id)
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

// deleteFor builds the delete event that retires the entity described by a
// state event, carrying only its type + identity (no descriptive attributes,
// no relationships). Scope rides along so the delete is emitted under the same
// instrumentation scope (discovery method) as the state it retires (#253).
func deleteFor(ev Event) Event {
	return Event{
		Kind: EntityDelete,
		Entity: &Entity{
			Type:  ev.Entity.Type,
			ID:    ev.Entity.ID,
			Scope: ev.Entity.Scope,
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
