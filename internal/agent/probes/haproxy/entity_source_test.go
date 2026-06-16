package haproxy

import (
	"testing"

	"senhub-agent.go/internal/agent/services/agentstate"
)

// TestEntitySource_IDFromHostID verifies that when no instance_name is set
// the stable id is "haproxy@<hostID>".
func TestEntitySource_IDFromHostID(t *testing.T) {
	src := newHAProxyEntitySource("localhost", 8080, "", "abc-host-id")
	want := "haproxy@abc-host-id"
	if src.instanceID != want {
		t.Errorf("instanceID = %q, want %q", src.instanceID, want)
	}
}

// TestEntitySource_IDFallback verifies that an empty hostID falls back to "haproxy".
func TestEntitySource_IDFallback(t *testing.T) {
	src := newHAProxyEntitySource("localhost", 8080, "", "")
	want := "haproxy"
	if src.instanceID != want {
		t.Errorf("instanceID = %q, want %q", src.instanceID, want)
	}
}

// TestEntitySource_InstanceNameOverrides verifies that a non-empty instance_name
// overrides the host-derived id entirely.
func TestEntitySource_InstanceNameOverrides(t *testing.T) {
	src := newHAProxyEntitySource("10.0.0.1", 8080, "my-haproxy-prod", "abc-host-id")
	want := "my-haproxy-prod"
	if src.instanceID != want {
		t.Errorf("instanceID = %q, want %q", src.instanceID, want)
	}
}

// TestEntitySource_NoNetworkInID verifies that neither the address nor the port
// appears in the identity.
func TestEntitySource_NoNetworkInID(t *testing.T) {
	src := newHAProxyEntitySource("192.0.2.1", 9090, "", "some-host-id")
	id := src.instanceID
	if id == "" {
		t.Fatal("instanceID must not be empty")
	}
	for _, bad := range []string{"192.0.2.1", "9090", "haproxy://"} {
		for _, c := range []rune(id) {
			_ = c
		}
		if contains(id, bad) {
			t.Errorf("instanceID %q must not contain %q (network-derived data forbidden in id)", id, bad)
		}
	}
}

func contains(s, sub string) bool {
	return len(s) >= len(sub) && (s == sub || len(sub) == 0 ||
		func() bool {
			for i := 0; i+len(sub) <= len(s); i++ {
				if s[i:i+len(sub)] == sub {
					return true
				}
			}
			return false
		}())
}

// TestEntitySource_DescriptiveAttrs verifies that server.address and server.port
// are present as descriptive attributes.
func TestEntitySource_DescriptiveAttrs(t *testing.T) {
	src := newHAProxyEntitySource("10.1.2.3", 8404, "", "h")
	if src.attrs["server.address"] != "10.1.2.3" {
		t.Errorf("server.address = %v, want %q", src.attrs["server.address"], "10.1.2.3")
	}
	if src.attrs["server.port"] != int64(8404) {
		t.Errorf("server.port = %v, want %d", src.attrs["server.port"], 8404)
	}
	if src.attrs["service.name"] != "haproxy" {
		t.Errorf("service.name = %v, want %q", src.attrs["service.name"], "haproxy")
	}
}

// TestEntitySource_ObserveBeforeReachable verifies ok=false before the first
// successful reach.
func TestEntitySource_ObserveBeforeReachable(t *testing.T) {
	src := newHAProxyEntitySource("localhost", 8080, "", "hid")
	_, ok := src.Observe()
	if ok {
		t.Error("Observe() should return ok=false before setReachable(true)")
	}
}

// TestEntitySource_ObserveAfterReachable verifies ok=true and entity presence
// after setReachable(true).
func TestEntitySource_ObserveAfterReachable(t *testing.T) {
	src := newHAProxyEntitySource("localhost", 8080, "", "hid")
	src.setReachable(true)
	obs, ok := src.Observe()
	if !ok {
		t.Fatal("Observe() should return ok=true after setReachable(true)")
	}
	if len(obs.Entities) != 1 {
		t.Fatalf("Observe() returned %d entities, want 1", len(obs.Entities))
	}
	ent := obs.Entities[0]
	if ent.Type != "service.instance" {
		t.Errorf("entity type = %q, want %q", ent.Type, "service.instance")
	}
	gotID, ok2 := ent.ID["service.instance.id"]
	if !ok2 {
		t.Fatal("entity ID missing service.instance.id key")
	}
	if gotID != "haproxy@hid" {
		t.Errorf("service.instance.id = %q, want %q", gotID, "haproxy@hid")
	}
}

// TestEntitySource_MonitorsEdge_WithAgentID verifies that a monitors relation
// is present in the observation when the agent instance id is set.
func TestEntitySource_MonitorsEdge_WithAgentID(t *testing.T) {
	agentstate.SetAgentInstanceID("agent-instance-xyz")
	t.Cleanup(func() { agentstate.SetAgentInstanceID("") })

	src := newHAProxyEntitySource("localhost", 8080, "", "hid")
	src.setReachable(true)

	obs, ok := src.Observe()
	if !ok {
		t.Fatal("Observe() returned ok=false")
	}
	if len(obs.Relations) != 1 {
		t.Fatalf("Observe() returned %d relations, want 1", len(obs.Relations))
	}
	rel := obs.Relations[0]
	if rel.Type != "monitors" {
		t.Errorf("relation type = %q, want %q", rel.Type, "monitors")
	}
	if rel.FromType != "service.instance" {
		t.Errorf("relation FromType = %q, want %q", rel.FromType, "service.instance")
	}
	if rel.ToType != "service.instance" {
		t.Errorf("relation ToType = %q, want %q", rel.ToType, "service.instance")
	}
	fromID := rel.FromID["service.instance.id"]
	if fromID != "agent-instance-xyz" {
		t.Errorf("relation FromID[service.instance.id] = %q, want %q", fromID, "agent-instance-xyz")
	}
	toID := rel.ToID["service.instance.id"]
	if toID != "haproxy@hid" {
		t.Errorf("relation ToID[service.instance.id] = %q, want %q", toID, "haproxy@hid")
	}
}

// TestEntitySource_MonitorsEdge_NoAgentID verifies that no monitors relation is
// emitted when the agent instance id is empty (entity emission disabled).
func TestEntitySource_MonitorsEdge_NoAgentID(t *testing.T) {
	agentstate.SetAgentInstanceID("")

	src := newHAProxyEntitySource("localhost", 8080, "", "hid")
	src.setReachable(true)

	obs, ok := src.Observe()
	if !ok {
		t.Fatal("Observe() returned ok=false")
	}
	if len(obs.Relations) != 0 {
		t.Errorf("Observe() returned %d relations, want 0 when agentID is empty", len(obs.Relations))
	}
}
