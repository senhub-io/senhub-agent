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

func TestContainerStatus_Mapping(t *testing.T) {
	cases := map[string]string{
		"running":    "running",
		"Running":    "running",
		"paused":     "paused",
		"restarting": "restarting",
		"exited":     "stopped",
		"created":    "stopped",
		"dead":       "stopped",
		"removing":   "stopped",
		"":           "",
	}
	for state, want := range cases {
		if got := containerStatus(state); got != want {
			t.Errorf("containerStatus(%q) = %q, want %q", state, got, want)
		}
	}
}

func TestDockerEntitySource_StatusAttribute(t *testing.T) {
	s := &dockerEntitySource{hostID: func() string { return "h" }}
	s.update([]containerListItem{
		{ID: "a", Names: []string{"/web"}, Image: "nginx", State: "running"},
		{ID: "b", Names: []string{"/job"}, Image: "busybox", State: "exited"},
		{ID: "c", Names: []string{"/x"}, Image: "img"}, // no State → status omitted
	})
	obs, _ := s.Observe()

	byID := map[string]map[string]any{}
	for _, e := range obs.Entities {
		byID[e.ID[idKeyContainerID].(string)] = e.Attributes
	}
	if byID["a"][attrContainerStatus] != "running" {
		t.Errorf("container a status = %v, want running", byID["a"][attrContainerStatus])
	}
	if byID["b"][attrContainerStatus] != "stopped" {
		t.Errorf("container b status = %v, want stopped", byID["b"][attrContainerStatus])
	}
	if _, has := byID["c"][attrContainerStatus]; has {
		t.Errorf("container c must omit status when State is empty, got %v", byID["c"][attrContainerStatus])
	}
}

func TestDockerEntitySource_NotReadyBeforeFirstUpdate(t *testing.T) {
	s := &dockerEntitySource{hostID: func() string { return "h" }}
	if _, ok := s.Observe(); ok {
		t.Error("Observe() must be ok=false before the first update")
	}
}

// A container joining a swarm overlay must carry the edge that places it on
// that segment — the attachment is the only place a segment is observable per
// workload (ADR 0034).
func TestUpdate_AttachesContainersToSwarmOverlays(t *testing.T) {
	s := &dockerEntitySource{hostID: func() string { return "h-1" }}
	c := containerListItem{ID: "abc123", Names: []string{"/web"}, Image: "nginx", State: "running"}
	c.NetworkSettings.Networks = map[string]struct {
		NetworkID string `json:"NetworkID"`
	}{
		"frontend": {NetworkID: "u2n5w4y1c3k7q9r0t8v6x2z4a"}, // 25 chars: swarm
		"bridge":   {NetworkID: "0123456789abcdef"},          // engine built-in
	}
	s.update([]containerListItem{c})

	obs, ok := s.Observe()
	if !ok {
		t.Fatal("Observe ok=false")
	}
	var attached []string
	for _, rel := range obs.Relations {
		if rel.Type == relAttachedTo {
			id, _ := rel.ToID["network.segment.id"].(string)
			attached = append(attached, id)
		}
	}
	if len(attached) != 1 {
		t.Fatalf("attached to %v, want exactly the swarm overlay", attached)
	}
	if attached[0] != "swarm:u2n5w4y1c3k7q9r0t8v6x2z4a" {
		t.Errorf("segment id = %q, want the subtype-prefixed swarm id", attached[0])
	}
}

// The engine's built-in networks must never produce an edge. A bridge is local
// to one machine and identically named on every host, so a segment node built
// from it would be shared by the whole fleet — the collapse family this project
// keeps paying for.
func TestUpdate_IgnoresLocalNetworks(t *testing.T) {
	s := &dockerEntitySource{hostID: func() string { return "h-1" }}
	c := containerListItem{ID: "abc123", Names: []string{"/web"}, State: "running"}
	c.NetworkSettings.Networks = map[string]struct {
		NetworkID string `json:"NetworkID"`
	}{
		"bridge": {NetworkID: "0123456789abcdef"},
		"host":   {NetworkID: "fedcba9876543210"},
	}
	s.update([]containerListItem{c})

	obs, _ := s.Observe()
	for _, rel := range obs.Relations {
		if rel.Type == relAttachedTo {
			t.Errorf("a local network produced an attachment edge: %+v", rel.ToID)
		}
	}
}
