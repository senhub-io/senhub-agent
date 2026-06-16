package nats

import (
	"testing"

	"senhub-agent.go/internal/agent/services/agentstate"
)

// newTestEntitySource builds an entity source with an injectable host-id getter
// so tests are hermetic (no gopsutil call, no live target).
func newTestEntitySource(endpoint, instanceName, hostID string) *natsEntitySource {
	s := newNATSEntitySource(endpoint, instanceName)
	s.getHostID = func() string { return hostID }
	return s
}

// TestEntitySource_NotEmittedBeforePinned verifies that Observe returns ok=false
// before the id is pinned, so no entity escapes with an unknown identity.
func TestEntitySource_NotEmittedBeforePinned(t *testing.T) {
	s := newTestEntitySource("http://localhost:8222", "", "host-abc")
	obs, ok := s.Observe()
	if ok {
		t.Error("Observe must return ok=false before the id is pinned")
	}
	if len(obs.Entities) != 0 {
		t.Errorf("expected no entities before pinned, got %d", len(obs.Entities))
	}
}

// TestEntitySource_TechID_ServerName verifies that "server_name" is preferred
// over "server_id" when both are present.
func TestEntitySource_TechID_ServerName(t *testing.T) {
	s := newTestEntitySource("http://localhost:8222", "", "host-abc")
	s.pinFromVarz("my-nats-server", "NUID1234")

	obs, ok := s.Observe()
	if !ok {
		t.Fatal("Observe must return ok=true after pin via varz")
	}
	if len(obs.Entities) != 1 {
		t.Fatalf("expected 1 entity, got %d", len(obs.Entities))
	}
	gotID := obs.Entities[0].ID["service.instance.id"]
	if gotID != "nats:my-nats-server" {
		t.Errorf("id: want %q got %v", "nats:my-nats-server", gotID)
	}
}

// TestEntitySource_TechID_ServerID verifies that "server_id" is used when
// "server_name" is empty.
func TestEntitySource_TechID_ServerID(t *testing.T) {
	s := newTestEntitySource("http://localhost:8222", "", "host-abc")
	s.pinFromVarz("", "NUID5678")

	obs, ok := s.Observe()
	if !ok {
		t.Fatal("Observe must return ok=true after pin via varz")
	}
	gotID := obs.Entities[0].ID["service.instance.id"]
	if gotID != "nats:NUID5678" {
		t.Errorf("id: want %q got %v", "nats:NUID5678", gotID)
	}
}

// TestEntitySource_InstanceNameOverride verifies that "instance_name" from
// config is used verbatim and takes precedence over the tech id.
func TestEntitySource_InstanceNameOverride(t *testing.T) {
	s := newTestEntitySource("http://localhost:8222", "my-override", "host-abc")
	// Even if varz returns a real server identity, instance_name wins.
	s.pinFromVarz("actual-server-name", "NUID9999")

	obs, ok := s.Observe()
	if !ok {
		t.Fatal("Observe must return ok=true after pin")
	}
	gotID := obs.Entities[0].ID["service.instance.id"]
	if gotID != "my-override" {
		t.Errorf("id: want %q got %v", "my-override", gotID)
	}
}

// TestEntitySource_FallbackWithHostID verifies that the precedence-2 fallback
// "nats@<host.id>" is used when pinFallback is called with a known host.
func TestEntitySource_FallbackWithHostID(t *testing.T) {
	s := newTestEntitySource("http://localhost:8222", "", "host-deadbeef")
	s.pinFallback()

	obs, ok := s.Observe()
	if !ok {
		t.Fatal("Observe must return ok=true after pinFallback")
	}
	gotID := obs.Entities[0].ID["service.instance.id"]
	if gotID != "nats@host-deadbeef" {
		t.Errorf("id: want %q got %v", "nats@host-deadbeef", gotID)
	}
}

