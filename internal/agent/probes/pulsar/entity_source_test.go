package pulsar

import (
	"errors"
	"testing"

	"senhub-agent.go/internal/agent/services/agentstate"
	"senhub-agent.go/internal/agent/services/entity"
)

// makeEntitySource builds a pulsarEntitySource with injected dependencies so
// no live network or real machine-id is needed.
func makeEntitySource(t *testing.T, instanceName string, clusters []string, clusterErr error, hostIDVal string) *pulsarEntitySource {
	t.Helper()
	s := &pulsarEntitySource{
		host: "localhost",
		port: 8080,
		fetchClusters: func() ([]string, error) {
			return clusters, clusterErr
		},
		hostID: func() string { return hostIDVal },
	}
	if instanceName != "" {
		s.id = instanceName
		s.pinned = true
	}
	return s
}

// entityID returns the service.instance.id from the observation or "".
func entityID(obs entity.Observation) string {
	if len(obs.Entities) == 0 {
		return ""
	}
	v, _ := obs.Entities[0].ID["service.instance.id"].(string)
	return v
}

// relationType returns the type of the first relation or "".
func relationType(obs entity.Observation) string {
	if len(obs.Relations) == 0 {
		return ""
	}
	return obs.Relations[0].Type
}

func TestEntitySource_InstanceNameOverride_PinnedImmediately(t *testing.T) {
	s := makeEntitySource(t, "my-pulsar-prod", nil, nil, "pulsar@host-id")

	// With instanceName set the id is pinned at construction.
	if !s.pinned || s.id != "my-pulsar-prod" {
		t.Fatalf("expected pinned id %q, got pinned=%v id=%q", "my-pulsar-prod", s.pinned, s.id)
	}

	// Observe must return the entity even before setReachable has been called,
	// as long as up is set to true (the entity source starts with up=false, so
	// we call setReachable first — cluster fetch is never attempted when already
	// pinned).
	s.setReachable(true)
	obs, ok := s.Observe()
	if !ok {
		t.Fatal("Observe returned ok=false, want true")
	}
	if got := entityID(obs); got != "my-pulsar-prod" {
		t.Errorf("entity id = %q, want %q", got, "my-pulsar-prod")
	}
}

func TestEntitySource_InstanceNameOverride_NeverFetchesClusters(t *testing.T) {
	fetched := false
	s := &pulsarEntitySource{
		host: "localhost",
		port: 8080,
		fetchClusters: func() ([]string, error) {
			fetched = true
			return []string{"standalone"}, nil
		},
		hostID: func() string { return "pulsar@host-id" },
		id:     "operator-name",
		pinned: true,
	}

	s.setReachable(true)
	if fetched {
		t.Error("fetchClusters was called despite instanceName being set")
	}
}

func TestEntitySource_TechID_PinnedOnFirstSuccessfulCollect(t *testing.T) {
	s := makeEntitySource(t, "", []string{"pulsar-cluster"}, nil, "pulsar@host-id")

	// Before any setReachable call: not pinned, Observe must return false.
	if s.pinned {
		t.Fatal("source must not be pinned before first setReachable call")
	}
	if _, ok := s.Observe(); ok {
		t.Fatal("Observe must return ok=false before id is pinned")
	}

	// First successful reachable cycle: cluster fetch succeeds → pin tech id.
	s.setReachable(true)
	if !s.pinned {
		t.Fatal("source must be pinned after first successful setReachable(true)")
	}
	if s.id != "pulsar:pulsar-cluster" {
		t.Errorf("pinned id = %q, want %q", s.id, "pulsar:pulsar-cluster")
	}
}

func TestEntitySource_EntityNotEmittedBeforeIDIsPinned(t *testing.T) {
	// Cluster fetch always fails → id never gets pinned through the tech path.
	// After up goes false the fallback path pins.
	s := makeEntitySource(t, "", nil, errors.New("transient"), "pulsar@host-id")

	// First cycle: broker up but cluster fetch fails → not yet pinned.
	s.setReachable(true)
	if s.pinned {
		t.Fatal("source must not be pinned when cluster fetch fails transiently")
	}
	if _, ok := s.Observe(); ok {
		t.Error("Observe must return ok=false when id is not pinned")
	}
}

