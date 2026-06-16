package activemq

import (
	"testing"

	"senhub-agent.go/internal/agent/services/agentstate"
)

// constantHostID returns a fixed host id, used to inject a deterministic
// machine id into the entity source without calling gopsutil.
func constantHostID(id string) func() string {
	return func() string { return id }
}

// TestEntitySource_InstanceNameOverride verifies that an operator-supplied
// instance_name is used verbatim as the entity id and that the id is pinned
// at construction (Observe returns ok=true as soon as the broker is up).
func TestEntitySource_InstanceNameOverride(t *testing.T) {
	src := newActivemqEntitySource("my-broker", "localhost", 8161, constantHostID("host-abc"))

	// Before setReachable the entity must not be emitted (broker is down).
	if _, ok := src.Observe(); ok {
		t.Fatal("Observe() returned ok=true before the broker is reachable")
	}

	src.setReachable(true, "")

	obs, ok := src.Observe()
	if !ok {
		t.Fatal("Observe() returned ok=false after setReachable(true)")
	}
	if len(obs.Entities) != 1 {
		t.Fatalf("expected 1 entity, got %d", len(obs.Entities))
	}
	e := obs.Entities[0]
	if got := e.ID["service.instance.id"]; got != "my-broker" {
		t.Errorf("service.instance.id = %q, want %q", got, "my-broker")
	}
	if e.Type != "service.instance" {
		t.Errorf("entity type = %q, want service.instance", e.Type)
	}
}

// TestEntitySource_TechIDPinned verifies that:
//   - before pinTechID is called, Observe returns ok=false (entity not emitted);
//   - after pinTechID, id is "activemq:<brokerID>" and is emitted.
func TestEntitySource_TechIDPinned(t *testing.T) {
	src := newActivemqEntitySource("", "localhost", 8161, constantHostID("host-abc"))

	src.setReachable(true, "")

	// Not yet pinned — must not emit.
	if _, ok := src.Observe(); ok {
		t.Fatal("Observe() returned ok=true before the tech id is pinned")
	}

	src.pinTechID("uuid-deadbeef")

	obs, ok := src.Observe()
	if !ok {
		t.Fatal("Observe() returned ok=false after pinTechID")
	}
	if len(obs.Entities) != 1 {
		t.Fatalf("expected 1 entity, got %d", len(obs.Entities))
	}
	want := "activemq:uuid-deadbeef"
	if got := obs.Entities[0].ID["service.instance.id"]; got != want {
		t.Errorf("service.instance.id = %q, want %q", got, want)
	}
}

// TestEntitySource_IDImmutableAfterPin verifies that a second call to
// pinTechID (simulating a reconnect) does not change the pinned id.
func TestEntitySource_IDImmutableAfterPin(t *testing.T) {
	src := newActivemqEntitySource("", "localhost", 8161, constantHostID("host-abc"))
	src.setReachable(true, "")
	src.pinTechID("uuid-first")

	// Simulate a reconnect that might return a different id (should be ignored).
	src.pinTechID("uuid-second")

	obs, _ := src.Observe()
	if got := obs.Entities[0].ID["service.instance.id"]; got != "activemq:uuid-first" {
		t.Errorf("id changed after second pinTechID: %q", got)
	}
}

// TestEntitySource_NotEmittedBeforeIDPinned verifies that the entity is not
// emitted even when the broker is reachable, until the id is pinned.
func TestEntitySource_NotEmittedBeforeIDPinned(t *testing.T) {
	src := newActivemqEntitySource("", "broker1", 8161, constantHostID("host-xyz"))
	src.setReachable(true, "")

	// No pinTechID call yet — must return ok=false.
	if _, ok := src.Observe(); ok {
		t.Fatal("entity emitted before id was pinned")
	}

	// Now degrade to fallback — id is now pinned.
	src.pinFallback()

	if _, ok := src.Observe(); !ok {
		t.Fatal("Observe() returned ok=false after fallback was pinned")
	}
}

// TestEntitySource_FallbackHostID verifies that pinFallback uses
// "activemq@<hostID>" when the host id is non-empty.
func TestEntitySource_FallbackHostID(t *testing.T) {
	src := newActivemqEntitySource("", "localhost", 8161, constantHostID("machine-001"))
	src.setReachable(true, "")
	src.pinFallback()

	obs, ok := src.Observe()
	if !ok {
		t.Fatal("Observe() returned ok=false after fallback")
	}
	want := "activemq@machine-001"
	if got := obs.Entities[0].ID["service.instance.id"]; got != want {
		t.Errorf("fallback id = %q, want %q", got, want)
	}
}

