package entity

import (
	"sync"
	"testing"
	"time"
)

// Delete reasons (#806). The tracker only ever sees absence; the detector is
// the level that knows whether a source answered, went quiet or stopped
// existing. Each test below drives one of those four paths end to end and
// asserts the reason that reaches the event stream, because getting this wrong
// is not a cosmetic bug: two of the four paths say nothing about the resource,
// and calling them "terminated" is what turns a config edit into an incident.

// fakeSource is a Source whose observation and trustworthiness are settable
// between reconcile cycles.
type fakeSource struct {
	mu  sync.Mutex
	obs Observation
	ok  bool
}

func (f *fakeSource) Observe() (Observation, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.obs, f.ok
}

func (f *fakeSource) set(o Observation, ok bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.obs, f.ok = o, ok
}

// dbTarget is a probe-monitored target. Sources report flat entities and flat
// relations; the detector folds the relation onto the entity, which is what
// anchors it past the unanchored-node guard and onto the wire.
var dbTargetID = map[string]any{"db.instance.id": "postgresql:5432@h-001"}

func dbTarget() Entity {
	return Entity{Type: "db", ID: dbTargetID}
}

// monitoredDB is the healthy view: the db plus its runs_on edge to the host.
func monitoredDB() Observation {
	return Observation{
		Entities: []Entity{dbTarget()},
		Relations: []Relation{{
			Type:     "runs_on",
			FromType: "db",
			FromID:   dbTargetID,
			ToType:   "host",
			ToID:     map[string]any{"host.id": "h-001"},
		}},
	}
}

func newReasonDetector() *Detector {
	return NewDetector(
		func() (HostIdentity, error) {
			return HostIdentity{ID: "h-001", Name: "web-1", OSType: "linux"}, nil
		},
		func() AgentIdentity {
			return AgentIdentity{InstanceID: "agent-7f3a", ServiceName: "senhub-agent", ServiceVersion: "1.0.0"}
		},
		time.Minute,
	)
}

// collectDeletes drives two cycles and returns the delete events from the
// second, keyed by entity type.
func deletesByType(events []Event) map[string]Event {
	out := map[string]Event{}
	for _, ev := range events {
		if ev.Kind == EntityDelete && ev.Entity != nil {
			out[ev.Entity.Type] = ev
		}
	}
	return out
}

func TestDeleteReason_SourceAnswersAndDropsIt_Terminated(t *testing.T) {
	src := &fakeSource{obs: monitoredDB(), ok: true}
	unregister := RegisterSource(src)
	defer unregister()

	var mu sync.Mutex
	var got []Event
	tr := NewTracker(func(ev Event) { mu.Lock(); got = append(got, ev); mu.Unlock() }, 0)
	d := newReasonDetector()
	t0 := time.Unix(1780272000, 0).UTC()

	d.reconcile(tr, t0)
	// The source is healthy and now reports nothing: the legitimate way to say
	// everything it watched is gone.
	src.set(Observation{}, true)
	got = nil
	d.reconcile(tr, t0.Add(time.Minute))

	del, ok := deletesByType(got)["db"]
	if !ok {
		t.Fatalf("no delete for the db entity; got %d events", len(got))
	}
	if del.DeleteReason != ReasonTerminated {
		t.Errorf("DeleteReason = %q, want %q", del.DeleteReason, ReasonTerminated)
	}
}

