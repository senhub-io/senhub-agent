package rabbitmq

import (
	"testing"

	"senhub-agent.go/internal/agent/services/agentstate"
	"senhub-agent.go/internal/agent/services/entity"
)

// ── helpers ──────────────────────────────────────────────────────────────────

// noHostID is a stub that returns "" (simulates an unresolvable host id).
func noHostID() string { return "" }

// fixedHostID returns a deterministic host id for tests.
func fixedHostID(id string) func() string { return func() string { return id } }

// newTestEntitySource builds an entity source with injected dependencies.
func newTestEntitySource(instanceName, addr string, port int64, hostIDFn func() string) *rabbitmqEntitySource {
	return newRabbitmqEntitySource(instanceName, addr, port, hostIDFn)
}

// ── tests ─────────────────────────────────────────────────────────────────────

// TestEntitySource_InstanceNameOverride verifies that when "instance_name" is
// configured the id is pinned immediately at construction and the entity is
// emitted on the first reachable Observe() without any tech-id fetch.
func TestEntitySource_InstanceNameOverride(t *testing.T) {
	s := newTestEntitySource("my-rabbit-prod", "rabbit.example.com", 15672, noHostID)

	if got := s.pinnedInstanceID(); got != "my-rabbit-prod" {
		t.Fatalf("pinnedInstanceID() = %q, want %q", got, "my-rabbit-prod")
	}

	s.setReachable(true, "")
	obs, ok := s.Observe()
	if !ok {
		t.Fatal("Observe() ok=false after instance_name pinned and reachable")
	}
	if len(obs.Entities) != 1 {
		t.Fatalf("Observe() entities=%d, want 1", len(obs.Entities))
	}
	if got := obs.Entities[0].ID["service.instance.id"]; got != "my-rabbit-prod" {
		t.Errorf("service.instance.id = %v, want %q", got, "my-rabbit-prod")
	}
}

// TestEntitySource_TechIDPinnedOnFirstCollect verifies the tech id is picked
// up from the node name on the first successful collect and formatted correctly.
func TestEntitySource_TechIDPinnedOnFirstCollect(t *testing.T) {
	s := newTestEntitySource("", "localhost", 15672, noHostID)

	// Before any collect: id not pinned → ok=false.
	s.setReachable(true, "")
	if _, ok := s.Observe(); ok {
		t.Error("Observe() should return ok=false before tech id is pinned")
	}

	// Simulate a successful collect delivering the node name.
	s.tryPinTechID("rabbit@myhost")

	if got := s.pinnedInstanceID(); got != "rabbitmq:rabbit@myhost" {
		t.Fatalf("pinnedInstanceID() = %q, want %q", got, "rabbitmq:rabbit@myhost")
	}

	obs, ok := s.Observe()
	if !ok {
		t.Fatal("Observe() ok=false after tech id pinned and reachable")
	}
	if got := obs.Entities[0].ID["service.instance.id"]; got != "rabbitmq:rabbit@myhost" {
		t.Errorf("service.instance.id = %v, want %q", got, "rabbitmq:rabbit@myhost")
	}
}

// TestEntitySource_NotEmittedBeforePinned verifies that no entity is emitted
// while the id has not been pinned yet, even when the broker is reachable.
func TestEntitySource_NotEmittedBeforePinned(t *testing.T) {
	s := newTestEntitySource("", "host", 15672, noHostID)
	s.setReachable(true, "")

	obs, ok := s.Observe()
	if ok {
		t.Error("Observe() ok=true before id pinned — must stay false")
	}
	if len(obs.Entities) != 0 {
		t.Errorf("Observe() returned entities before id pinned: %+v", obs.Entities)
	}
}

// TestEntitySource_TechIDImmutable verifies that once the tech id is pinned a
// second tryPinTechID call with a different value does not change it.
func TestEntitySource_TechIDImmutable(t *testing.T) {
	s := newTestEntitySource("", "localhost", 15672, noHostID)
	s.tryPinTechID("rabbit@first")
	s.tryPinTechID("rabbit@second") // must be ignored

	if got := s.pinnedInstanceID(); got != "rabbitmq:rabbit@first" {
		t.Errorf("pinnedInstanceID() = %q, want %q (id must not change once pinned)", got, "rabbitmq:rabbit@first")
	}
}

