package couchdb

import (
	"testing"

	"senhub-agent.go/internal/agent/services/agentstate"
)

// resetAgentID clears the agent instance id set via agentstate between tests.
func resetAgentID(t *testing.T) {
	t.Helper()
	agentstate.SetAgentInstanceID("")
	t.Cleanup(func() { agentstate.SetAgentInstanceID("") })
}

// TestEntitySource_InstanceNameOverride verifies that when "instance_name" is
// supplied, it is used verbatim as db.instance.id and the entity is emitted
// immediately (no waiting for the server UUID).
func TestEntitySource_InstanceNameOverride(t *testing.T) {
	resetAgentID(t)

	src := newCouchDBEntitySource("http://couchdb.example.com:5984", "prod-couch-01")
	src.setReachable(true)

	obs, ok := src.Observe()
	if !ok {
		t.Fatal("Observe() returned ok=false with instance_name set")
	}
	if len(obs.Entities) != 1 {
		t.Fatalf("got %d entities, want 1", len(obs.Entities))
	}
	e := obs.Entities[0]
	if e.Type != "db" {
		t.Errorf("entity type = %q, want %q", e.Type, "db")
	}
	got, _ := e.ID["db.instance.id"].(string)
	if got != "prod-couch-01" {
		t.Errorf("db.instance.id = %q, want %q", got, "prod-couch-01")
	}
	if e.Attributes["db.system.name"] != "couchdb" {
		t.Errorf("db.system.name = %v, want couchdb", e.Attributes["db.system.name"])
	}
}

// TestEntitySource_TechIDPinned verifies that when no instance_name is set,
// the entity is NOT emitted until the server UUID is pinned via pinServerUUID,
// and that the resulting id is "couchdb:<uuid>".
func TestEntitySource_TechIDPinned(t *testing.T) {
	resetAgentID(t)

	src := newCouchDBEntitySource("http://localhost:5984", "")
	src.setReachable(true)

	// Before pinning: must not emit.
	if _, ok := src.Observe(); ok {
		t.Fatal("Observe() returned ok=true before tech id pinned")
	}

	// Pin the UUID.
	src.pinServerUUID("aabbccdd1122")
	obs, ok := src.Observe()
	if !ok {
		t.Fatal("Observe() returned ok=false after pinServerUUID")
	}
	if len(obs.Entities) != 1 {
		t.Fatalf("got %d entities, want 1", len(obs.Entities))
	}
	got, _ := obs.Entities[0].ID["db.instance.id"].(string)
	want := "couchdb:aabbccdd1122"
	if got != want {
		t.Errorf("db.instance.id = %q, want %q", got, want)
	}
}

// TestEntitySource_NotEmittedBeforeUUID verifies the no-emit guarantee when
// the reachable flag is set but no UUID has been provided yet.
func TestEntitySource_NotEmittedBeforeUUID(t *testing.T) {
	resetAgentID(t)

	src := newCouchDBEntitySource("http://localhost:5984", "")
	src.setReachable(true)

	_, ok := src.Observe()
	if ok {
		t.Error("entity must NOT be emitted before the tech id is pinned")
	}
}

// TestEntitySource_HostPortFallback verifies that if pinServerUUID is called
// with an empty uuid (no stable tech id available), the entity is still not
// emitted — the degraded host:port path requires an explicit pin with a
// non-empty id.  In CouchDB's case the UUID is always available so there is
// no true "fallback", but the source must never emit with an empty id.
func TestEntitySource_EmptyUUIDIsIgnored(t *testing.T) {
	resetAgentID(t)

	src := newCouchDBEntitySource("http://localhost:5984", "")
	src.setReachable(true)

	src.pinServerUUID("") // should be a no-op

	_, ok := src.Observe()
	if ok {
		t.Error("Observe() must return ok=false when pinServerUUID called with empty uuid")
	}
}

