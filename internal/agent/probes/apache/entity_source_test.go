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
	rel, found := findRelation(obs, "monitors")
	if !found {
		t.Fatalf("no monitors relation; got %v", relTypes(obs))
	}
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
	// No monitors edge when the agent id is empty. A runs_on edge may still be
	// present (the localhost endpoint is loopback) — assert on the monitors type,
	// not the relation count.
	if _, found := findRelation(obs, "monitors"); found {
		t.Errorf("monitors relation must be absent when agent id empty; got %v", relTypes(obs))
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

// relTypes lists the relation types in an observation.
func relTypes(obs entity.Observation) []string {
	var ts []string
	for _, r := range obs.Relations {
		ts = append(ts, r.Type)
	}
	return ts
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

// TestEntitySource_LocalRunsOn verifies a loopback-monitored apache emits a
// runs_on→host edge (so it does not float), and a remote-monitored one does not.
func TestEntitySource_LocalRunsOn(t *testing.T) {
	agentstate.SetAgentInstanceID("agent-1")
	t.Cleanup(func() { agentstate.SetAgentInstanceID("") })

	// Loopback endpoint → runs_on present, targeting the agent host.
	local := newApacheEntitySource("", "H", "127.0.0.1", 80)
	local.setReachable(true, "")
	obs, _ := local.Observe()
	runsOn, found := findRelation(obs, "runs_on")
	if !found {
		t.Fatalf("loopback apache: expected a runs_on edge, got relations %v", relTypes(obs))
	}
	if runsOn.ToType != "host" || runsOn.ToID["host.id"] != "H" {
		t.Errorf("runs_on target = %s/%v, want host/H", runsOn.ToType, runsOn.ToID)
	}
	if runsOn.FromID["service.instance.id"] != "apache@H" {
		t.Errorf("runs_on source = %v, want apache@H", runsOn.FromID)
	}

	// Remote endpoint → no runs_on.
	remote := newApacheEntitySource("", "H", "10.0.0.5", 80)
	remote.setReachable(true, "")
	robs, _ := remote.Observe()
	if _, found := findRelation(robs, "runs_on"); found {
		t.Errorf("remote apache must NOT emit runs_on; relations=%v", relTypes(robs))
	}
}

// Compile-time check that apacheEntitySource implements entity.Source.
var _ entity.Source = (*apacheEntitySource)(nil)
