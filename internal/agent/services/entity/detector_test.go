package entity

import (
	"errors"
	"sync"
	"testing"
	"time"
)

func TestEventChannel_PublishSubscribe(t *testing.T) {
	resetEventChannelForTest()
	defer resetEventChannelForTest()

	ch := SubscribeEvents(8)
	PublishEvent(Event{Kind: EntityState, Entity: &Entity{Type: "host", ID: map[string]any{"host.id": "h-001"}}})

	select {
	case ev := <-ch:
		if ev.Entity == nil || ev.Entity.Type != "host" {
			t.Fatalf("got %+v, want host entity", ev)
		}
	case <-time.After(time.Second):
		t.Fatal("no event received")
	}
}

func TestDetector_Reconcile_PublishesFoundation(t *testing.T) {
	var (
		mu  sync.Mutex
		got []Event
	)
	publish := func(ev Event) { mu.Lock(); got = append(got, ev); mu.Unlock() }

	d := NewDetector(
		func() (HostIdentity, error) {
			return HostIdentity{ID: "h-001", Name: "web-1", OSType: "linux"}, nil
		},
		func() AgentIdentity {
			return AgentIdentity{InstanceID: "agent-7f3a", ServiceName: "senhub-agent", ServiceVersion: "1.0.0"}
		},
		time.Minute,
	)
	d.reconcile(NewTracker(publish), time.Unix(1780272000, 0).UTC())

	if len(got) != 3 {
		t.Fatalf("published %d events, want 3 (host, service.instance, runs_on)", len(got))
	}
	if got[0].Entity == nil || got[0].Entity.Type != "host" {
		t.Errorf("event[0] = %+v, want host", got[0])
	}
	if got[2].Relation == nil || got[2].Relation.Type != "runs_on" {
		t.Errorf("event[2] = %+v, want runs_on", got[2])
	}

	// The emitted Interval is slacked above the tick cadence so a late
	// heartbeat does not expire a live entity.
	wantInterval := time.Minute * livenessSlackFactor
	for i, ev := range got {
		if ev.Interval != wantInterval {
			t.Errorf("event[%d].Interval = %v, want %v (cadence × slack)", i, ev.Interval, wantInterval)
		}
	}
}

func TestDetector_Reconcile_SkipsOnMissingIdentity(t *testing.T) {
	publishCount := 0
	publish := func(Event) { publishCount++ }

	// Host identity errors → skip the cycle, emit nothing.
	d := NewDetector(
		func() (HostIdentity, error) { return HostIdentity{}, errors.New("host info unavailable") },
		func() AgentIdentity { return AgentIdentity{InstanceID: "agent-7f3a"} },
		time.Minute,
	)
	d.reconcile(NewTracker(publish), time.Unix(1780272000, 0).UTC())

	// Empty agent instance id → also skip.
	d2 := NewDetector(
		func() (HostIdentity, error) { return HostIdentity{ID: "h-001"}, nil },
		func() AgentIdentity { return AgentIdentity{} },
		time.Minute,
	)
	d2.reconcile(NewTracker(publish), time.Unix(1780272000, 0).UTC())

	if publishCount != 0 {
		t.Fatalf("published %d events on missing identity, want 0", publishCount)
	}
}
