package entity

import (
	"testing"
	"time"
)

// sourceFunc adapts a function to the Source interface for tests.
type sourceFunc func() Observation

func (f sourceFunc) Observe() Observation { return f() }

func TestObservation_ToEvents(t *testing.T) {
	ts := time.Unix(1000, 0).UTC()
	obs := Observation{
		Entities:  []Entity{{Type: "db", ID: map[string]any{"db.instance.id": "pg1"}}},
		Relations: []Relation{{Type: "monitors", FromType: "service.instance", ToType: "db"}},
	}
	got := obs.toEvents(ts, time.Minute)
	if len(got) != 2 {
		t.Fatalf("got %d events, want 2", len(got))
	}
	// Entities precede relations so endpoints land before edges.
	if got[0].Kind != EntityState || got[0].Entity == nil || got[0].Entity.Type != "db" {
		t.Errorf("event[0] = %+v, want db entity_state", got[0])
	}
	if got[1].Kind != RelationState || got[1].Relation == nil {
		t.Errorf("event[1] = %+v, want relation_state", got[1])
	}
	for i, ev := range got {
		if !ev.Time.Equal(ts) || ev.Interval != time.Minute {
			t.Errorf("event[%d] time/interval = %v/%v, want %v/1m", i, ev.Time, ev.Interval, ts)
		}
	}
}

// TestSource_DetectorMergesAndTracksDeletes verifies the detector folds a
// source's observation into the snapshot, and that when the source stops
// observing an item the tracker emits its delete.
func TestSource_DetectorMergesAndTracksDeletes(t *testing.T) {
	resetSourcesForTest()
	defer resetSourcesForTest()

	present := true
	RegisterSource(sourceFunc(func() Observation {
		if !present {
			return Observation{}
		}
		return Observation{
			Entities: []Entity{{
				Type: "db", ID: map[string]any{"db.instance.id": "pg1"},
				Attributes: map[string]any{"db.system.name": "postgresql"},
			}},
			Relations: []Relation{{
				Type:     "monitors",
				FromType: "service.instance", FromID: map[string]any{"service.instance.id": "agent-1"},
				ToType: "db", ToID: map[string]any{"db.instance.id": "pg1"},
			}},
		}
	}))

	var got []Event
	d := NewDetector(
		func() (HostIdentity, error) { return HostIdentity{ID: "h-1", Name: "web", OSType: "linux"}, nil },
		func() AgentIdentity { return AgentIdentity{InstanceID: "agent-1"} },
		time.Minute,
	)
	tr := NewTracker(func(ev Event) { got = append(got, ev) })

	// Cycle 1: foundation (host + service.instance + runs_on) + db + monitors = 5.
	d.reconcile(tr, time.Unix(1000, 0).UTC())
	if len(got) != 5 {
		t.Fatalf("cycle 1 published %d events, want 5 (3 foundation + db + monitors)", len(got))
	}
	var dbState, monState int
	for _, ev := range got {
		if ev.Kind == EntityState && ev.Entity != nil && ev.Entity.Type == "db" {
			dbState++
		}
		if ev.Kind == RelationState && ev.Relation != nil && ev.Relation.Type == "monitors" {
			monState++
		}
	}
	if dbState != 1 || monState != 1 {
		t.Fatalf("cycle 1: dbState=%d monState=%d, want 1/1", dbState, monState)
	}

	// Cycle 2: the source no longer observes the db → db + monitors deleted.
	got = nil
	present = false
	d.reconcile(tr, time.Unix(1060, 0).UTC())

	var dbDel, monDel int
	for _, ev := range got {
		if ev.Kind == EntityDelete && ev.Entity != nil && ev.Entity.Type == "db" {
			dbDel++
		}
		if ev.Kind == RelationDelete && ev.Relation != nil && ev.Relation.Type == "monitors" {
			monDel++
		}
	}
	if dbDel != 1 || monDel != 1 {
		t.Errorf("cycle 2: dbDel=%d monDel=%d, want 1/1 (db + monitors retired)", dbDel, monDel)
	}
}
