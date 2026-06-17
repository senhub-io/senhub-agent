package memcached

import (
	"testing"

	"senhub-agent.go/internal/agent/services/agentstate"
	"senhub-agent.go/internal/agent/services/entity"
)

// TestEntitySource_InstanceNameOverride verifies that when instance_name is set
// it becomes the db.instance.id verbatim (not host:port).
func TestEntitySource_InstanceNameOverride(t *testing.T) {
	src := newMemcachedEntitySource("myhost", 11211, "prod-memcached")
	if src.instanceID != "prod-memcached" {
		t.Errorf("instanceID = %q, want %q", src.instanceID, "prod-memcached")
	}
}

// TestEntitySource_HostPortFallback verifies that when instance_name is empty
// host:port is used as db.instance.id (the documented db degraded fallback for
// engines with no stable server-reported id).
func TestEntitySource_HostPortFallback(t *testing.T) {
	src := newMemcachedEntitySource("10.0.0.1", 11211, "")
	if src.instanceID != "10.0.0.1:11211" {
		t.Errorf("instanceID = %q, want %q", src.instanceID, "10.0.0.1:11211")
	}
}

// TestEntitySource_NotReadyBeforeFirstCollect verifies ok=false before any
// setReachable call (the detector must not treat the empty cache as "server gone").
func TestEntitySource_NotReadyBeforeFirstCollect(t *testing.T) {
	src := newMemcachedEntitySource("localhost", 11211, "")
	_, ok := src.Observe()
	if ok {
		t.Error("Observe() ok=true before first collect; want ok=false")
	}
}

// TestEntitySource_EmptyObservationWhenDown verifies that ok=true but no
// entities are emitted when the server is unreachable after the first collect.
func TestEntitySource_EmptyObservationWhenDown(t *testing.T) {
	src := newMemcachedEntitySource("localhost", 11211, "")
	src.setReachable(false, "")
	obs, ok := src.Observe()
	if !ok {
		t.Error("Observe() ok=false after first collect; want ok=true")
	}
	if len(obs.Entities) != 0 {
		t.Errorf("Observe() returned %d entities when down, want 0", len(obs.Entities))
	}
	if len(obs.Relations) != 0 {
		t.Errorf("Observe() returned %d relations when down, want 0", len(obs.Relations))
	}
}

// TestEntitySource_EntityEmittedWhenUp verifies the db entity is present and
// carries the expected identity and descriptive attributes.
func TestEntitySource_EntityEmittedWhenUp(t *testing.T) {
	src := newMemcachedEntitySource("10.0.0.2", 11211, "")
	src.setReachable(true, "1.6.22")
	obs, ok := src.Observe()
	if !ok {
		t.Fatal("Observe() ok=false after successful collect")
	}
	if len(obs.Entities) != 1 {
		t.Fatalf("Observe() returned %d entities, want 1", len(obs.Entities))
	}

	ent := obs.Entities[0]
	if ent.Type != "db" {
		t.Errorf("entity.Type = %q, want %q", ent.Type, "db")
	}
	if v, _ := ent.ID["db.instance.id"].(string); v != "10.0.0.2:11211" {
		t.Errorf("db.instance.id = %q, want %q", v, "10.0.0.2:11211")
	}
	if v, _ := ent.Attributes["db.system.name"].(string); v != "memcached" {
		t.Errorf("db.system.name = %q, want %q", v, "memcached")
	}
	if v, _ := ent.Attributes["server.address"].(string); v != "10.0.0.2" {
		t.Errorf("server.address = %q, want %q", v, "10.0.0.2")
	}
	if v, _ := ent.Attributes["server.port"].(int64); v != 11211 {
		t.Errorf("server.port = %d, want %d", v, 11211)
	}
}

// TestEntitySource_InstanceNameInEntityID verifies that when instance_name is
// set it is used as db.instance.id in the emitted entity.
func TestEntitySource_InstanceNameInEntityID(t *testing.T) {
	src := newMemcachedEntitySource("10.0.0.2", 11211, "my-cache")
	src.setReachable(true, "1.6.22")
	obs, ok := src.Observe()
	if !ok {
		t.Fatal("Observe() ok=false after successful collect")
	}
	if len(obs.Entities) != 1 {
		t.Fatalf("Observe() returned %d entities, want 1", len(obs.Entities))
	}
	if v, _ := obs.Entities[0].ID["db.instance.id"].(string); v != "my-cache" {
		t.Errorf("db.instance.id = %q, want %q", v, "my-cache")
	}
}

