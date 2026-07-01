package consul

import (
	"testing"

	"senhub-agent.go/internal/agent/services/agentstate"
	"senhub-agent.go/internal/agent/services/entity"
)

// makeTestSource builds a consulEntitySource with an injected hostIDFn so
// tests are hermetic (no real gopsutil call).
func makeTestSource(instanceName, host string, port int, hostID string) *consulEntitySource {
	s := newConsulEntitySource(instanceName, host, port)
	s.hostIDFn = func() string { return hostID }
	s.hostID = hostID
	return s
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

// instanceIDOf returns the service.instance.id from the first entity in obs,
// or "" when no entities are present.
func instanceIDOf(obs entity.Observation) string {
	if len(obs.Entities) == 0 {
		return ""
	}
	v, _ := obs.Entities[0].ID["service.instance.id"].(string)
	return v
}

// TestEntitySource_InstanceNameOverride: when "instance_name" is set in config
// the entity is emitted immediately (id pinned at construction) with that exact
// value, bypassing the tech-id fetch.
func TestEntitySource_InstanceNameOverride(t *testing.T) {
	s := makeTestSource("my-consul", "localhost", 8500, "host-uuid-1")

	// The id is already pinned from config, so a successful collect with any
	// nodeID must not change it.
	s.setReachable(true, "unrelated-node-uuid", "1.17.0")

	obs, ok := s.Observe()
	if !ok {
		t.Fatal("Observe() returned ok=false after setReachable(true), want ok=true")
	}
	if got := instanceIDOf(obs); got != "my-consul" {
		t.Errorf("service.instance.id = %q, want %q", got, "my-consul")
	}
}

// TestEntitySource_TechIDPinned: without instance_name the entity MUST NOT be
// emitted before the first successful collect, and after a successful collect
// the id must be "consul:<node-id>".
func TestEntitySource_TechIDPinned(t *testing.T) {
	s := makeTestSource("", "consul.internal", 8500, "host-uuid-1")

	// Before any collect: Observe must return ok=false.
	_, ok := s.Observe()
	if ok {
		t.Fatal("Observe() returned ok=true before first collect, want ok=false")
	}

	// Simulate a successful collect with a node UUID.
	const nodeUUID = "a1b2c3d4-e5f6-7890-abcd-ef1234567890"
	s.setReachable(true, nodeUUID, "1.17.0")

	obs, ok := s.Observe()
	if !ok {
		t.Fatal("Observe() returned ok=false after setReachable(true), want ok=true")
	}
	want := "consul:" + nodeUUID
	if got := instanceIDOf(obs); got != want {
		t.Errorf("service.instance.id = %q, want %q", got, want)
	}
}

// TestEntitySource_NotEmittedBeforePin: the entity must not be emitted when
// setReachable(false) is called before the id is ever pinned.
func TestEntitySource_NotEmittedBeforePin(t *testing.T) {
	s := makeTestSource("", "consul.internal", 8500, "host-uuid-1")

	// Simulate a failing first collect.
	s.setReachable(false, "", "")

	_, ok := s.Observe()
	if ok {
		t.Fatal("Observe() returned ok=true after failed pre-pin collect, want ok=false")
	}
}

// TestEntitySource_IDImmutableAfterPin: once the id is pinned from the tech
// source, a subsequent collect with a different nodeID must not change it.
func TestEntitySource_IDImmutableAfterPin(t *testing.T) {
	s := makeTestSource("", "consul.internal", 8500, "host-uuid-1")

	const firstUUID = "aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee"
	s.setReachable(true, firstUUID, "1.17.0")

	firstID := instanceIDOf(func() entity.Observation { obs, _ := s.Observe(); return obs }())

	// A second collect with a different UUID must not re-key the entity.
	s.setReachable(true, "ffffffff-ffff-ffff-ffff-ffffffffffff", "1.18.0")
	secondID := instanceIDOf(func() entity.Observation { obs, _ := s.Observe(); return obs }())

	if firstID != secondID {
		t.Errorf("id changed after pin: first=%q, second=%q", firstID, secondID)
	}
	want := "consul:" + firstUUID
	if secondID != want {
		t.Errorf("service.instance.id = %q, want %q", secondID, want)
	}
}

// TestEntitySource_FallbackPath: when the target is reachable but returns an
// empty NodeID, the entity source must degrade to "consul@<host.id>".
func TestEntitySource_FallbackPath(t *testing.T) {
	const hostID = "machine-id-abcdef"
	s := makeTestSource("", "consul.internal", 8500, hostID)

	// Successful collect but no node UUID.
	s.setReachable(true, "", "1.17.0")

	obs, ok := s.Observe()
	if !ok {
		t.Fatal("Observe() returned ok=false after fallback pin, want ok=true")
	}
	want := "consul@" + hostID
	if got := instanceIDOf(obs); got != want {
		t.Errorf("service.instance.id = %q, want %q", got, want)
	}
}

// TestEntitySource_FallbackLastResort: when both the node UUID and the host.id
// are unavailable, the id must fall back to the bare "consul" string.
func TestEntitySource_FallbackLastResort(t *testing.T) {
	s := makeTestSource("", "consul.internal", 8500, "") // empty hostID

	s.setReachable(true, "", "1.17.0")

	obs, ok := s.Observe()
	if !ok {
		t.Fatal("Observe() returned ok=false after last-resort pin, want ok=true")
	}
	if got := instanceIDOf(obs); got != "consul" {
		t.Errorf("service.instance.id = %q, want %q", got, "consul")
	}
}

// TestEntitySource_MonitorsEdge_Present: when agentstate carries an agent id
// the entity observation must include a monitors relation pointing from the
// agent to the consul service.instance.
func TestEntitySource_MonitorsEdge_Present(t *testing.T) {
	const agentID = "senhub-agent-instance-42"
	agentstate.SetAgentInstanceID(agentID)
	t.Cleanup(func() { agentstate.SetAgentInstanceID("") })

	const nodeUUID = "11111111-2222-3333-4444-555555555555"
	s := makeTestSource("", "consul.internal", 8500, "host-uuid-1")
	s.setReachable(true, nodeUUID, "1.17.0")

	obs, ok := s.Observe()
	if !ok {
		t.Fatal("Observe() ok=false, want ok=true")
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
	if got, _ := rel.FromID["service.instance.id"].(string); got != agentID {
		t.Errorf("relation.FromID[service.instance.id] = %q, want %q", got, agentID)
	}
	if got, _ := rel.ToID["service.instance.id"].(string); got != "consul:"+nodeUUID {
		t.Errorf("relation.ToID[service.instance.id] = %q, want %q", got, "consul:"+nodeUUID)
	}
}

// TestEntitySource_MonitorsEdge_AbsentWhenNoAgentID: when agentstate returns
// "" the monitors edge must NOT be emitted (a From endpoint that cannot resolve
// would be buffered then dropped by the consumer).
func TestEntitySource_MonitorsEdge_AbsentWhenNoAgentID(t *testing.T) {
	agentstate.SetAgentInstanceID("")
	// no t.Cleanup needed — already "".

	s := makeTestSource("", "consul.internal", 8500, "host-uuid-1")
	s.setReachable(true, "dead-beef-1234-5678-90ab-cdef01234567", "1.17.0")

	obs, ok := s.Observe()
	if !ok {
		t.Fatal("Observe() ok=false, want ok=true")
	}
	// The target is remote (consul.internal), so no runs_on either; assert the
	// monitors edge specifically is absent rather than counting relations.
	if _, found := findRelation(obs, "monitors"); found {
		t.Errorf("monitors relation must be absent when agent id is empty; got %v", relTypes(obs))
	}
}

// TestEntitySource_LocalRunsOn verifies a loopback-monitored consul emits a
// runs_on→host edge (so it does not float), and a remote-monitored one does not.
func TestEntitySource_LocalRunsOn(t *testing.T) {
	agentstate.SetAgentInstanceID("agent-1")
	t.Cleanup(func() { agentstate.SetAgentInstanceID("") })

	// Loopback endpoint → runs_on present, targeting the agent host.
	local := makeTestSource("", "127.0.0.1", 8500, "H")
	local.setReachable(true, "node-uuid", "1.17.0")
	obs, _ := local.Observe()
	runsOn, found := findRelation(obs, "runs_on")
	if !found {
		t.Fatalf("loopback consul: expected a runs_on edge, got relations %v", relTypes(obs))
	}
	if runsOn.ToType != "host" || runsOn.ToID["host.id"] != "H" {
		t.Errorf("runs_on target = %s/%v, want host/H", runsOn.ToType, runsOn.ToID)
	}
	if runsOn.FromID["service.instance.id"] != "consul:node-uuid" {
		t.Errorf("runs_on source = %v, want consul:node-uuid", runsOn.FromID)
	}

	// Remote endpoint → no runs_on.
	remote := makeTestSource("", "10.0.0.5", 8500, "H")
	remote.setReachable(true, "node-uuid", "1.17.0")
	robs, _ := remote.Observe()
	if _, found := findRelation(robs, "runs_on"); found {
		t.Errorf("remote consul must NOT emit runs_on; relations=%v", relTypes(robs))
	}
}

// TestEntitySource_DescriptiveAttrs: server.address and server.port must be
// present in the entity attributes (not used as identity).
func TestEntitySource_DescriptiveAttrs(t *testing.T) {
	s := makeTestSource("", "consul.example.com", 8501, "host-uuid-1")
	s.setReachable(true, "a1b2c3d4-1234-5678-9abc-def012345678", "1.17.0")

	obs, _ := s.Observe()
	if len(obs.Entities) == 0 {
		t.Fatal("no entities in observation")
	}
	attrs := obs.Entities[0].Attributes
	if v, _ := attrs["service.name"].(string); v != "consul" {
		t.Errorf("service.name = %q, want %q", v, "consul")
	}
	if v, _ := attrs["server.address"].(string); v != "consul.example.com" {
		t.Errorf("server.address = %q, want %q", v, "consul.example.com")
	}
	if v, _ := attrs["server.port"].(int64); v != 8501 {
		t.Errorf("server.port = %v, want 8501", v)
	}
}
