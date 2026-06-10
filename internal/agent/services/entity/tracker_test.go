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
	tr := NewTracker(func(ev Event) { got = append(got, ev) })

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

func TestTracker_StableIdentityNoSpuriousDelete(t *testing.T) {
	var got []Event
	tr := NewTracker(func(ev Event) { got = append(got, ev) })
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
