package entity

import (
	"testing"
	"time"
)

func hostState(id, name string, ts time.Time) Event {
	return Event{Kind: EntityState, Time: ts, Entity: &Entity{
		Type: "host", ID: map[string]any{"host.id": id},
		Attributes: map[string]any{"host.name": name},
	}}
}

func TestTracker_DeletesDisappearedItems(t *testing.T) {
	var got []Event
	tr := NewTracker(func(ev Event) { got = append(got, ev) }, 0)

	t1 := time.Unix(1000, 0).UTC()
	t2 := time.Unix(1060, 0).UTC()

	// host A carries a monitors edge to db B (embedded, the folded form).
	hostAB := Event{Kind: EntityState, Time: t1, Entity: &Entity{
		Type: "host", ID: map[string]any{"host.id": "A"},
		Attributes:    map[string]any{"host.name": "a"},
		Relationships: []Relationship{{Type: "monitors", TargetType: "db", TargetID: map[string]any{"db.instance.id": "B"}}},
	}}
	dbB := Event{Kind: EntityState, Time: t1, Entity: &Entity{
		Type: "db", ID: map[string]any{"db.instance.id": "B"},
	}}

	// Cycle 1: host A (→ monitors B) + db B present.
	tr.Reconcile([]Event{hostAB, dbB}, t1)

	// Cycle 2: only host A remains, and it no longer lists the monitors edge.
	// B disappears → explicit entity delete; the edge is retired by absence
	// (it simply isn't on A's heartbeat anymore — no edge delete on the wire).
	got = nil
	tr.Reconcile([]Event{hostState("A", "a", t2)}, t2)

	if len(got) != 2 {
		t.Fatalf("cycle 2 published %d events, want 2 (A state + B delete)", len(got))
	}
	var stateA, delB int
	for _, ev := range got {
		switch {
		case ev.Kind == EntityState && ev.Entity != nil && ev.Entity.ID["host.id"] == "A":
			stateA++
			if len(ev.Entity.Relationships) != 0 {
				t.Errorf("A heartbeat still lists relationships, want none: %v", ev.Entity.Relationships)
			}
		case ev.Kind == EntityDelete && ev.Entity != nil && ev.Entity.ID["db.instance.id"] == "B":
			delB++
			if !ev.Time.Equal(t2) {
				t.Errorf("db delete time = %v, want %v (the cycle it was observed gone)", ev.Time, t2)
			}
			if len(ev.Entity.Attributes) != 0 {
				t.Errorf("delete carries descriptive attributes, want none: %v", ev.Entity.Attributes)
			}
		}
	}
	if stateA != 1 || delB != 1 {
		t.Errorf("got stateA=%d delB=%d, want 1/1", stateA, delB)
	}
}

// TestTracker_ScopeRidesStateAndDelete pins #253: an entity's discovery-method
// scope rides its heartbeat state, and the absence-delete the tracker synthesizes
// carries the SAME scope — so the delete is emitted under the same instrumentation
// scope as the state it retires, not the generic entities scope.
func TestTracker_ScopeRidesStateAndDelete(t *testing.T) {
	var got []Event
	tr := NewTracker(func(ev Event) { got = append(got, ev) }, 0)
	t1 := time.Unix(1000, 0).UTC()
	t2 := time.Unix(1060, 0).UTC()

	route := Event{Kind: EntityState, Time: t1, Entity: &Entity{
		Type:  "network.route",
		ID:    map[string]any{"network.device.id": "serial:9:a", "route.destination": "0.0.0.0/0"},
		Scope: ScopeSNMPRoute,
	}}

	tr.Reconcile([]Event{route}, t1)
	if len(got) != 1 || got[0].Entity.Scope != ScopeSNMPRoute {
		t.Fatalf("state must carry scope %q, got %+v", ScopeSNMPRoute, got)
	}

	// Cycle 2: the route disappears → absence-delete must keep the scope.
	got = nil
	tr.Reconcile(nil, t2)
	if len(got) != 1 || got[0].Kind != EntityDelete {
		t.Fatalf("got %d events, want 1 delete", len(got))
	}
	if got[0].Entity.Scope != ScopeSNMPRoute {
		t.Errorf("delete scope = %q, want %q (delete must ride the state's scope)", got[0].Entity.Scope, ScopeSNMPRoute)
	}
}

func TestTracker_StableIdentityNoSpuriousDelete(t *testing.T) {
	var got []Event
	tr := NewTracker(func(ev Event) { got = append(got, ev) }, 0)
	t1 := time.Unix(1000, 0).UTC()
	t2 := time.Unix(1060, 0).UTC()

	// Same entity, changed descriptive attribute between cycles. Identity is
	// unchanged → it is a heartbeat update, never a delete + recreate.
	tr.Reconcile([]Event{hostState("A", "old-name", t1)}, t1)
	got = nil
	tr.Reconcile([]Event{hostState("A", "new-name", t2)}, t2)

	if len(got) != 1 {
		t.Fatalf("got %d events, want 1 (heartbeat state only, no delete)", len(got))
	}
	if got[0].Kind != EntityState || got[0].Entity.Attributes["host.name"] != "new-name" {
		t.Errorf("got %+v, want host A state with updated name", got[0])
	}
}

// TestTracker_SuppressesUnchangedHeartbeats pins the #272 P6 fix: an
// unchanged state is re-published only after the refresh window; a
// changed attribute republishes immediately; a deleted key republishes
// when it reappears.
func TestTracker_SuppressesUnchangedHeartbeats(t *testing.T) {
	var got []Event
	tr := NewTracker(func(ev Event) { got = append(got, ev) }, time.Minute)

	state := func(attr string, ts time.Time) Event {
		return Event{Kind: EntityState, Time: ts, Interval: 3 * time.Minute, Entity: &Entity{
			Type: "db", ID: map[string]any{"db.instance.id": "pg1"},
			Attributes: map[string]any{"state": attr},
		}}
	}

	t0 := time.Unix(1000, 0).UTC()
	tr.Reconcile([]Event{state("a", t0)}, t0)
	if len(got) != 1 {
		t.Fatalf("first state must publish, got %d", len(got))
	}

	// Same content 10s later: suppressed.
	tr.Reconcile([]Event{state("a", t0.Add(10*time.Second))}, t0.Add(10*time.Second))
	if len(got) != 1 {
		t.Fatalf("unchanged state within refresh must be suppressed, got %d events", len(got))
	}

	// Changed content: published immediately.
	tr.Reconcile([]Event{state("b", t0.Add(20*time.Second))}, t0.Add(20*time.Second))
	if len(got) != 2 {
		t.Fatalf("changed state must publish, got %d events", len(got))
	}

	// Unchanged but refresh elapsed: published (liveness heartbeat).
	tr.Reconcile([]Event{state("b", t0.Add(2*time.Minute))}, t0.Add(2*time.Minute))
	if len(got) != 3 {
		t.Fatalf("unchanged state past refresh must republish, got %d events", len(got))
	}

	// Disappears: delete fires; reappearing publishes again at once.
	tr.Reconcile(nil, t0.Add(3*time.Minute))
	if countKind(got, EntityDelete) != 1 {
		t.Fatalf("absence must delete, got %+v", got)
	}
	tr.Reconcile([]Event{state("b", t0.Add(4*time.Minute))}, t0.Add(4*time.Minute))
	if countKind(got, EntityState) != 4 {
		t.Fatalf("reappearance must publish immediately, got %+v", got)
	}
}