func TestDeleteReason_SourceFailingBeyondTTL_Unmonitored(t *testing.T) {
	src := &fakeSource{obs: monitoredDB(), ok: true}
	unregister := RegisterSource(src)
	defer unregister()

	var mu sync.Mutex
	var got []Event
	tr := NewTracker(func(ev Event) { mu.Lock(); got = append(got, ev); mu.Unlock() }, 0)
	d := newReasonDetector()
	t0 := time.Unix(1780272000, 0).UTC()

	d.reconcile(tr, t0)
	// The source stops being trustworthy. Past lastGoodTTL the cached view is
	// no longer served and the absence-delete fires — but we stopped SEEING
	// the database, we did not learn that it went away.
	src.set(Observation{}, false)
	got = nil
	d.reconcile(tr, t0.Add(lastGoodTTL+time.Minute))

	del, ok := deletesByType(got)["db"]
	if !ok {
		t.Fatalf("no delete for the db entity; got %d events", len(got))
	}
	if del.DeleteReason != ReasonUnmonitored {
		t.Errorf("DeleteReason = %q, want %q — a source that stopped answering says nothing about the resource",
			del.DeleteReason, ReasonUnmonitored)
	}
}

// TestDeleteReason_ProbeRemovedFromConfig_Unmonitored is the incident case.
// Someone edits probes.d/ and removes a probe; its targets vanish from the
// graph. A consumer reading that as "the database is gone" pages a team over a
// configuration change.
func TestDeleteReason_ProbeRemovedFromConfig_Unmonitored(t *testing.T) {
	src := &fakeSource{obs: monitoredDB(), ok: true}
	unregister := RegisterSource(src)

	var mu sync.Mutex
	var got []Event
	tr := NewTracker(func(ev Event) { mu.Lock(); got = append(got, ev); mu.Unlock() }, 0)
	d := newReasonDetector()
	t0 := time.Unix(1780272000, 0).UTC()

	d.reconcile(tr, t0)
	// The probe shuts down and unregisters its source.
	unregister()
	got = nil
	d.reconcile(tr, t0.Add(time.Minute))

	del, ok := deletesByType(got)["db"]
	if !ok {
		t.Fatalf("no delete for the db entity; got %d events", len(got))
	}
	if del.DeleteReason != ReasonUnmonitored {
		t.Errorf("DeleteReason = %q, want %q — removing a probe from the configuration must never read as the resource dying",
			del.DeleteReason, ReasonUnmonitored)
	}
	if del.DeleteReason == ReasonTerminated {
		t.Error("a configuration change reported as a termination (#806)")
	}
}

func TestDeleteReason_EntityLosesItsAnchor_ParentRemoved(t *testing.T) {
	src := &fakeSource{obs: monitoredDB(), ok: true}
	unregister := RegisterSource(src)
	defer unregister()

	var mu sync.Mutex
	var got []Event
	tr := NewTracker(func(ev Event) { mu.Lock(); got = append(got, ev); mu.Unlock() }, 0)
	d := newReasonDetector()
	t0 := time.Unix(1780272000, 0).UTC()

	d.reconcile(tr, t0)
	// Same entity, same identity, but the source no longer reports its
	// anchoring relation: the unanchored-node guard drops it before the wire.
	src.set(Observation{Entities: []Entity{dbTarget()}}, true)
	got = nil
	d.reconcile(tr, t0.Add(time.Minute))

	del, ok := deletesByType(got)["db"]
	if !ok {
		t.Fatalf("no delete for the db entity; got %d events", len(got))
	}
	if del.DeleteReason != ReasonParentRemoved {
		t.Errorf("DeleteReason = %q, want %q", del.DeleteReason, ReasonParentRemoved)
	}
}

// TestDeleteReason_NotSpelledCascade guards the vocabulary boundary: "cascade"
// belongs to the consumer's delete_source axis and means Toise retired an edge
// whose far end died. Spelling a producer reason that way would recreate one
// level down the ambiguity the two axes exist to remove.
func TestDeleteReason_NotSpelledCascade(t *testing.T) {
	for _, r := range []string{ReasonTerminated, ReasonParentRemoved, ReasonUnmonitored} {
		if r == "cascade" {
			t.Errorf("%q collides with the delete_source axis", r)
		}
	}
	if ReasonParentRemoved != "parent_removed" {
		t.Errorf("ReasonParentRemoved = %q, want parent_removed", ReasonParentRemoved)
	}
}
