package entity

import (
	"testing"
	"time"
)

// sourceFunc adapts a function to the Source interface for tests.
type sourceFunc func() Observation

func (f sourceFunc) Observe() (Observation, bool) { return f(), true }

// TestObservation_FoldRelationships verifies a relation is folded onto its
// source entity (matched by the From endpoint) as a bare embedded
// relationship, and that a relation with no source entity is reported as an
// orphan rather than silently dropped.
func TestObservation_FoldRelationships(t *testing.T) {
	obs := Observation{
		Entities: []Entity{{
			Type: "service.instance", ID: map[string]any{"service.instance.id": "agent-1"},
		}},
		Relations: []Relation{
			{
				Type:     "monitors",
				FromType: "service.instance", FromID: map[string]any{"service.instance.id": "agent-1"},
				ToType: "db", ToID: map[string]any{"db.instance.id": "pg1"},
				// Edge attributes are intentionally dropped by the bare embed.
				Attributes: map[string]any{"source": "probe"},
			},
			{
				Type:     "runs_on",
				FromType: "service.instance", FromID: map[string]any{"service.instance.id": "missing"},
				ToType: "host", ToID: map[string]any{"host.id": "h-1"},
			},
		},
	}

	entities, orphans := obs.foldRelationships()
	if len(entities) != 1 {
		t.Fatalf("got %d entities, want 1", len(entities))
	}
	rels := entities[0].Relationships
	if len(rels) != 1 {
		t.Fatalf("source entity carries %d relationships, want 1", len(rels))
	}
	if rels[0].Type != "monitors" || rels[0].TargetType != "db" || rels[0].TargetID["db.instance.id"] != "pg1" {
		t.Errorf("embedded relationship = %+v, want monitors → db pg1", rels[0])
	}
	if len(orphans) != 1 || orphans[0].Type != "runs_on" {
		t.Errorf("orphans = %+v, want 1 (runs_on with no source entity)", orphans)
	}
}

// TestSource_DetectorMergesAndTracksDeletes verifies the detector folds a
// source's observation into the snapshot — the monitors edge rides embedded on
// the service.instance entity — and that when the source stops observing the
// db, the db is deleted and its monitors edge is retired by absence (the
// service.instance heartbeat simply stops listing it).
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
	tr := NewTracker(func(ev Event) { got = append(got, ev) }, 0)

	// Cycle 1: foundation (host + service.instance) + db = 3 entity events.
	// runs_on and monitors both fold onto the service.instance.
	d.reconcile(tr, time.Unix(1000, 0).UTC())
	if len(got) != 3 {
		t.Fatalf("cycle 1 published %d events, want 3 (host + service.instance + db)", len(got))
	}
	var dbState int
	var svcRels []Relationship
	for _, ev := range got {
		if ev.Entity == nil {
			continue
		}
		switch ev.Entity.Type {
		case "db":
			if ev.Kind == EntityState {
				dbState++
			}
		case "service.instance":
			svcRels = ev.Entity.Relationships
		}
	}
	if dbState != 1 {
		t.Fatalf("cycle 1: dbState=%d, want 1", dbState)
	}
	if !hasRel(svcRels, "runs_on", "host") || !hasRel(svcRels, "monitors", "db") {
		t.Fatalf("cycle 1: service.instance relationships = %+v, want runs_on→host and monitors→db", svcRels)
	}

	// Cycle 2: the source no longer observes the db. The db gets an explicit
	// delete; the monitors edge is retired by absence (svc no longer lists it).
	got = nil
	present = false
	d.reconcile(tr, time.Unix(1060, 0).UTC())

	var dbDel int
	for _, ev := range got {
		if ev.Entity == nil {
			continue
		}
		if ev.Kind == EntityDelete && ev.Entity.Type == "db" {
			dbDel++
		}
		if ev.Kind == EntityState && ev.Entity.Type == "service.instance" {
			if hasRel(ev.Entity.Relationships, "monitors", "db") {
				t.Errorf("cycle 2: monitors edge still present on service.instance, want retired by absence")
			}
		}
	}
	if dbDel != 1 {
		t.Errorf("cycle 2: dbDel=%d, want 1 (db retired)", dbDel)
	}
}

