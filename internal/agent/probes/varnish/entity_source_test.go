package varnish

import (
	"testing"

	"senhub-agent.go/internal/agent/services/agentstate"
	"senhub-agent.go/internal/agent/services/entity"
)

// relByType returns a pointer to the first relation of the given type, or nil.
func relByType(obs entity.Observation, ty string) *entity.Relation {
	for i := range obs.Relations {
		if obs.Relations[i].Type == ty {
			return &obs.Relations[i]
		}
	}
	return nil
}

// relTypes lists the relation types in an observation.
func relTypes(obs entity.Observation) []string {
	var ts []string
	for _, r := range obs.Relations {
		ts = append(ts, r.Type)
	}
	return ts
}

// TestResolveInstanceID verifies the D1 precedence rule in isolation.
func TestResolveInstanceID_InstanceNameTakesPrecedence(t *testing.T) {
	id := resolveInstanceID("my-varnish", "host-123")
	if id != "my-varnish" {
		t.Errorf("resolveInstanceID = %q, want %q", id, "my-varnish")
	}
}

func TestResolveInstanceID_HostIDWhenNoInstanceName(t *testing.T) {
	id := resolveInstanceID("", "abc-def-123")
	if id != "varnish@abc-def-123" {
		t.Errorf("resolveInstanceID = %q, want %q", id, "varnish@abc-def-123")
	}
}

func TestResolveInstanceID_LastResortWhenBothEmpty(t *testing.T) {
	id := resolveInstanceID("", "")
	if id != "varnish" {
		t.Errorf("resolveInstanceID = %q, want %q", id, "varnish")
	}
}

// TestEntitySource_InstanceID_FromHostID checks that, without instance_name,
// the service.instance.id is "varnish@<hostid>" (hermetic — no real gopsutil).
func TestEntitySource_InstanceID_FromHostID(t *testing.T) {
	src := newVarnishEntitySource("", "stub-host-id")
	src.setReachable(true)

	obs, ok := src.Observe()
	if !ok {
		t.Fatal("Observe() returned ok=false; want true")
	}
	if len(obs.Entities) != 1 {
		t.Fatalf("Observe() entities count = %d, want 1", len(obs.Entities))
	}
	got := obs.Entities[0].ID["service.instance.id"]
	if got != "varnish@stub-host-id" {
		t.Errorf("service.instance.id = %q, want %q", got, "varnish@stub-host-id")
	}
}

// TestEntitySource_InstanceID_OverriddenByInstanceName checks that
// instance_name takes priority over the host-derived id.
func TestEntitySource_InstanceID_OverriddenByInstanceName(t *testing.T) {
	src := newVarnishEntitySource("prod-frontend", "stub-host-id")
	src.setReachable(true)

	obs, ok := src.Observe()
	if !ok {
		t.Fatal("Observe() returned ok=false; want true")
	}
	if len(obs.Entities) != 1 {
		t.Fatalf("Observe() entities count = %d, want 1", len(obs.Entities))
	}
	got := obs.Entities[0].ID["service.instance.id"]
	if got != "prod-frontend" {
		t.Errorf("service.instance.id = %q, want %q", got, "prod-frontend")
	}
}

// TestEntitySource_DescriptiveAttrs verifies that service.name and
// server.address are present as descriptive attributes (not identity fields).
func TestEntitySource_DescriptiveAttrs(t *testing.T) {
	src := newVarnishEntitySource("", "h")
	src.setReachable(true)

	obs, ok := src.Observe()
	if !ok || len(obs.Entities) != 1 {
		t.Fatalf("Observe() ok=%v, entities=%d; want ok=true, 1 entity", ok, len(obs.Entities))
	}
	attrs := obs.Entities[0].Attributes
	if attrs["service.name"] != "varnish" {
		t.Errorf("service.name = %v, want %q", attrs["service.name"], "varnish")
	}
	if attrs["server.address"] != "localhost" {
		t.Errorf("server.address = %v, want %q", attrs["server.address"], "localhost")
	}
}

// TestEntitySource_NoURLInID asserts that the old network-derived id form is
// gone: the id must not contain "://", a port separator, or an IP pattern.
func TestEntitySource_NoURLInID(t *testing.T) {
	for _, tc := range []struct {
		instanceName string
		hostID       string
	}{
		{"", ""},
		{"", "some-host-uuid"},
		{"myname", "some-host-uuid"},
	} {
		id := resolveInstanceID(tc.instanceName, tc.hostID)
		for _, bad := range []string{"://", ":80", "localhost", "127.0.0.1"} {
			if contains(id, bad) {
				t.Errorf("resolveInstanceID(%q, %q) = %q contains forbidden substring %q",
					tc.instanceName, tc.hostID, id, bad)
			}
		}
	}
}

