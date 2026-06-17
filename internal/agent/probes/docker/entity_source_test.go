package docker

import (
	"testing"

	"senhub-agent.go/internal/agent/services/entity"
)

func runsOnCount(obs entity.Observation) int {
	n := 0
	for _, r := range obs.Relations {
		if r.Type == relRunsOn {
			n++
		}
	}
	return n
}

func TestDockerEntitySource_RunsOnAnchorsContainerToHost(t *testing.T) {
	s := &dockerEntitySource{hostID: func() string { return "host-9" }}
	s.update([]containerListItem{
		{ID: "abc123", Names: []string{"/web"}, Image: "nginx:1.27"},
		{ID: "def456", Names: []string{"/db"}, Image: "postgres:16"},
	})

	obs, ok := s.Observe()
	if !ok {
		t.Fatal("Observe() ok=false after update")
	}
	if len(obs.Entities) != 2 {
		t.Fatalf("want 2 container entities, got %d", len(obs.Entities))
	}
	if got := runsOnCount(obs); got != 2 {
		t.Fatalf("want 2 runs_on edges (one per container), got %d: %+v", got, obs.Relations)
	}
	for _, r := range obs.Relations {
		if r.FromType != entityTypeContainer {
			t.Errorf("runs_on From must be a container: %+v", r)
		}
		if r.ToType != entityTypeHost || r.ToID[idKeyHost] != "host-9" {
			t.Errorf("runs_on must target the host host-9: %+v", r)
		}
		// the edge FromID must match the emitted container identity exactly
		if _, ok := r.FromID[idKeyContainerID]; !ok {
			t.Errorf("runs_on FromID must carry container.id: %+v", r.FromID)
		}
	}
}

func TestDockerEntitySource_NoHostIDSkipsRunsOn(t *testing.T) {
	// host.id unavailable → emit the container node but no unresolvable edge.
	s := &dockerEntitySource{hostID: func() string { return "" }}
	s.update([]containerListItem{{ID: "abc123", Names: []string{"/web"}, Image: "nginx"}})

	obs, _ := s.Observe()
	if len(obs.Entities) != 1 {
		t.Fatalf("want 1 container entity, got %d", len(obs.Entities))
	}
	if got := runsOnCount(obs); got != 0 {
		t.Errorf("no runs_on expected when host.id is unavailable, got %d", got)
	}
}

func TestDockerEntitySource_NotReadyBeforeFirstUpdate(t *testing.T) {
	s := &dockerEntitySource{hostID: func() string { return "h" }}
	if _, ok := s.Observe(); ok {
		t.Error("Observe() must be ok=false before the first update")
	}
}