// TestEntitySource_MonitorsEdgePresent verifies that a monitors relation is
// emitted when the agent instance id is set.
func TestEntitySource_MonitorsEdgePresent(t *testing.T) {
	agentstate.SetAgentInstanceID("test-agent-001")
	t.Cleanup(func() { agentstate.SetAgentInstanceID("") })

	src := newMemcachedEntitySource("localhost", 11211, "")
	src.setReachable(true, "1.6.22")

	obs, ok := src.Observe()
	if !ok {
		t.Fatal("Observe() ok=false after successful collect")
	}

	if len(obs.Relations) != 1 {
		t.Fatalf("Observe() returned %d relations, want 1", len(obs.Relations))
	}
	rel := obs.Relations[0]
	if rel.Type != "monitors" {
		t.Errorf("relation.Type = %q, want %q", rel.Type, "monitors")
	}
	if rel.FromType != "service.instance" {
		t.Errorf("relation.FromType = %q, want %q", rel.FromType, "service.instance")
	}
	if v, _ := rel.FromID["service.instance.id"].(string); v != "test-agent-001" {
		t.Errorf("relation.FromID[service.instance.id] = %q, want %q", v, "test-agent-001")
	}
	if rel.ToType != "db" {
		t.Errorf("relation.ToType = %q, want %q", rel.ToType, "db")
	}
	if v, _ := rel.ToID["db.instance.id"].(string); v != "localhost:11211" {
		t.Errorf("relation.ToID[db.instance.id] = %q, want %q", v, "localhost:11211")
	}
}

// TestEntitySource_MonitorsEdgeAbsentWhenNoAgentID verifies that no relation
// is emitted when the agent instance id is not set.
func TestEntitySource_MonitorsEdgeAbsentWhenNoAgentID(t *testing.T) {
	// Ensure agent id is unset for this test.
	agentstate.SetAgentInstanceID("")
	t.Cleanup(func() { agentstate.SetAgentInstanceID("") })

	src := newMemcachedEntitySource("localhost", 11211, "")
	src.setReachable(true, "1.6.22")

	obs, ok := src.Observe()
	if !ok {
		t.Fatal("Observe() ok=false after successful collect")
	}
	if len(obs.Relations) != 0 {
		t.Errorf("Observe() returned %d relations with no agent id, want 0", len(obs.Relations))
	}
}

// TestEntitySource_IDImmutableAfterPinned verifies that calling setReachable
// multiple times does not change instanceID (immutability contract).
func TestEntitySource_IDImmutableAfterPinned(t *testing.T) {
	src := newMemcachedEntitySource("host1", 11211, "")
	want := src.instanceID

	src.setReachable(true, "1.6.0")
	src.setReachable(true, "1.6.22")
	src.setReachable(false, "")
	src.setReachable(true, "1.7.0")

	if src.instanceID != want {
		t.Errorf("instanceID changed: got %q, want %q", src.instanceID, want)
	}
}

// TestEntitySource_FoldedRelationship verifies that when a monitors relation is
// present it can be folded via the entity framework's foldRelationships, i.e.
// the agent entity is NOT emitted by this source — only the db entity whose
// FromType matches the relation's source endpoint would be needed, but the
// relation is From:service.instance (the agent, emitted elsewhere) To:db. The
// fold resolves it only when the agent entity is present in the combined
// observation; orphan behaviour is tested here: the relation IS an orphan from
// the memcached source alone (the agent entity comes from the entity foundation,
// not here). We just verify the relation is well-formed.
func TestEntitySource_RelationWellFormed(t *testing.T) {
	agentstate.SetAgentInstanceID("agt-42")
	t.Cleanup(func() { agentstate.SetAgentInstanceID("") })

	src := newMemcachedEntitySource("db.local", 11211, "named-cache")
	src.setReachable(true, "1.6.22")

	obs, _ := src.Observe()
	if len(obs.Relations) != 1 {
		t.Fatalf("want 1 relation, got %d", len(obs.Relations))
	}
	r := obs.Relations[0]
	// Verify the relation endpoint types match the entity model.
	_ = entity.Relation{
		Type:     r.Type,
		FromType: r.FromType,
		FromID:   r.FromID,
		ToType:   r.ToType,
		ToID:     r.ToID,
	}
	if r.ToID["db.instance.id"] != "named-cache" {
		t.Errorf("ToID[db.instance.id] = %v, want %q", r.ToID["db.instance.id"], "named-cache")
	}
}
