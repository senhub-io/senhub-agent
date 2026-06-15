package wildfly

import (
	"testing"

	"senhub-agent.go/internal/agent/services/agentstate"
)

// stubHostID returns a fixed host id for hermetic unit tests.
func stubHostID(id string) func() string {
	return func() string { return id }
}

// TestEntitySource_InstanceName_Override verifies that when instance_name is
// set it is used verbatim as service.instance.id, regardless of hostID.
func TestEntitySource_InstanceName_Override(t *testing.T) {
	src := newWildflyEntitySource("http://localhost:9990", "my-wf-prod", stubHostID("abc-host-id"))
	src.setReachable(true, "")

	obs, ok := src.Observe()
	if !ok {
		t.Fatal("Observe returned ok=false, want true")
	}
	if len(obs.Entities) != 1 {
		t.Fatalf("len(entities) = %d, want 1", len(obs.Entities))
	}
	got, _ := obs.Entities[0].ID["service.instance.id"].(string)
	if got != "my-wf-prod" {
		t.Errorf("service.instance.id = %q, want %q", got, "my-wf-prod")
	}
}

// TestEntitySource_HostID_Default verifies that when instance_name is empty,
// the id is "wildfly@<hostID>".
func TestEntitySource_HostID_Default(t *testing.T) {
	src := newWildflyEntitySource("http://localhost:9990", "", stubHostID("deadbeef-1234"))
	src.setReachable(true, "")

	obs, ok := src.Observe()
	if !ok {
		t.Fatal("Observe returned ok=false, want true")
	}
	if len(obs.Entities) != 1 {
		t.Fatalf("len(entities) = %d, want 1", len(obs.Entities))
	}
	got, _ := obs.Entities[0].ID["service.instance.id"].(string)
	want := "wildfly@deadbeef-1234"
	if got != want {
		t.Errorf("service.instance.id = %q, want %q", got, want)
	}
}

// TestEntitySource_LastResort verifies that when instance_name is empty and
// hostID resolution fails, the id falls back to "wildfly".
func TestEntitySource_LastResort(t *testing.T) {
	src := newWildflyEntitySource("http://localhost:9990", "", stubHostID(""))
	src.setReachable(true, "")

	obs, ok := src.Observe()
	if !ok {
		t.Fatal("Observe returned ok=false, want true")
	}
	if len(obs.Entities) != 1 {
		t.Fatalf("len(entities) = %d, want 1", len(obs.Entities))
	}
	got, _ := obs.Entities[0].ID["service.instance.id"].(string)
	if got != "wildfly" {
		t.Errorf("service.instance.id = %q, want %q", got, "wildfly")
	}
}

// TestEntitySource_MonitorsEdge_Present verifies that a monitors relation is
// emitted when the agent instance id is set.
func TestEntitySource_MonitorsEdge_Present(t *testing.T) {
	agentstate.SetAgentInstanceID("agent-instance-001")
	t.Cleanup(func() { agentstate.SetAgentInstanceID("") })

	src := newWildflyEntitySource("http://localhost:9990", "my-wf", stubHostID(""))
	src.setReachable(true, "")

	obs, ok := src.Observe()
	if !ok {
		t.Fatal("Observe returned ok=false, want true")
	}
	if len(obs.Relations) != 1 {
		t.Fatalf("len(relations) = %d, want 1", len(obs.Relations))
	}
	r := obs.Relations[0]
	if r.Type != "monitors" {
		t.Errorf("relation type = %q, want %q", r.Type, "monitors")
	}
	if r.FromType != "service.instance" {
		t.Errorf("FromType = %q, want %q", r.FromType, "service.instance")
	}
	fromID, _ := r.FromID["service.instance.id"].(string)
	if fromID != "agent-instance-001" {
		t.Errorf("FromID service.instance.id = %q, want %q", fromID, "agent-instance-001")
	}
	if r.ToType != "service.instance" {
		t.Errorf("ToType = %q, want %q", r.ToType, "service.instance")
	}
	toID, _ := r.ToID["service.instance.id"].(string)
	if toID != "my-wf" {
		t.Errorf("ToID service.instance.id = %q, want %q", toID, "my-wf")
	}
}

// TestEntitySource_MonitorsEdge_Absent verifies that no monitors relation is
// emitted when the agent instance id is not set (entity emission off).
func TestEntitySource_MonitorsEdge_Absent(t *testing.T) {
	agentstate.SetAgentInstanceID("")
	t.Cleanup(func() { agentstate.SetAgentInstanceID("") })

	src := newWildflyEntitySource("http://localhost:9990", "my-wf", stubHostID(""))
	src.setReachable(true, "")

	obs, ok := src.Observe()
	if !ok {
		t.Fatal("Observe returned ok=false, want true")
	}
	if len(obs.Relations) != 0 {
		t.Errorf("len(relations) = %d, want 0 when agentID is empty", len(obs.Relations))
	}
}

// TestEntitySource_DescriptiveAttrs verifies that server.address and
// server.port are kept as descriptive attributes (not part of the id).
func TestEntitySource_DescriptiveAttrs(t *testing.T) {
	src := newWildflyEntitySource("http://wf.example.com:9999", "", stubHostID("h1"))
	src.setReachable(true, "")

	obs, ok := src.Observe()
	if !ok {
		t.Fatal("Observe returned ok=false, want true")
	}
	if len(obs.Entities) != 1 {
		t.Fatalf("len(entities) = %d, want 1", len(obs.Entities))
	}
	attrs := obs.Entities[0].Attributes
	if v, _ := attrs["server.address"].(string); v != "wf.example.com" {
		t.Errorf("server.address = %q, want %q", v, "wf.example.com")
	}
	if v, _ := attrs["server.port"].(int64); v != 9999 {
		t.Errorf("server.port = %d, want 9999", v)
	}
	if _, ok := obs.Entities[0].ID["server.address"]; ok {
		t.Error("server.address must not appear in the entity ID")
	}
	if _, ok := obs.Entities[0].ID["server.port"]; ok {
		t.Error("server.port must not appear in the entity ID")
	}
}

// TestEntitySource_Down verifies that Observe returns ok=false when
// the endpoint is unreachable.
func TestEntitySource_Down(t *testing.T) {
	src := newWildflyEntitySource("http://localhost:9990", "", stubHostID("h1"))
	// setReachable not called — defaults to down

	_, ok := src.Observe()
	if ok {
		t.Error("Observe returned ok=true, want false when down")
	}
}

// TestEntitySource_ServiceName verifies that service.name is always "wildfly".
func TestEntitySource_ServiceName(t *testing.T) {
	src := newWildflyEntitySource("http://localhost:9990", "", stubHostID("h1"))
	src.setReachable(true, "")

	obs, ok := src.Observe()
	if !ok {
		t.Fatal("Observe returned ok=false, want true")
	}
	if v, _ := obs.Entities[0].Attributes["service.name"].(string); v != "wildfly" {
		t.Errorf("service.name = %q, want %q", v, "wildfly")
	}
}