// TestEntitySource_FallbackNoHostID verifies that the last-resort "nats" id is
// used when host identity is also unavailable.
func TestEntitySource_FallbackNoHostID(t *testing.T) {
	s := newTestEntitySource("http://localhost:8222", "", "")
	s.pinFallback()

	obs, ok := s.Observe()
	if !ok {
		t.Fatal("Observe must return ok=true after pinFallback")
	}
	gotID := obs.Entities[0].ID["service.instance.id"]
	if gotID != "nats" {
		t.Errorf("id: want %q got %v", "nats", gotID)
	}
}

// TestEntitySource_IDImmutable verifies that subsequent pinFromVarz calls do
// not change the already-pinned id.
func TestEntitySource_IDImmutable(t *testing.T) {
	s := newTestEntitySource("http://localhost:8222", "", "host-abc")
	s.pinFromVarz("first-name", "NUID-A")
	s.pinFromVarz("second-name", "NUID-B") // must be ignored

	obs, _ := s.Observe()
	gotID := obs.Entities[0].ID["service.instance.id"]
	if gotID != "nats:first-name" {
		t.Errorf("id must be immutable after first pin; got %v", gotID)
	}
}

// TestEntitySource_MonitorsEdge_AgentIDSet verifies that a "monitors" relation
// is appended when the agent instance id is set.
func TestEntitySource_MonitorsEdge_AgentIDSet(t *testing.T) {
	agentstate.SetAgentInstanceID("agent-instance-001")
	t.Cleanup(func() { agentstate.SetAgentInstanceID("") })

	s := newTestEntitySource("http://localhost:8222", "", "host-abc")
	s.pinFromVarz("my-nats", "NUID-X")

	obs, ok := s.Observe()
	if !ok {
		t.Fatal("Observe must return ok=true after pin")
	}
	if len(obs.Relations) != 1 {
		t.Fatalf("expected 1 relation, got %d", len(obs.Relations))
	}
	rel := obs.Relations[0]
	if rel.Type != "monitors" {
		t.Errorf("relation type: want %q got %q", "monitors", rel.Type)
	}
	if rel.FromType != "service.instance" {
		t.Errorf("from type: want %q got %q", "service.instance", rel.FromType)
	}
	if rel.FromID["service.instance.id"] != "agent-instance-001" {
		t.Errorf("from id: want %q got %v", "agent-instance-001", rel.FromID["service.instance.id"])
	}
	if rel.ToType != "service.instance" {
		t.Errorf("to type: want %q got %q", "service.instance", rel.ToType)
	}
	if rel.ToID["service.instance.id"] != "nats:my-nats" {
		t.Errorf("to id: want %q got %v", "nats:my-nats", rel.ToID["service.instance.id"])
	}
}

// TestEntitySource_MonitorsEdge_AgentIDEmpty verifies that no "monitors"
// relation is appended when the agent instance id is not yet resolved.
func TestEntitySource_MonitorsEdge_AgentIDEmpty(t *testing.T) {
	agentstate.SetAgentInstanceID("")
	t.Cleanup(func() { agentstate.SetAgentInstanceID("") })

	s := newTestEntitySource("http://localhost:8222", "", "host-abc")
	s.pinFromVarz("my-nats", "NUID-X")

	obs, ok := s.Observe()
	if !ok {
		t.Fatal("Observe must return ok=true after pin")
	}
	if len(obs.Relations) != 0 {
		t.Errorf("expected no relations when agent id is empty, got %d", len(obs.Relations))
	}
}

// TestEntitySource_ServiceNameAttr verifies that "service.name":"nats" is
// carried as a descriptive attribute on the emitted entity.
func TestEntitySource_ServiceNameAttr(t *testing.T) {
	s := newTestEntitySource("http://localhost:8222", "", "host-abc")
	s.pinFromVarz("my-nats", "NUID-X")

	obs, _ := s.Observe()
	if len(obs.Entities) == 0 {
		t.Fatal("no entities emitted")
	}
	if obs.Entities[0].Attributes["service.name"] != "nats" {
		t.Errorf("service.name attr: want %q got %v", "nats", obs.Entities[0].Attributes["service.name"])
	}
}
