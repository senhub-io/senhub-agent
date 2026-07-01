package phpfpm

import (
	"testing"

	"senhub-agent.go/internal/agent/services/agentstate"
	"senhub-agent.go/internal/agent/services/entity"
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
	r := phpfpmRelByType(obs, "monitors")
	if r == nil {
		t.Fatalf("expected a monitors relation, got %v", phpfpmRelTypes(obs))
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
	// A runs_on edge may still be present (127.0.0.1 endpoint is loopback) — that
	// is independent of the agent id, so assert specifically on the monitors type.
	if phpfpmRelByType(obs, "monitors") != nil {
		t.Errorf("expected no monitors relation when agent id is empty, got %v", phpfpmRelTypes(obs))
	}
}

// TestObserve_LocalRunsOn verifies a loopback-monitored pool emits a
// runs_on→host edge, while a remote one does not.
func TestObserve_LocalRunsOn(t *testing.T) {
	agentstate.SetAgentInstanceID("agent-instance-id")
	t.Cleanup(func() { agentstate.SetAgentInstanceID("") })

	local := newPhpfpmEntitySource("", "H", "127.0.0.1", 9000)
	local.setReachable(true, "")
	obs, _ := local.Observe()
	runsOn := phpfpmRelByType(obs, "runs_on")
	if runsOn == nil {
		t.Fatalf("loopback phpfpm: expected a runs_on edge, got %v", phpfpmRelTypes(obs))
	}
	if runsOn.ToType != "host" || runsOn.ToID["host.id"] != "H" {
		t.Errorf("runs_on target = %s/%v, want host/H", runsOn.ToType, runsOn.ToID)
	}
	if runsOn.FromID["service.instance.id"] != "phpfpm@H" {
		t.Errorf("runs_on source = %v, want phpfpm@H", runsOn.FromID)
	}

	remote := newPhpfpmEntitySource("", "H", "10.0.0.5", 9000)
	remote.setReachable(true, "")
	robs, _ := remote.Observe()
	if phpfpmRelByType(robs, "runs_on") != nil {
		t.Errorf("remote phpfpm must NOT emit runs_on; relations=%v", phpfpmRelTypes(robs))
	}
}

func phpfpmRelByType(obs entity.Observation, ty string) *entity.Relation {
	for i := range obs.Relations {
		if obs.Relations[i].Type == ty {
			return &obs.Relations[i]
		}
	}
	return nil
}

func phpfpmRelTypes(obs entity.Observation) []string {
	var ts []string
	for _, r := range obs.Relations {
		ts = append(ts, r.Type)
	}
	return ts
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