// TestEntitySource_FallbackWithHostID verifies the host-id fallback path.
// pinFallback() must format the id as "rabbitmq@<hostID>" when the host id
// function returns a non-empty value.
func TestEntitySource_FallbackWithHostID(t *testing.T) {
	s := newTestEntitySource("", "host", 15672, fixedHostID("abc-machine-uuid"))
	s.pinFallback()

	if got := s.pinnedInstanceID(); got != "rabbitmq@abc-machine-uuid" {
		t.Errorf("pinnedInstanceID() = %q, want %q", got, "rabbitmq@abc-machine-uuid")
	}

	s.setReachable(true, "")
	obs, ok := s.Observe()
	if !ok {
		t.Fatal("Observe() ok=false after fallback pinned and reachable")
	}
	if got := obs.Entities[0].ID["service.instance.id"]; got != "rabbitmq@abc-machine-uuid" {
		t.Errorf("service.instance.id = %v", got)
	}
}

// TestEntitySource_FallbackLastResort verifies that when the host id is also
// empty the fallback degrades to the bare "rabbitmq" string.
func TestEntitySource_FallbackLastResort(t *testing.T) {
	s := newTestEntitySource("", "host", 15672, noHostID)
	s.pinFallback()

	if got := s.pinnedInstanceID(); got != "rabbitmq" {
		t.Errorf("pinnedInstanceID() = %q, want %q", got, "rabbitmq")
	}
}

// TestEntitySource_FallbackNotOverwritesPinned verifies that pinFallback is a
// no-op when the tech id has already been pinned (immutability).
func TestEntitySource_FallbackNotOverwritesPinned(t *testing.T) {
	s := newTestEntitySource("", "host", 15672, fixedHostID("some-host"))
	s.tryPinTechID("rabbit@node")
	s.pinFallback() // must be ignored

	if got := s.pinnedInstanceID(); got != "rabbitmq:rabbit@node" {
		t.Errorf("pinnedInstanceID() = %q, want %q", got, "rabbitmq:rabbit@node")
	}
}

// TestEntitySource_MonitorsEdgePresent verifies that when the agent instance id
// is set the Observe() result includes a "monitors" relation from the agent to
// the target service.instance.
func TestEntitySource_MonitorsEdgePresent(t *testing.T) {
	agentstate.SetAgentInstanceID("agent-node-id-123")
	t.Cleanup(func() { agentstate.SetAgentInstanceID("") })

	s := newTestEntitySource("", "host", 15672, noHostID)
	s.tryPinTechID("rabbit@prod")
	s.setReachable(true, "")

	obs, ok := s.Observe()
	if !ok {
		t.Fatal("Observe() ok=false")
	}

	if len(obs.Relations) != 1 {
		t.Fatalf("Observe() relations=%d, want 1", len(obs.Relations))
	}
	r := obs.Relations[0]
	if r.Type != "monitors" {
		t.Errorf("relation type = %q, want %q", r.Type, "monitors")
	}
	if r.FromType != "service.instance" || r.ToType != "service.instance" {
		t.Errorf("from/to type = %q/%q, want service.instance/service.instance", r.FromType, r.ToType)
	}
	if got := r.FromID["service.instance.id"]; got != "agent-node-id-123" {
		t.Errorf("from id = %v, want %q", got, "agent-node-id-123")
	}
	if got := r.ToID["service.instance.id"]; got != "rabbitmq:rabbit@prod" {
		t.Errorf("to id = %v, want %q", got, "rabbitmq:rabbit@prod")
	}
}

