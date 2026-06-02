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

	dbB := Event{Kind: EntityState, Time: t1, Entity: &Entity{
		Type: "db", ID: map[string]any{"db.instance.id": "B"},
	}}
	relAB := Event{Kind: RelationState, Time: t1, Relation: &Relation{
		Type:     "monitors",
		FromType: "host", FromID: map[string]any{"host.id": "A"},
		ToType: "db", ToID: map[string]any{"db.instance.id": "B"},
	}}

	// Cycle 1: host A + db B + relation A→B present.
	tr.Reconcile([]Event{hostState("A", "a", t1), dbB, relAB}, t1)

	// Cycle 2: only host A remains. B and the relation disappear.
	got = nil
	tr.Reconcile([]Event{hostState("A", "a", t2)}, t2)

	if len(got) != 3 {
		t.Fatalf("cycle 2 published %d events, want 3 (A state + B delete + relation delete)", len(got))
	}
	var stateA, delB, delRel int
	for _, ev := range got {
		switch {
		case ev.Kind == EntityState && ev.Entity != nil && ev.Entity.ID["host.id"] == "A":
			stateA++
		case ev.Kind == EntityDelete && ev.Entity != nil && ev.Entity.ID["db.instance.id"] == "B":
			delB++
			if !ev.Time.Equal(t2) {
				t.Errorf("db delete time = %v, want %v (the cycle it was observed gone)", ev.Time, t2)
			}
			if len(ev.Entity.Attributes) != 0 {
				t.Errorf("delete carries descriptive attributes, want none: %v", ev.Entity.Attributes)
			}
		case ev.Kind == RelationDelete && ev.Relation != nil && ev.Relation.Type == "monitors":
			delRel++
			if !ev.Time.Equal(t2) {
				t.Errorf("relation delete time = %v, want %v", ev.Time, t2)
			}
		}
	}
	if stateA != 1 || delB != 1 || delRel != 1 {
		t.Errorf("got stateA=%d delB=%d delRel=%d, want 1/1/1", stateA, delB, delRel)
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
