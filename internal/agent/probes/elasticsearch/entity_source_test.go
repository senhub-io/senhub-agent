package elasticsearch

import (
	"testing"

	"senhub-agent.go/internal/agent/services/agentstate"
	"senhub-agent.go/internal/agent/services/entity"
)

// TestEntitySource_InstanceNameOverride verifies that when instance_name is
// configured the entity is emitted immediately with that verbatim id, without
// waiting for a cluster_uuid fetch.
func TestEntitySource_InstanceNameOverride(t *testing.T) {
	s := newElasticsearchEntitySource("my-prod-es", "es.example.com", 9200)
	s.setReachable(true)

	obs, ok := s.Observe()
	if !ok {
		t.Fatal("Observe() ok=false, want true when instance_name is set")
	}
	if len(obs.Entities) != 1 {
		t.Fatalf("Entities len=%d, want 1", len(obs.Entities))
	}
	got, _ := obs.Entities[0].ID["db.instance.id"].(string)
	if got != "my-prod-es" {
		t.Errorf("db.instance.id = %q, want %q", got, "my-prod-es")
	}
	if obs.Entities[0].Type != "db" {
		t.Errorf("entity type = %q, want \"db\"", obs.Entities[0].Type)
	}
}

// TestEntitySource_TechIDPinned verifies that after pinClusterUUID the entity
// is emitted with id "elasticsearch:<uuid>".
func TestEntitySource_TechIDPinned(t *testing.T) {
	s := newElasticsearchEntitySource("", "localhost", 9200)
	s.setReachable(true)

	// Before pin: entity must be suppressed.
	if _, ok := s.Observe(); ok {
		t.Fatal("Observe() ok=true before cluster_uuid is pinned, want false")
	}

	s.pinClusterUUID("abc-123-uuid")
	obs, ok := s.Observe()
	if !ok {
		t.Fatal("Observe() ok=false after cluster_uuid pinned, want true")
	}
	got, _ := obs.Entities[0].ID["db.instance.id"].(string)
	if got != "elasticsearch:abc-123-uuid" {
		t.Errorf("db.instance.id = %q, want %q", got, "elasticsearch:abc-123-uuid")
	}
}

// TestEntitySource_NotEmittedBeforePin verifies that ok=false is returned even
// when reachable=true while no cluster_uuid has been pinned and no instance_name
// was supplied.
func TestEntitySource_NotEmittedBeforePin(t *testing.T) {
	s := newElasticsearchEntitySource("", "es.host", 9200)
	s.setReachable(true)

	_, ok := s.Observe()
	if ok {
		t.Error("Observe() ok=true before pin, want false — entity must be suppressed until id is stable")
	}
}

// TestEntitySource_PinIsImmutable verifies that a second call to pinClusterUUID
// does not overwrite the first.
func TestEntitySource_PinIsImmutable(t *testing.T) {
	s := newElasticsearchEntitySource("", "localhost", 9200)
	s.setReachable(true)
	s.pinClusterUUID("first-uuid")
	s.pinClusterUUID("second-uuid") // must be a no-op

	obs, _ := s.Observe()
	got, _ := obs.Entities[0].ID["db.instance.id"].(string)
	if got != "elasticsearch:first-uuid" {
		t.Errorf("db.instance.id = %q, want %q (second pin must not overwrite first)", got, "elasticsearch:first-uuid")
	}
}

// TestEntitySource_DescriptiveAttrs verifies server.address, server.port, and
// db.system.name are present in entity attributes.
func TestEntitySource_DescriptiveAttrs(t *testing.T) {
	s := newElasticsearchEntitySource("", "es.internal", 9201)
	s.setReachable(true)
	s.pinClusterUUID("uuid-descriptive")

	obs, _ := s.Observe()
	attrs := obs.Entities[0].Attributes
	if v, _ := attrs["db.system.name"].(string); v != "elasticsearch" {
		t.Errorf("db.system.name = %q, want \"elasticsearch\"", v)
	}
	if v, _ := attrs["server.address"].(string); v != "es.internal" {
		t.Errorf("server.address = %q, want \"es.internal\"", v)
	}
	if v, _ := attrs["server.port"].(int64); v != 9201 {
		t.Errorf("server.port = %v, want 9201", v)
	}
}