func contains(s, sub string) bool {
	return len(s) >= len(sub) && (s == sub || len(s) > 0 && findSub(s, sub))
}

func findSub(s, sub string) bool {
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}

// TestEntitySource_MonitorsEdge_Present verifies that a monitors relation is
// emitted when the agent instance id is set.
func TestEntitySource_MonitorsEdge_Present(t *testing.T) {
	agentstate.SetAgentInstanceID("agent-instance-abc")
	t.Cleanup(func() { agentstate.SetAgentInstanceID("") })

	src := newVarnishEntitySource("", "hostx")
	src.setReachable(true)

	obs, ok := src.Observe()
	if !ok {
		t.Fatal("Observe() returned ok=false")
	}
	// varnishstat is always local, so a runs_on edge is also present; assert by
	// type, not count.
	rel := relByType(obs, "monitors")
	if rel == nil {
		t.Fatalf("no monitors relation; got %v", relTypes(obs))
	}
	if rel.FromType != "service.instance" {
		t.Errorf("relation FromType = %q, want %q", rel.FromType, "service.instance")
	}
	if rel.FromID["service.instance.id"] != "agent-instance-abc" {
		t.Errorf("relation FromID = %v, want agent-instance-abc", rel.FromID)
	}
	if rel.ToType != "service.instance" {
		t.Errorf("relation ToType = %q, want %q", rel.ToType, "service.instance")
	}
	if rel.ToID["service.instance.id"] != "varnish@hostx" {
		t.Errorf("relation ToID = %v, want varnish@hostx", rel.ToID)
	}
}

// TestEntitySource_MonitorsEdge_Absent verifies that no relation is emitted
// when the agent instance id is empty (entity emission off or not started).
func TestEntitySource_MonitorsEdge_Absent(t *testing.T) {
	agentstate.SetAgentInstanceID("")
	t.Cleanup(func() { agentstate.SetAgentInstanceID("") })

	src := newVarnishEntitySource("", "hostx")
	src.setReachable(true)

	obs, ok := src.Observe()
	if !ok {
		t.Fatal("Observe() returned ok=false")
	}
	// A runs_on edge is still present (varnishstat is always local); only the
	// monitors edge must be absent when the agent id is empty.
	if relByType(obs, "monitors") != nil {
		t.Errorf("monitors relation must be absent when agent id is empty; got %v", relTypes(obs))
	}
}

// TestEntitySource_LocalRunsOn verifies that the varnish instance is anchored to
// the agent host with a runs_on edge — varnishstat is always read locally
// (server.address "localhost"), so the target is loopback. The host-scoped id
// "varnish@<host>" carries no loopback literal, so the collapse guard passes.
func TestEntitySource_LocalRunsOn(t *testing.T) {
	agentstate.SetAgentInstanceID("agent-1")
	t.Cleanup(func() { agentstate.SetAgentInstanceID("") })

	src := newVarnishEntitySource("", "H")
	src.setReachable(true)
	obs, _ := src.Observe()
	runsOn := relByType(obs, "runs_on")
	if runsOn == nil {
		t.Fatalf("local varnish: expected a runs_on edge, got relations %v", relTypes(obs))
	}
	if runsOn.ToType != "host" || runsOn.ToID["host.id"] != "H" {
		t.Errorf("runs_on target = %s/%v, want host/H", runsOn.ToType, runsOn.ToID)
	}
	if runsOn.FromID["service.instance.id"] != "varnish@H" {
		t.Errorf("runs_on source = %v, want varnish@H", runsOn.FromID)
	}

	// Host id unreadable → no runs_on (helper refuses an empty hostID).
	noHost := newVarnishEntitySource("", "")
	noHost.setReachable(true)
	nobs, _ := noHost.Observe()
	if relByType(nobs, "runs_on") != nil {
		t.Errorf("varnish with empty host id must NOT emit runs_on; relations=%v", relTypes(nobs))
	}
}

// TestEntitySource_NotUp verifies that Observe returns ok=false when the probe
// has not reported a successful collection yet (or the last one failed).
func TestEntitySource_NotUp(t *testing.T) {
	src := newVarnishEntitySource("", "hostx")
	// do NOT call setReachable(true) — default is down

	_, ok := src.Observe()
	if ok {
		t.Error("Observe() returned ok=true; want false when probe is down")
	}
}