func TestEntitySource_TechID_IDNeverChangesAfterPin(t *testing.T) {
	callCount := 0
	s := &pulsarEntitySource{
		host: "localhost",
		port: 8080,
		fetchClusters: func() ([]string, error) {
			callCount++
			if callCount == 1 {
				return []string{"first-cluster"}, nil
			}
			return []string{"second-cluster"}, nil
		},
		hostID: func() string { return "pulsar@host-id" },
	}

	// Pin on first call.
	s.setReachable(true)
	if s.id != "pulsar:first-cluster" {
		t.Fatalf("id after first pin = %q", s.id)
	}

	// Subsequent cycles must not change the id.
	s.setReachable(true)
	s.setReachable(true)
	if s.id != "pulsar:first-cluster" {
		t.Errorf("id changed after re-pin: %q", s.id)
	}
	if callCount != 1 {
		t.Errorf("fetchClusters called %d times, want 1 (pinned after first success)", callCount)
	}
}

func TestEntitySource_FallbackPath_WhenBrokerUnreachable(t *testing.T) {
	s := makeEntitySource(t, "", nil, nil, "pulsar@abc123")

	// Broker never reachable → pin fallback on first down cycle.
	s.setReachable(false)
	if !s.pinned {
		t.Fatal("fallback must be pinned when broker is unreachable")
	}
	if s.id != "pulsar@abc123" {
		t.Errorf("fallback id = %q, want %q", s.id, "pulsar@abc123")
	}
	// Observe still returns false because up=false.
	if _, ok := s.Observe(); ok {
		t.Error("Observe must return ok=false when broker is down (even with fallback id)")
	}
}

func TestEntitySource_FallbackPath_EmptyHostID(t *testing.T) {
	s := makeEntitySource(t, "", nil, nil, "pulsar") // stub returns bare "pulsar"
	s.setReachable(false)
	if s.id != "pulsar" {
		t.Errorf("last-resort id = %q, want %q", s.id, "pulsar")
	}
}

func TestEntitySource_MonitorsEdge_PresentWhenAgentIDSet(t *testing.T) {
	agentstate.SetAgentInstanceID("agent-123")
	t.Cleanup(func() { agentstate.SetAgentInstanceID("") })

	s := makeEntitySource(t, "", []string{"my-cluster"}, nil, "pulsar@host-id")
	s.setReachable(true)

	obs, ok := s.Observe()
	if !ok {
		t.Fatal("Observe returned ok=false")
	}
	if len(obs.Relations) != 1 {
		t.Fatalf("len(Relations) = %d, want 1", len(obs.Relations))
	}
	rel := obs.Relations[0]
	if rel.Type != "monitors" {
		t.Errorf("relation type = %q, want monitors", rel.Type)
	}
	if rel.FromType != "service.instance" {
		t.Errorf("FromType = %q, want service.instance", rel.FromType)
	}
	fromID, _ := rel.FromID["service.instance.id"].(string)
	if fromID != "agent-123" {
		t.Errorf("FromID = %q, want agent-123", fromID)
	}
	if rel.ToType != "service.instance" {
		t.Errorf("ToType = %q, want service.instance", rel.ToType)
	}
	toID, _ := rel.ToID["service.instance.id"].(string)
	if toID != "pulsar:my-cluster" {
		t.Errorf("ToID = %q, want pulsar:my-cluster", toID)
	}
}

func TestEntitySource_MonitorsEdge_AbsentWhenAgentIDEmpty(t *testing.T) {
	agentstate.SetAgentInstanceID("")

	s := makeEntitySource(t, "", []string{"my-cluster"}, nil, "pulsar@host-id")
	s.setReachable(true)

	obs, ok := s.Observe()
	if !ok {
		t.Fatal("Observe returned ok=false")
	}
	if len(obs.Relations) != 0 {
		t.Errorf("Relations must be empty when agent id is unset, got %d", len(obs.Relations))
	}
}

func TestEntitySource_DescriptiveAttributes(t *testing.T) {
	s := makeEntitySource(t, "", []string{"cluster1"}, nil, "pulsar@host-id")
	s.setReachable(true)

	obs, ok := s.Observe()
	if !ok {
		t.Fatal("Observe returned ok=false")
	}
	if len(obs.Entities) != 1 {
		t.Fatalf("len(Entities) = %d, want 1", len(obs.Entities))
	}
	e := obs.Entities[0]
	if e.Type != "service.instance" {
		t.Errorf("Type = %q, want service.instance", e.Type)
	}
	if v, _ := e.Attributes["service.name"].(string); v != "pulsar" {
		t.Errorf("service.name = %q, want pulsar", v)
	}
	if v, _ := e.Attributes["server.address"].(string); v != "localhost" {
		t.Errorf("server.address = %q, want localhost", v)
	}
	if v, _ := e.Attributes["server.port"].(int64); v != 8080 {
		t.Errorf("server.port = %v, want 8080", v)
	}
}
