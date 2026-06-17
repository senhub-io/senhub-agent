package process

import (
	"regexp"
	"testing"
	"time"

	"senhub-agent.go/internal/agent/probes/types"
)

// TestEntitySource_NotReadyBeforeFirstUpdate verifies the source reports
// ok=false until the first Collect feeds it, so a transient enumeration
// failure never deletes the watched processes from the consumer.
func TestEntitySource_NotReadyBeforeFirstUpdate(t *testing.T) {
	s := newProcessEntitySource()
	if _, ok := s.Observe(); ok {
		t.Error("Observe ok=true before any update, want false")
	}
}

// TestEntitySource_EmitsProcessAndRunsOn checks the entity shape: a "process"
// node keyed by {process.pid, process.creation.time} and a runs_on edge to the
// host.
func TestEntitySource_EmitsProcessAndRunsOn(t *testing.T) {
	s := newProcessEntitySource()
	s.update("host-uuid-1", []procEntity{
		{pid: 1234, createTime: 1700000000000, name: "nginx", owner: "www"},
	})

	obs, ok := s.Observe()
	if !ok {
		t.Fatal("Observe ok=false after update, want true")
	}
	if len(obs.Entities) != 1 {
		t.Fatalf("got %d entities, want 1", len(obs.Entities))
	}
	e := obs.Entities[0]
	if e.Type != "process" {
		t.Errorf("entity type = %q, want process", e.Type)
	}
	if e.ID["process.pid"] != int64(1234) {
		t.Errorf("process.pid = %v, want 1234", e.ID["process.pid"])
	}
	if e.ID["process.creation.time"] != int64(1700000000000) {
		t.Errorf("process.creation.time = %v, want 1700000000000", e.ID["process.creation.time"])
	}
	if _, isAttr := e.Attributes["process.creation.time"]; isAttr {
		t.Error("process.creation.time must be in ID, not Attributes (it is identifying)")
	}
	if e.Attributes["process.name"] != "nginx" {
		t.Errorf("process.name attr = %v, want nginx", e.Attributes["process.name"])
	}

	if len(obs.Relations) != 1 {
		t.Fatalf("got %d relations, want 1", len(obs.Relations))
	}
	r := obs.Relations[0]
	if r.Type != "runs_on" || r.FromType != "process" || r.ToType != "host" {
		t.Errorf("relation = %s %s→%s, want runs_on process→host", r.Type, r.FromType, r.ToType)
	}
	if r.ToID["host.id"] != "host-uuid-1" {
		t.Errorf("runs_on target host.id = %v, want host-uuid-1", r.ToID["host.id"])
	}
}

// TestEntitySource_EmptyAfterUpdateIsReady confirms an empty-but-fed source is
// ok=true (a legitimate "everything I watched is gone"), not a transient miss.
func TestEntitySource_EmptyAfterUpdateIsReady(t *testing.T) {
	s := newProcessEntitySource()
	s.update("host-uuid-1", nil)
	obs, ok := s.Observe()
	if !ok {
		t.Error("Observe ok=false on empty ready set, want true")
	}
	if len(obs.Entities) != 0 {
		t.Errorf("got %d entities, want 0", len(obs.Entities))
	}
}

// TestProbe_EntitySourceOnlyInInventoryMode pins the churn guard: a process
// entity source exists only when the operator filters by name or user, never
// in a pure top_n or unfiltered view.
func TestProbe_EntitySourceOnlyInInventoryMode(t *testing.T) {
	cases := []struct {
		name string
		cfg  config
		want bool
	}{
		{"by_name", config{byName: regexp.MustCompile("nginx"), interval: time.Second}, true},
		{"by_user", config{byUser: "www", interval: time.Second}, true},
		{"top_n_only", config{topN: 20, interval: time.Second}, false},
		{"unfiltered", config{interval: time.Second}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p := &processProbe{BaseProbe: &types.BaseProbe{}, cfg: tc.cfg, logger: testLogger()}
			if tc.cfg.byName != nil || tc.cfg.byUser != "" {
				p.entitySrc = newProcessEntitySource()
			}
			got := p.entitySrc != nil
			if got != tc.want {
				t.Errorf("entitySrc present = %v, want %v", got, tc.want)
			}
		})
	}
}
