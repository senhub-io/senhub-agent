package apache

import (
	"strings"
	"testing"

	"senhub-agent.go/internal/agent/services/agentstate"
	"senhub-agent.go/internal/agent/services/entity"
)

// TestResolveInstanceID_InstanceNameOverrides verifies that an explicit
// instance_name is used verbatim and wins over the host-id fallback.
func TestResolveInstanceID_InstanceNameOverrides(t *testing.T) {
	id := resolveInstanceID("my-apache", "host-abc")
	if id != "my-apache" {
		t.Errorf("resolveInstanceID = %q, want %q", id, "my-apache")
	}
}

// TestResolveInstanceID_HostIDFallback verifies that the id is "apache@<hostID>"
// when no instance_name is configured and the host id is available.
func TestResolveInstanceID_HostIDFallback(t *testing.T) {
	id := resolveInstanceID("", "host-abc")
	want := "apache@host-abc"
	if id != want {
		t.Errorf("resolveInstanceID = %q, want %q", id, want)
	}
}

// TestResolveInstanceID_LastResort verifies the bare "apache" fallback when
// both instance_name and host id are unavailable.
func TestResolveInstanceID_LastResort(t *testing.T) {
	id := resolveInstanceID("", "")
	if id != "apache" {
		t.Errorf("resolveInstanceID = %q, want %q", id, "apache")
	}
}

// TestEntitySource_IDNeverNetworkDerived verifies that the instance id
// never contains a URL, port, or IP — the old "apache://host:port" form.
func TestEntitySource_IDNeverNetworkDerived(t *testing.T) {
	src := newApacheEntitySource("", "stub-host-id", "192.168.1.10", 8080)
	if strings.Contains(src.instanceID, "://") || strings.Contains(src.instanceID, ":8080") {
		t.Errorf("instance id %q must not be network-derived", src.instanceID)
	}
	if src.instanceID != "apache@stub-host-id" {
		t.Errorf("instance id = %q, want %q", src.instanceID, "apache@stub-host-id")
	}
}

// TestEntitySource_NotReadyBeforeFirstCollect verifies that Observe returns
// ok=false before the first setReachable call.
func TestEntitySource_NotReadyBeforeFirstCollect(t *testing.T) {
	src := newApacheEntitySource("", "h1", "localhost", 80)
	_, ok := src.Observe()
	if ok {
		t.Error("Observe returned ok=true before first setReachable, want false")
	}
}

// TestEntitySource_MonitorsEdgePresent verifies that the monitors relation is
// included in the observation when an agent id is set.
func TestEntitySource_MonitorsEdgePresent(t *testing.T) {
	agentstate.SetAgentInstanceID("agent-instance-1")

	src := newApacheEntitySource("", "h1", "localhost", 80)
	src.setReachable(true, "")

	obs, ok := src.Observe()
	if !ok {
		t.Fatal("Observe ok=false after setReachable(true)")
	}
	if len(obs.Entities) != 1 {
		t.Fatalf("got %d entities, want 1", len(obs.Entities))
	}
	if len(obs.Relations) != 1 {
		t.Fatalf("got %d relations, want 1 (monitors)", len(obs.Relations))
	}
	rel := obs.Relations[0]
	if rel.Type != "monitors" {
		t.Errorf("relation.Type = %q, want %q", rel.Type, "monitors")
	}
	if rel.FromType != "service.instance" {
		t.Errorf("relation.FromType = %q, want %q", rel.FromType, "service.instance")
	}
	if rel.ToType != "service.instance" {
		t.Errorf("relation.ToType = %q, want %q", rel.ToType, "service.instance")
	}
	if rel.FromID["service.instance.id"] != "agent-instance-1" {
		t.Errorf("FromID service.instance.id = %v, want agent-instance-1", rel.FromID["service.instance.id"])
	}
	if rel.ToID["service.instance.id"] != "apache@h1" {
		t.Errorf("ToID service.instance.id = %v, want apache@h1", rel.ToID["service.instance.id"])
	}
}