// TestEntitySource_MonitorsEdgeAbsent verifies that no "monitors" relation is
// emitted when the agent instance id is not set (empty string).
func TestEntitySource_MonitorsEdgeAbsent(t *testing.T) {
	agentstate.SetAgentInstanceID("")

	s := newTestEntitySource("", "host", 15672, noHostID)
	s.tryPinTechID("rabbit@prod")
	s.setReachable(true, "")

	obs, ok := s.Observe()
	if !ok {
		t.Fatal("Observe() ok=false")
	}
	if len(obs.Relations) != 0 {
		t.Errorf("Observe() relations=%d, want 0 when agent id empty", len(obs.Relations))
	}
}

// TestEntitySource_DescriptiveAttrs verifies that server.address, server.port,
// and service.name appear as entity attributes (not in the ID map).
func TestEntitySource_DescriptiveAttrs(t *testing.T) {
	s := newTestEntitySource("", "rabbit.example.com", 15672, noHostID)
	s.tryPinTechID("rabbit@example")
	s.setReachable(true, "")

	obs, ok := s.Observe()
	if !ok {
		t.Fatal("Observe() ok=false")
	}
	attrs := obs.Entities[0].Attributes
	if attrs["service.name"] != "rabbitmq" {
		t.Errorf("service.name = %v", attrs["service.name"])
	}
	if attrs["server.address"] != "rabbit.example.com" {
		t.Errorf("server.address = %v", attrs["server.address"])
	}
	if attrs["server.port"] != int64(15672) {
		t.Errorf("server.port = %v", attrs["server.port"])
	}
	// Network-derived values must NOT be in the ID.
	id := obs.Entities[0].ID
	if _, ok := id["server.address"]; ok {
		t.Error("server.address must not appear in the entity ID")
	}
	if _, ok := id["server.port"]; ok {
		t.Error("server.port must not appear in the entity ID")
	}
}

// TestEntitySource_UnreachableOkFalse verifies that Observe() returns ok=false
// when the broker is unreachable, even if the id is already pinned, so the
// detector keeps the last good snapshot rather than treating it as a delete.
func TestEntitySource_UnreachableOkFalse(t *testing.T) {
	s := newTestEntitySource("", "host", 15672, noHostID)
	s.tryPinTechID("rabbit@node")
	s.setReachable(false, "")

	if _, ok := s.Observe(); ok {
		t.Error("Observe() ok=true when broker unreachable — should return false")
	}
}

// TestEntitySource_EntityType verifies the emitted entity type is "service.instance".
func TestEntitySource_EntityType(t *testing.T) {
	s := newTestEntitySource("", "host", 15672, noHostID)
	s.tryPinTechID("rabbit@node")
	s.setReachable(true, "")

	obs, _ := s.Observe()
	if got := obs.Entities[0].Type; got != "service.instance" {
		t.Errorf("entity type = %q, want %q", got, "service.instance")
	}
}

// TestEntitySource_InstanceNameTechIDIgnored verifies that when instance_name
// is set, a subsequent tryPinTechID call is silently ignored (the operator
// override wins permanently).
func TestEntitySource_InstanceNameTechIDIgnored(t *testing.T) {
	s := newTestEntitySource("operator-override", "host", 15672, noHostID)
	s.tryPinTechID("rabbit@othername") // must be ignored

	if got := s.pinnedInstanceID(); got != "operator-override" {
		t.Errorf("pinnedInstanceID() = %q, want %q", got, "operator-override")
	}
}

// TestEntitySource_ObservationRelationShape verifies the Observation returned
// by Observe satisfies the entity.Observation type (compile-time + runtime
// shape check).
func TestEntitySource_ObservationRelationShape(t *testing.T) {
	agentstate.SetAgentInstanceID("ag-id")
	t.Cleanup(func() { agentstate.SetAgentInstanceID("") })

	s := newTestEntitySource("", "host", 15672, noHostID)
	s.tryPinTechID("rabbit@node")
	s.setReachable(true, "")

	obs, ok := s.Observe()
	if !ok {
		t.Fatal("ok=false")
	}

	// Verify the Observation implements the entity.Source interface contract.
	var _ entity.Observation = obs

	// One entity + one relation.
	if len(obs.Entities) != 1 || len(obs.Relations) != 1 {
		t.Errorf("want 1 entity + 1 relation, got %d + %d", len(obs.Entities), len(obs.Relations))
	}
}
