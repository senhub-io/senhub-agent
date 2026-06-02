package entity

import (
	"testing"
	"time"
)

func TestDetectFoundation(t *testing.T) {
	now := time.Unix(1780272000, 0).UTC()
	h := HostIdentity{ID: "h-001", Name: "web-server-1", OSType: "linux"}
	a := AgentIdentity{InstanceID: "agent-7f3a", ServiceName: "senhub-agent", ServiceVersion: "1.0.0"}

	got := DetectFoundation(h, a, now, time.Minute)
	if len(got) != 3 {
		t.Fatalf("got %d events, want 3", len(got))
	}

	// Endpoints (entities) must precede the edge so a single batch carries
	// both endpoints before the relation that references them.
	if got[0].Kind != EntityState || got[0].Entity == nil || got[0].Entity.Type != "host" {
		t.Errorf("event[0] = %+v, want host entity_state", got[0])
	}
	if got[1].Kind != EntityState || got[1].Entity == nil || got[1].Entity.Type != "service.instance" {
		t.Errorf("event[1] = %+v, want service.instance entity_state", got[1])
	}
	if got[2].Kind != RelationState || got[2].Relation == nil || got[2].Relation.Type != "runs_on" {
		t.Errorf("event[2] = %+v, want runs_on relation_state", got[2])
	}

	for i, ev := range got {
		if !ev.Time.Equal(now) {
			t.Errorf("event[%d].Time = %v, want observation time %v", i, ev.Time, now)
		}
		if ev.Interval != time.Minute {
			t.Errorf("event[%d].Interval = %v, want 1m", i, ev.Interval)
		}
	}

	// Identity carries the stable ids; descriptive facts are attributes.
	host := got[0].Entity
	if host.ID["host.id"] != "h-001" {
		t.Errorf("host.id = %v, want h-001", host.ID["host.id"])
	}
	if host.Attributes["host.name"] != "web-server-1" || host.Attributes["os.type"] != "linux" {
		t.Errorf("host attributes = %v", host.Attributes)
	}

	runsOn := got[2].Relation
	if runsOn.FromID["service.instance.id"] != "agent-7f3a" || runsOn.ToID["host.id"] != "h-001" {
		t.Errorf("runs_on endpoints = from %v to %v", runsOn.FromID, runsOn.ToID)
	}
}
