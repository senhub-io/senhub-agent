package phpfpm

import (
	"testing"

	"senhub-agent.go/internal/agent/services/agentstate"
)

// TestResolveInstanceID_InstanceNameTakesPrecedence verifies that a non-empty
// instance_name is used verbatim, regardless of hostID.
func TestResolveInstanceID_InstanceNameTakesPrecedence(t *testing.T) {
	got := resolveInstanceID("my-prod-fpm", "some-host-uuid")
	if got != "my-prod-fpm" {
		t.Errorf("resolveInstanceID = %q, want %q", got, "my-prod-fpm")
	}
}

// TestResolveInstanceID_HostIDFallback verifies that when instance_name is
// empty the id is "phpfpm@<hostID>".
func TestResolveInstanceID_HostIDFallback(t *testing.T) {
	got := resolveInstanceID("", "abc-123-uuid")
	want := "phpfpm@abc-123-uuid"
	if got != want {
		t.Errorf("resolveInstanceID = %q, want %q", got, want)
	}
}

// TestResolveInstanceID_LastResort verifies that "phpfpm" is returned when
// both instance_name and hostID are empty.
func TestResolveInstanceID_LastResort(t *testing.T) {
	got := resolveInstanceID("", "")
	if got != "phpfpm" {
		t.Errorf("resolveInstanceID = %q, want %q", got, "phpfpm")
	}
}

// TestNewPhpfpmEntitySource_StableID verifies that the entity source uses the
// injected hostID to form the instance id and that no network-derived value
// (URL, port) appears in the id.
func TestNewPhpfpmEntitySource_StableID(t *testing.T) {
	s := newPhpfpmEntitySource("", "test-host-uuid", "192.0.2.1", 9000)
	if s.instanceID != "phpfpm@test-host-uuid" {
		t.Errorf("instanceID = %q, want %q", s.instanceID, "phpfpm@test-host-uuid")
	}
	// server.address and server.port must be descriptive attributes, not part of id.
	if addr, ok := s.attrs["server.address"]; !ok || addr != "192.0.2.1" {
		t.Errorf("server.address = %v", s.attrs["server.address"])
	}
	if port, ok := s.attrs["server.port"]; !ok || port != int64(9000) {
		t.Errorf("server.port = %v", s.attrs["server.port"])
	}
}

// TestNewPhpfpmEntitySource_InstanceNameOverride verifies that the
// operator-supplied instance_name is used verbatim as the service.instance.id.
func TestNewPhpfpmEntitySource_InstanceNameOverride(t *testing.T) {
	s := newPhpfpmEntitySource("web-fpm-1", "test-host-uuid", "127.0.0.1", 80)
	if s.instanceID != "web-fpm-1" {
		t.Errorf("instanceID = %q, want %q", s.instanceID, "web-fpm-1")
	}
}

// TestObserve_MonitorsEdgePresent verifies that when the agent instance id is
// set and the endpoint is up, a monitors relation is included in the
// observation from the agent to the target service.instance.
func TestObserve_MonitorsEdgePresent(t *testing.T) {
	agentstate.SetAgentInstanceID("agent-instance-id")
	t.Cleanup(func() { agentstate.SetAgentInstanceID("") })

	s := newPhpfpmEntitySource("", "host-uuid", "127.0.0.1", 9000)
	s.setReachable(true, "")

	obs, ok := s.Observe()
	if !ok {
		t.Fatal("Observe() ok=false, want true when up")
	}
	if len(obs.Relations) != 1 {
		t.Fatalf("Relations count = %d, want 1", len(obs.Relations))
	}
	r := obs.Relations[0]
	if r.Type != "monitors" {
		t.Errorf("relation type = %q, want %q", r.Type, "monitors")
	}
	if r.FromType != "service.instance" {
		t.Errorf("FromType = %q, want %q", r.FromType, "service.instance")
	}
	if r.ToType != "service.instance" {
		t.Errorf("ToType = %q, want %q", r.ToType, "service.instance")
	}
	if got := r.FromID["service.instance.id"]; got != "agent-instance-id" {
		t.Errorf("FromID service.instance.id = %v, want %q", got, "agent-instance-id")
	}
	if got := r.ToID["service.instance.id"]; got != "phpfpm@host-uuid" {
		t.Errorf("ToID service.instance.id = %v, want %q", got, "phpfpm@host-uuid")
	}
}

// TestObserve_MonitorsEdgeAbsentWhenNoAgentID verifies that no monitors
// relation is emitted when the agent instance id is not yet known (entity
// emission disabled or not started).
func TestObserve_MonitorsEdgeAbsentWhenNoAgentID(t *testing.T) {
	agentstate.SetAgentInstanceID("")
	t.Cleanup(func() { agentstate.SetAgentInstanceID("") })

	s := newPhpfpmEntitySource("", "host-uuid", "127.0.0.1", 9000)
	s.setReachable(true, "")

	obs, ok := s.Observe()
	if !ok {
		t.Fatal("Observe() ok=false, want true when up")
	}
	if len(obs.Relations) != 0 {
		t.Errorf("Relations count = %d, want 0 when agent id is empty", len(obs.Relations))
	}
}

// TestObserve_NotOkWhenDown verifies that ok=false is returned before the
// first successful collect cycle (or when the endpoint is down).
func TestObserve_NotOkWhenDown(t *testing.T) {
	s := newPhpfpmEntitySource("", "host-uuid", "127.0.0.1", 9000)
	_, ok := s.Observe()
	if ok {
		t.Error("Observe() ok=true before any successful collect, want false")
	}
}

// TestObserve_EntityID verifies that the entity id in the observation uses the
// resolved instance id, not a URL or port.
func TestObserve_EntityID(t *testing.T) {
	agentstate.SetAgentInstanceID("")
	t.Cleanup(func() { agentstate.SetAgentInstanceID("") })

	s := newPhpfpmEntitySource("", "my-machine", "10.0.0.1", 8080)
	s.setReachable(true, "")

	obs, ok := s.Observe()
	if !ok {
		t.Fatal("Observe() ok=false, want true")
	}
	if len(obs.Entities) != 1 {
		t.Fatalf("Entities count = %d, want 1", len(obs.Entities))
	}
	e := obs.Entities[0]
	if e.Type != "service.instance" {
		t.Errorf("entity type = %q, want %q", e.Type, "service.instance")
	}
	got, _ := e.ID["service.instance.id"].(string)
	if got != "phpfpm@my-machine" {
		t.Errorf("service.instance.id = %q, want %q", got, "phpfpm@my-machine")
	}
}