// TestEntitySource_PinIsImmutable verifies that a second call to pinServerUUID
// does not overwrite the first pinned id (immutability guarantee).
func TestEntitySource_PinIsImmutable(t *testing.T) {
	resetAgentID(t)

	src := newCouchDBEntitySource("http://localhost:5984", "")
	src.setReachable(true)
	src.pinServerUUID("first-uuid")
	src.pinServerUUID("second-uuid") // must be ignored

	obs, ok := src.Observe()
	if !ok {
		t.Fatal("Observe() returned ok=false after first pin")
	}
	got, _ := obs.Entities[0].ID["db.instance.id"].(string)
	if got != "couchdb:first-uuid" {
		t.Errorf("db.instance.id = %q, want couchdb:first-uuid (immutable)", got)
	}
}

// TestEntitySource_MonitorsEdgePresent verifies that when the agent id is set,
// the Observe result carries a monitors relation from the agent's
// service.instance to the db entity.
func TestEntitySource_MonitorsEdgePresent(t *testing.T) {
	resetAgentID(t)
	agentstate.SetAgentInstanceID("agent-test-01")

	src := newCouchDBEntitySource("http://localhost:5984", "my-couch")
	src.setReachable(true)

	obs, ok := src.Observe()
	if !ok {
		t.Fatal("Observe() returned ok=false")
	}
	if len(obs.Relations) != 1 {
		t.Fatalf("got %d relations, want 1", len(obs.Relations))
	}
	r := obs.Relations[0]
	if r.Type != "monitors" {
		t.Errorf("relation type = %q, want monitors", r.Type)
	}
	if r.FromType != "service.instance" {
		t.Errorf("FromType = %q, want service.instance", r.FromType)
	}
	fromID, _ := r.FromID["service.instance.id"].(string)
	if fromID != "agent-test-01" {
		t.Errorf("FromID service.instance.id = %q, want agent-test-01", fromID)
	}
	if r.ToType != "db" {
		t.Errorf("ToType = %q, want db", r.ToType)
	}
	toID, _ := r.ToID["db.instance.id"].(string)
	if toID != "my-couch" {
		t.Errorf("ToID db.instance.id = %q, want my-couch", toID)
	}
}

// TestEntitySource_MonitorsEdgeAbsentWhenNoAgentID verifies that when the
// agent id is empty (entity emission not started), no monitors relation is
// emitted rather than an unresolvable From endpoint.
func TestEntitySource_MonitorsEdgeAbsentWhenNoAgentID(t *testing.T) {
	resetAgentID(t)
	// agentstate left empty

	src := newCouchDBEntitySource("http://localhost:5984", "my-couch")
	src.setReachable(true)

	obs, ok := src.Observe()
	if !ok {
		t.Fatal("Observe() returned ok=false")
	}
	if len(obs.Relations) != 0 {
		t.Errorf("got %d relations, want 0 when agent id is empty", len(obs.Relations))
	}
}

// TestEntitySource_NotReachableSuppresses verifies that ok=false is returned
// when the source is marked unreachable, regardless of pin state.
func TestEntitySource_NotReachableSuppresses(t *testing.T) {
	resetAgentID(t)

	src := newCouchDBEntitySource("http://localhost:5984", "my-couch")
	src.setReachable(false)

	_, ok := src.Observe()
	if ok {
		t.Error("Observe() must return ok=false when unreachable")
	}
}

// TestEntitySource_DescriptiveAttrs verifies that server.address and
// server.port are present as descriptive attributes (not part of the identity).
func TestEntitySource_DescriptiveAttrs(t *testing.T) {
	resetAgentID(t)

	src := newCouchDBEntitySource("http://couch.example.com:5984", "my-couch")
	src.setReachable(true)

	obs, ok := src.Observe()
	if !ok {
		t.Fatal("Observe() returned ok=false")
	}
	attrs := obs.Entities[0].Attributes
	if attrs["server.address"] != "couch.example.com" {
		t.Errorf("server.address = %v, want couch.example.com", attrs["server.address"])
	}
	if attrs["server.port"] != int64(5984) {
		t.Errorf("server.port = %v, want 5984", attrs["server.port"])
	}
	if _, hasID := obs.Entities[0].ID["server.address"]; hasID {
		t.Error("server.address must be a descriptive attr, not an id key")
	}
}
