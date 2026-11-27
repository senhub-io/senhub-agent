package entity

import (
	"sync"
	"testing"
	"time"
)

// Startup hold (#625). A source that has not answered once yet (a slow first
// scan after a restart) must not let a partial first report go out: the
// consumer would drop that source's relations, then re-create them minutes
// later. The first report therefore waits, bounded in time, until every
// registered source has answered at least once.

func TestDetector_FirstReportWaitsForASourceThatHasNotAnsweredYet(t *testing.T) {
	var mu sync.Mutex
	var events []Event
	tr := NewTracker(func(ev Event) {
		mu.Lock()
		defer mu.Unlock()
		events = append(events, ev)
	}, 0)
	d := newReasonDetector()
	src := &fakeSource{obs: monitoredDB(), ok: false}
	unregister := RegisterSource(src)
	defer unregister()
	t0 := time.Unix(1780272000, 0).UTC()

	d.reconcile(tr, t0)
	mu.Lock()
	if len(events) != 0 {
		mu.Unlock()
		t.Fatalf("first report held while a source never answered: got %d events, want 0", len(events))
	}
	mu.Unlock()

	src.set(monitoredDB(), true)
	d.reconcile(tr, t0.Add(time.Minute))
	mu.Lock()
	defer mu.Unlock()
	hasDB := false
	for _, ev := range events {
		if ev.Entity != nil && ev.Entity.Type == "db" {
			hasDB = true
		}
	}
	if !hasDB {
		t.Fatalf("expected the db entity once the source answered, got %d events", len(events))
	}
}

func TestDetector_FirstReportGoesOutAfterTheHoldEvenIfASourceNeverAnswers(t *testing.T) {
	var mu sync.Mutex
	var events []Event
	tr := NewTracker(func(ev Event) {
		mu.Lock()
		defer mu.Unlock()
		events = append(events, ev)
	}, 0)
	d := newReasonDetector()
	src := &fakeSource{obs: monitoredDB(), ok: false}
	unregister := RegisterSource(src)
	defer unregister()
	t0 := time.Unix(1780272000, 0).UTC()

	d.reconcile(tr, t0)
	d.reconcile(tr, t0.Add(time.Minute))
	mu.Lock()
	if len(events) != 0 {
		mu.Unlock()
		t.Fatalf("report held for the first two ticks: got %d events, want 0", len(events))
	}
	mu.Unlock()

	d.reconcile(tr, t0.Add(2*time.Minute))
	mu.Lock()
	defer mu.Unlock()
	hasHost := false
	for _, ev := range events {
		if ev.Entity != nil && ev.Entity.Type == "host" {
			hasHost = true
		}
	}
	if !hasHost {
		t.Fatalf("expected the foundation (host) after the hold expired, got %d events", len(events))
	}
}