// TestEntitySource_MonitorsEdge_Present verifies the monitors relation is
// appended when the agent instance id is set.
func TestEntitySource_MonitorsEdge_Present(t *testing.T) {
	agentstate.SetAgentInstanceID("agent-xyz")
	t.Cleanup(func() { agentstate.SetAgentInstanceID("") })

	s := newElasticsearchEntitySource("", "localhost", 9200)
	s.setReachable(true)
	s.pinClusterUUID("cluster-001")

	obs, ok := s.Observe()
	if !ok {
		t.Fatal("Observe() ok=false, want true")
	}
	r, found := findRelation(obs, "monitors")
	if !found {
		t.Fatalf("monitors edge missing; got %d relations", len(obs.Relations))
	}
	if r.Type != "monitors" {
		t.Errorf("relation type = %q, want \"monitors\"", r.Type)
	}
	if r.FromType != "service.instance" {
		t.Errorf("FromType = %q, want \"service.instance\"", r.FromType)
	}
	fromID, _ := r.FromID["service.instance.id"].(string)
	if fromID != "agent-xyz" {
		t.Errorf("FromID service.instance.id = %q, want \"agent-xyz\"", fromID)
	}
	if r.ToType != "db" {
		t.Errorf("ToType = %q, want \"db\"", r.ToType)
	}
	toID, _ := r.ToID["db.instance.id"].(string)
	if toID != "elasticsearch:cluster-001" {
		t.Errorf("ToID db.instance.id = %q, want \"elasticsearch:cluster-001\"", toID)
	}
}

// TestEntitySource_MonitorsEdge_Absent verifies the monitors relation is NOT
// appended when agentstate returns an empty id.
func TestEntitySource_MonitorsEdge_Absent(t *testing.T) {
	agentstate.SetAgentInstanceID("")

	s := newElasticsearchEntitySource("", "localhost", 9200)
	s.setReachable(true)
	s.pinClusterUUID("cluster-002")

	obs, ok := s.Observe()
	if !ok {
		t.Fatal("Observe() ok=false, want true")
	}
	if _, has := findRelation(obs, "monitors"); has {
		t.Error("monitors edge present when agent id is empty; want absent")
	}
}

// findRelation returns the first relation of the given type.
func findRelation(obs entity.Observation, relType string) (entity.Relation, bool) {
	for _, r := range obs.Relations {
		if r.Type == relType {
			return r, true
		}
	}
	return entity.Relation{}, false
}

// TestEntitySource_LocalDBRunsOnHost: a loopback-reachable ES with a globally-
// unique tech id is anchored to the host with runs_on (enterprise#36); a remote
// db is not.
func TestEntitySource_LocalDBRunsOnHost(t *testing.T) {
	agentstate.SetAgentInstanceID("")
	runsOn := func(host string) bool {
		s := newElasticsearchEntitySource("", host, 9200)
		s.hostID = func() string { return "h-1" }
		s.pinClusterUUID("cluster-001")
		s.setReachable(true)
		obs, _ := s.Observe()
		for _, r := range obs.Relations {
			if r.Type == "runs_on" && r.FromType == "db" && r.ToID["host.id"] == "h-1" {
				return true
			}
		}
		return false
	}
	if !runsOn("127.0.0.1") {
		t.Error("loopback db with a tech id must emit runs_on→host")
	}
	if runsOn("10.0.0.5") {
		t.Error("remote db must NOT emit runs_on→host")
	}
}

// TestEntitySource_Unreachable verifies ok=false when the instance is
// unreachable even though the id is pinned.
func TestEntitySource_Unreachable(t *testing.T) {
	s := newElasticsearchEntitySource("", "localhost", 9200)
	s.pinClusterUUID("some-uuid")
	s.setReachable(false)

	_, ok := s.Observe()
	if ok {
		t.Error("Observe() ok=true when unreachable, want false")
	}
}