// TestEntitySource_MonitorsEdgeAbsentWhenNoAgentID verifies that the monitors
// relation is omitted when the agent instance id is not yet resolved.
func TestEntitySource_MonitorsEdgeAbsentWhenNoAgentID(t *testing.T) {
	// Reset agent instance id to empty.
	agentstate.SetAgentInstanceID("")

	src := newApacheEntitySource("", "h2", "localhost", 80)
	src.setReachable(true, "")

	obs, ok := src.Observe()
	if !ok {
		t.Fatal("Observe ok=false after setReachable(true)")
	}
	if len(obs.Relations) != 0 {
		t.Errorf("got %d relations, want 0 when agent id is empty", len(obs.Relations))
	}
}

// TestEntitySource_DescriptiveAttributesPreserved verifies that server.address
// and server.port remain as descriptive attributes (not part of the identity).
func TestEntitySource_DescriptiveAttributesPreserved(t *testing.T) {
	agentstate.SetAgentInstanceID("")

	src := newApacheEntitySource("", "h3", "10.0.0.1", 8080)
	src.setReachable(true, "")

	obs, ok := src.Observe()
	if !ok {
		t.Fatal("Observe ok=false")
	}
	if len(obs.Entities) != 1 {
		t.Fatalf("got %d entities, want 1", len(obs.Entities))
	}
	e := obs.Entities[0]
	if e.Attributes["server.address"] != "10.0.0.1" {
		t.Errorf("server.address = %v, want 10.0.0.1", e.Attributes["server.address"])
	}
	if e.Attributes["server.port"] != int64(8080) {
		t.Errorf("server.port = %v, want 8080", e.Attributes["server.port"])
	}
	if e.Attributes["service.name"] != "apache" {
		t.Errorf("service.name = %v, want apache", e.Attributes["service.name"])
	}
	// The id must not contain the address or port.
	if _, hasAddr := e.ID["server.address"]; hasAddr {
		t.Error("server.address must not be in the entity ID")
	}
}

// TestEntitySource_UpFalse verifies that an unreachable server produces an
// empty Observation (deletion signal) with ok=true.
func TestEntitySource_UpFalse(t *testing.T) {
	src := newApacheEntitySource("", "h4", "localhost", 80)
	src.setReachable(true, "")
	src.setReachable(false, "")

	obs, ok := src.Observe()
	if !ok {
		t.Fatal("Observe ok=false after setReachable(false), want true (deletion signal)")
	}
	if len(obs.Entities) != 0 || len(obs.Relations) != 0 {
		t.Errorf("expected empty observation, got entities=%d relations=%d", len(obs.Entities), len(obs.Relations))
	}
}

// TestEntitySource_InstanceNameOverridesHostID verifies end-to-end that
// an explicit instance_name flows through to the entity id.
func TestEntitySource_InstanceNameOverridesHostID(t *testing.T) {
	agentstate.SetAgentInstanceID("")

	src := newApacheEntitySource("prod-apache-primary", "h5", "localhost", 80)
	src.setReachable(true, "")

	obs, ok := src.Observe()
	if !ok {
		t.Fatal("Observe ok=false")
	}
	id := obs.Entities[0].ID["service.instance.id"]
	if id != "prod-apache-primary" {
		t.Errorf("service.instance.id = %v, want prod-apache-primary", id)
	}
}

// TestEntitySource_VersionAttribute verifies that a non-empty version string
// is included as service.version in the entity attributes.
func TestEntitySource_VersionAttribute(t *testing.T) {
	agentstate.SetAgentInstanceID("")

	src := newApacheEntitySource("", "h6", "localhost", 80)
	src.setReachable(true, "Apache/2.4.51")

	obs, _ := src.Observe()
	if len(obs.Entities) == 0 {
		t.Fatal("no entities")
	}
	if obs.Entities[0].Attributes["service.version"] != "Apache/2.4.51" {
		t.Errorf("service.version = %v, want Apache/2.4.51", obs.Entities[0].Attributes["service.version"])
	}
}

// Compile-time check that apacheEntitySource implements entity.Source.
var _ entity.Source = (*apacheEntitySource)(nil)
