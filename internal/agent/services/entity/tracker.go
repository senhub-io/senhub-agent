package entity

import (
	"fmt"
	"sort"
	"strings"
	"time"
)

// maxDormant is the default limit on how many dormant (absent but not expired)
// RetireByExpiry entities the tracker keeps. Beyond this, the oldest are
// silently forgotten so a flood of transient identities cannot grow the maps
// without bound.
const maxDormant = 50000

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
	// refresh is the heartbeat grid interval. Unchanged states are
	// suppressed until the next grid slot; zero disables suppression and
	// publishes every state every cycle.
	refresh time.Duration
	// grid is the first publication time that anchors the global heartbeat
	// phase shared by all entities.
	grid time.Time
	// nextGrid is the next time at which unchanged states are due.
	nextGrid time.Time
	// pub maps an identity key to the content hash and emit time of the
	// last published state for that key.
	pub map[string]published
	// expiry holds, for an entity marked RetireByExpiry, the interval its
	// last state announced. Such an entity is never deleted on absence: the
	// consumer expires it once that interval has run since the state it last
	// received, and the tracker forgets it at the same point so a later
	// return is published in full.
	expiry map[string]time.Duration
	// dormantCap is the maximum number of dormant (absent, RetireByExpiry,
	// not yet expired) entities retained before the oldest are forgotten
	// silently. Set to maxDormant by NewTracker; tests may lower it.
	dormantCap int
}

type published struct {
	hash string
	at   time.Time
}

// NewTracker builds a Tracker that emits via publish. refresh is the
// heartbeat grid interval (see Tracker.refresh); zero disables it.
func NewTracker(publish func(Event), refresh time.Duration) *Tracker {
	return &Tracker{
		publish:    publish,
		seen:       map[string]Event{},
		refresh:    refresh,
		pub:        map[string]published{},
		expiry:     map[string]time.Duration{},
		dormantCap: maxDormant,
	}
}

// Reconcile publishes a state event for every entity in current (the full set
// observed this cycle, all state-kind) whose content changed or whose grid
// slot is due, then a delete for every previously seen entity absent from
// current. Deletes are stamped with now. current is expected to carry only
// EntityState events.
//
// reasons maps an entity key to the Reason* value to stamp on its delete, for
// the entities whose disappearance the caller can explain. A key with no entry
// gets ReasonTerminated: the source answered and stopped listing the entity.
// nil is fine and means every delete this cycle is a plain termination.
//
// An entity marked RetireByExpiry is never deleted on absence; it is
// forgotten silently once its announced interval has run since its last
// published state. Absent ones still remembered are capped at dormantCap so a
// flood of short-lived identities cannot grow the maps without bound: past
// the cap the oldest by last publication are forgotten, still without a
// delete.
func (t *Tracker) Reconcile(current []Event, now time.Time, reasons map[string]string) {
	cur := make(map[string]bool, len(current))

	// A ticker fires a little early as often as late: compared exactly, a
	// tick a few milliseconds short of its slot misses it and the heartbeat
	// leaves one tick later, which is how a 120 s cadence turned into
	// alternating 120 s and 180 s gaps. Half a tick of tolerance absorbs it.
	gridDue := false
	if t.refresh > 0 && !t.nextGrid.IsZero() && !now.Before(t.nextGrid.Add(-t.refresh/4)) {
		gridDue = true
		for !t.nextGrid.After(now.Add(t.refresh / 4)) {
			t.nextGrid = t.nextGrid.Add(t.refresh)
		}
	}

	for _, ev := range current {
		if ev.Kind == EntityState {
			if ev.Entity != nil && !ev.Entity.ObservedAt.IsZero() {
				ev.Time = ev.Entity.ObservedAt
			} else if ev.Time.IsZero() {
				ev.Time = now
			}
		}
		k := eventKey(ev)
		cur[k] = true
		t.seen[k] = deleteFor(ev)

		if ev.Entity != nil && ev.Entity.RetireByExpiry {
			t.expiry[k] = ev.Interval
		}

		h := stateHash(ev)
		p, ok := t.pub[k]
		changed := !ok || p.hash != h
		if t.refresh > 0 && !changed && !gridDue {
			continue // unchanged before the next grid slot: suppressed heartbeat
		}
		t.publish(ev)
		t.pub[k] = published{hash: h, at: now}
		if t.grid.IsZero() {
			t.grid = now
			t.nextGrid = now.Add(t.refresh)
		}
	}

	// Collect dormant (absent, not expired) RetireByExpiry keys for possible
	// eviction if they exceed the cap.
	var dormant []string
	for k, del := range t.seen {
		if cur[k] {
			continue
		}
		// A cache that stops listing an entry says nothing about it: no
		// delete. Once the consumer's liveness deadline, armed by the last
		// state it received, has passed, the entity is forgotten silently.
		if del.Entity != nil && del.Entity.RetireByExpiry {
			p, published := t.pub[k]
			if !published || t.expiry[k] <= 0 || now.Sub(p.at) >= t.expiry[k] {
				delete(t.seen, k)
				delete(t.pub, k)
				delete(t.expiry, k)
			} else {
				dormant = append(dormant, k)
			}
			continue
		}
		del.Time = now
		// Absence alone does not say why. The reason is decided upstream, in
		// the detector, which knows whether the source answered, went quiet or
		// stopped existing; here it is only carried. Absent from the map means
		// the source answered and dropped the entity.
		del.DeleteReason = ReasonTerminated
		if r, ok := reasons[k]; ok && r != "" {
			del.DeleteReason = r
		}
		t.publish(del)
		delete(t.seen, k)
		delete(t.pub, k)
		delete(t.expiry, k)
	}

	// If too many dormant entities remain, evict the oldest until we are at
	// the cap. The entities are not expired yet, so we forget them silently
	// (no delete) so that future reappearances are published in full.
	if len(dormant) > t.dormantCap {
		sort.Slice(dormant, func(i, j int) bool {
			pi := t.pub[dormant[i]]
			pj := t.pub[dormant[j]]
			if pi.at.Equal(pj.at) {
				return dormant[i] < dormant[j]
			}
			return pi.at.Before(pj.at)
		})
		excess := len(dormant) - t.dormantCap
		for _, k := range dormant[:excess] {
			delete(t.seen, k)
			delete(t.pub, k)
			delete(t.expiry, k)
		}
	}
}

// Forget drops the record of what was published, so the next Reconcile
// publishes every state in full. Used when a new consumer subscribes: it
// has seen none of the states the suppression window would otherwise hold
// back. The heartbeat grid is not reset, so the global phase is unchanged.
func (t *Tracker) Forget() {
	t.pub = map[string]published{}
}

// stateHash renders the mutable content of a state event (attributes +
// relationships + announced interval) into a stable string, so unchanged
// heartbeats are recognizable. Identity is excluded — it is the map key.
// ObservedAt is excluded: a replayed fact must not count as a change. The
// announced interval is included: when it changes the consumer must learn
// the new liveness deadline at once, not at the next heartbeat.
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
// RetireByExpiry rides along so absence never turns into a delete for it.
func deleteFor(ev Event) Event {
	return Event{
		Kind: EntityDelete,
		Entity: &Entity{
			Type:           ev.Entity.Type,
			ID:             ev.Entity.ID,
			Scope:          ev.Entity.Scope,
			RetireByExpiry: ev.Entity.RetireByExpiry,
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