// TestEntitySource_FallbackLastResort verifies that pinFallback uses "activemq"
// when the host id func returns "".
func TestEntitySource_FallbackLastResort(t *testing.T) {
	src := newActivemqEntitySource("", "localhost", 8161, constantHostID(""))
	src.setReachable(true, "")
	src.pinFallback()

	obs, ok := src.Observe()
	if !ok {
		t.Fatal("Observe() returned ok=false after last-resort fallback")
	}
	if got := obs.Entities[0].ID["service.instance.id"]; got != "activemq" {
		t.Errorf("last-resort id = %q, want %q", got, "activemq")
	}
}

// TestEntitySource_MonitorsEdgePresent verifies that when agentstate has a
// known agent instance id, the monitors edge is appended to the observation.
func TestEntitySource_MonitorsEdgePresent(t *testing.T) {
	agentstate.SetAgentInstanceID("agent-instance-xyz")
	t.Cleanup(func() { agentstate.SetAgentInstanceID("") })

	src := newActivemqEntitySource("", "localhost", 8161, constantHostID("host-abc"))
	src.setReachable(true, "")
	src.pinTechID("broker-uuid")

	obs, ok := src.Observe()
	if !ok {
		t.Fatal("Observe() returned ok=false")
	}
	if len(obs.Relations) == 0 {
		t.Fatal("expected a monitors relation, got none")
	}
	rel := obs.Relations[0]
	if rel.Type != "monitors" {
		t.Errorf("relation type = %q, want monitors", rel.Type)
	}
	if got := rel.FromID["service.instance.id"]; got != "agent-instance-xyz" {
		t.Errorf("From id = %q, want agent-instance-xyz", got)
	}
	if got := rel.ToID["service.instance.id"]; got != "activemq:broker-uuid" {
		t.Errorf("To id = %q, want activemq:broker-uuid", got)
	}
}

// TestEntitySource_MonitorsEdgeAbsent verifies that when agentstate has no
// agent instance id, no monitors edge is emitted.
func TestEntitySource_MonitorsEdgeAbsent(t *testing.T) {
	agentstate.SetAgentInstanceID("")

	src := newActivemqEntitySource("", "localhost", 8161, constantHostID("host-abc"))
	src.setReachable(true, "")
	src.pinTechID("broker-uuid")

	obs, ok := src.Observe()
	if !ok {
		t.Fatal("Observe() returned ok=false")
	}
	if len(obs.Relations) != 0 {
		t.Errorf("expected no relation when agent id is empty, got %d", len(obs.Relations))
	}
}

// TestEntitySource_DescriptiveAttrs verifies that server.address and
// server.port are present as descriptive attributes (not identity).
func TestEntitySource_DescriptiveAttrs(t *testing.T) {
	src := newActivemqEntitySource("", "broker.example.com", 8161, constantHostID("h"))
	src.setReachable(true, "6.1.0")
	src.pinTechID("some-id")

	obs, ok := src.Observe()
	if !ok {
		t.Fatal("Observe() returned ok=false")
	}
	attrs := obs.Entities[0].Attributes
	if got := attrs["server.address"]; got != "broker.example.com" {
		t.Errorf("server.address = %v, want broker.example.com", got)
	}
	if got := attrs["server.port"]; got != int64(8161) {
		t.Errorf("server.port = %v, want 8161", got)
	}
	if got := attrs["service.name"]; got != "activemq" {
		t.Errorf("service.name = %v, want activemq", got)
	}
	if got := attrs["service.version"]; got != "6.1.0" {
		t.Errorf("service.version = %v, want 6.1.0", got)
	}
}

// TestEntitySource_NoURLInID verifies that no URL, host, or port appears in
// the pinned entity id (regression guard).
func TestEntitySource_NoURLInID(t *testing.T) {
	src := newActivemqEntitySource("", "192.168.1.10", 8161, constantHostID("host-x"))
	src.setReachable(true, "")
	src.pinTechID("abc-uuid")

	obs, _ := src.Observe()
	id := obs.Entities[0].ID["service.instance.id"].(string)
	for _, forbidden := range []string{"://", "192.168", "8161", "http"} {
		if contains(id, forbidden) {
			t.Errorf("entity id %q contains forbidden substring %q", id, forbidden)
		}
	}
}

func contains(s, sub string) bool {
	return len(s) >= len(sub) && (s == sub || len(s) > 0 && containsAt(s, sub))
}

func containsAt(s, sub string) bool {
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