func hasRel(rels []Relationship, typ, targetType string) bool {
	for _, r := range rels {
		if r.Type == typ && r.TargetType == targetType {
			return true
		}
	}
	return false
}

// flakySource lets a test flip between a good observation and a failed one.
type flakySource struct {
	obs Observation
	ok  bool
}

func (f *flakySource) Observe() (Observation, bool) { return f.obs, f.ok }

func testDetector() *Detector {
	return NewDetector(
		func() (HostIdentity, error) { return HostIdentity{ID: "h-1", Name: "web", OSType: "linux"}, nil },
		func() AgentIdentity { return AgentIdentity{InstanceID: "agent-1"} },
		time.Minute,
	)
}

func dbObservation(id string) Observation {
	return Observation{
		Entities: []Entity{{
			Type: "db", ID: map[string]any{"db.instance.id": id},
			Attributes: map[string]any{"db.system.name": "postgresql"},
		}},
		// Anchor the db with the agent's monitors edge so it survives the
		// anti-orphan guard (a real monitored db always carries this).
		Relations: []Relation{{
			Type:     "monitors",
			FromType: "service.instance", FromID: map[string]any{"service.instance.id": "agent-1"},
			ToType: "db", ToID: map[string]any{"db.instance.id": id},
		}},
	}
}

func countKind(events []Event, k Kind) int {
	n := 0
	for _, ev := range events {
		if ev.Kind == k {
			n++
		}
	}
	return n
}

// TestSource_FailedObserveKeepsLastGood pins the #272 acceptance: a
// transient source failure (ok=false) must NOT delete what the source
// used to report — the detector serves the last good observation until
// the staleness TTL, after which the deletes legitimately fire.
func TestSource_FailedObserveKeepsLastGood(t *testing.T) {
	resetSourcesForTest()
	defer resetSourcesForTest()

	src := &flakySource{obs: dbObservation("pg1"), ok: true}
	unregister := RegisterSource(src)
	defer unregister()

	var got []Event
	d := testDetector()
	tr := NewTracker(func(ev Event) { got = append(got, ev) }, 0)

	t0 := time.Unix(1000, 0).UTC()
	d.reconcile(tr, t0)
	if countKind(got, EntityDelete) != 0 {
		t.Fatalf("cycle 1 must not delete: %+v", got)
	}

	// Cycle 2: the sweep fails. The db must STILL be present (no delete).
	src.ok = false
	got = nil
	d.reconcile(tr, t0.Add(time.Minute))
	if countKind(got, EntityDelete) != 0 {
		t.Fatalf("transient failure must not delete the source's entities: %+v", got)
	}

	// Beyond the TTL the silence is real: the delete fires.
	got = nil
	d.reconcile(tr, t0.Add(time.Minute+lastGoodTTL))
	if countKind(got, EntityDelete) != 1 {
		t.Fatalf("beyond lastGoodTTL the entities must expire, got %+v", got)
	}
}

// TestSource_UnregisterRetiresEntities pins the #272 acceptance: after
// unregister, the source's entities are absence-deleted on the next
// cycle and a stop/reload does not duplicate the source.
func TestSource_UnregisterRetiresEntities(t *testing.T) {
	resetSourcesForTest()
	defer resetSourcesForTest()

	src := &flakySource{obs: dbObservation("pg1"), ok: true}
	unregister := RegisterSource(src)

	var got []Event
	d := testDetector()
	tr := NewTracker(func(ev Event) { got = append(got, ev) }, 0)

	t0 := time.Unix(1000, 0).UTC()
	d.reconcile(tr, t0)

	unregister()
	unregister() // idempotent: double-call must not corrupt the registry

	got = nil
	d.reconcile(tr, t0.Add(time.Minute))
	if countKind(got, EntityDelete) != 1 {
		t.Fatalf("unregistered source's entities must be deleted, got %+v", got)
	}
	if len(registeredSources()) != 0 {
		t.Fatalf("registry must be empty after unregister")
	}

	// Reload: re-registering starts a fresh id, no duplicates.
	unregister2 := RegisterSource(src)
	defer unregister2()
	if n := len(registeredSources()); n != 1 {
		t.Fatalf("re-register must yield exactly 1 source, got %d", n)
	}
}
