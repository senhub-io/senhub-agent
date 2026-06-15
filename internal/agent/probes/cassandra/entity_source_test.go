package cassandra

import (
	"net/http/httptest"
	"testing"

	"senhub-agent.go/internal/agent/services/agentstate"
)

// TestEntitySource_InstanceNameOverride verifies that when instance_name is
// set in config, the entity is emitted immediately with the verbatim value
// (no tech id fetch required, ok=true from the start).
func TestEntitySource_InstanceNameOverride(t *testing.T) {
	s := newEntitySource("my-cassandra", "127.0.0.1", "9042")

	obs, ok := s.Observe()
	if !ok {
		t.Fatal("Observe() ok=false with instance_name override; want true immediately")
	}
	if len(obs.Entities) != 1 {
		t.Fatalf("got %d entities, want 1", len(obs.Entities))
	}
	id := obs.Entities[0].ID["db.instance.id"]
	if id != "my-cassandra" {
		t.Errorf("db.instance.id = %q, want %q", id, "my-cassandra")
	}
}

// TestEntitySource_TechIDPinned verifies the tech-id path: before any update,
// ok=false; after the first successful update with a non-empty host_id,
// ok=true and db.instance.id = "cassandra:<host_id>".
func TestEntitySource_TechIDPinned(t *testing.T) {
	s := newEntitySource("", "127.0.0.1", "9042")

	// Before any successful collect: not ready.
	if _, ok := s.Observe(); ok {
		t.Fatal("Observe() ok=true before tech id is fetched; want false")
	}

	// Simulate a successful collect with a tech id.
	s.update("cassandra:a1b2c3d4-uuid", "127.0.0.1", "9042", true, "4.0.0")

	obs, ok := s.Observe()
	if !ok {
		t.Fatal("Observe() ok=false after tech id pinned; want true")
	}
	if len(obs.Entities) != 1 {
		t.Fatalf("got %d entities, want 1", len(obs.Entities))
	}
	id := obs.Entities[0].ID["db.instance.id"]
	if id != "cassandra:a1b2c3d4-uuid" {
		t.Errorf("db.instance.id = %q, want %q", id, "cassandra:a1b2c3d4-uuid")
	}
}

// TestEntitySource_NotEmittedBeforeTechIDPinned verifies that with no
// instance_name override, Observe() returns ok=false even after an up=true
// update that carries no tech id yet (lazy resolution path).
func TestEntitySource_NotEmittedBeforeTechIDPinned(t *testing.T) {
	s := newEntitySource("", "127.0.0.1", "9042")

	// up=true but no tech id — entity must not be emitted.
	s.update("", "127.0.0.1", "9042", true, "4.0.0")

	if _, ok := s.Observe(); ok {
		t.Fatal("Observe() ok=true when tech id not yet resolved; want false")
	}
}

// TestEntitySource_HostPortFallbackNotApplied verifies that for the cassandra
// probe (which always has a resolvable tech id via LocalHostId), the host:port
// fallback is never auto-applied: without an explicit instance_name and before
// a tech id arrives, the entity stays suppressed (ok=false), not emitting a
// host:port id that would later re-key the entity when the real id arrives.
func TestEntitySource_HostPortFallbackNotApplied(t *testing.T) {
	s := newEntitySource("", "somehost", "9042")

	// Several failed collects — must still be suppressed, not fall back to host:port.
	s.update("", "somehost", "9042", false, "")
	s.update("", "somehost", "9042", false, "")

	if _, ok := s.Observe(); ok {
		t.Fatal("Observe() ok=true with no pinned id; want false to avoid host:port re-keying")
	}
}

// TestEntitySource_IDImmutableAfterPin verifies that a second update with a
// different tech id does NOT change the pinned id (immutability contract).
func TestEntitySource_IDImmutableAfterPin(t *testing.T) {
	s := newEntitySource("", "127.0.0.1", "9042")

	s.update("cassandra:uuid-first", "127.0.0.1", "9042", true, "4.0.0")
	s.update("cassandra:uuid-second", "127.0.0.1", "9042", true, "4.0.1")

	obs, ok := s.Observe()
	if !ok {
		t.Fatal("Observe() ok=false after two updates; want true")
	}
	id := obs.Entities[0].ID["db.instance.id"]
	if id != "cassandra:uuid-first" {
		t.Errorf("db.instance.id changed after pin; got %q, want %q", id, "cassandra:uuid-first")
	}
}

// TestEntitySource_DownSetsObsFalse verifies that an up=false update sets
// ok=false so the detector keeps the last-good snapshot (D3 audit contract).
func TestEntitySource_DownSetsObsFalse(t *testing.T) {
	s := newEntitySource("", "127.0.0.1", "9042")

	// Pin the tech id first.
	s.update("cassandra:uuid", "127.0.0.1", "9042", true, "4.0.0")
	if _, ok := s.Observe(); !ok {
		t.Fatal("precondition: want ok=true after pin")
	}

	// Node goes down.
	s.update("", "127.0.0.1", "9042", false, "")

	if _, ok := s.Observe(); ok {
		t.Fatal("Observe() ok=true after down; want false so detector keeps last-good snapshot")
	}
}

// TestEntitySource_DescriptiveAttrs verifies server.address, server.port, and
// db.system.name are present as descriptive attributes (not identity fields).
func TestEntitySource_DescriptiveAttrs(t *testing.T) {
	s := newEntitySource("my-cass", "cassandra.example.com", "9042")

	// Trigger a rebuild with descriptive attrs by calling update.
	s.update("", "cassandra.example.com", "9042", true, "4.1.0")

	obs, ok := s.Observe()
	if !ok {
		t.Fatal("Observe() ok=false; want true with instance_name override")
	}
	if len(obs.Entities) != 1 {
		t.Fatalf("got %d entities, want 1", len(obs.Entities))
	}
	attrs := obs.Entities[0].Attributes
	if attrs["server.address"] != "cassandra.example.com" {
		t.Errorf("server.address = %v, want %q", attrs["server.address"], "cassandra.example.com")
	}
	if attrs["server.port"] != "9042" {
		t.Errorf("server.port = %v, want %q", attrs["server.port"], "9042")
	}
	if attrs["db.system.name"] != "cassandra" {
		t.Errorf("db.system.name = %v, want %q", attrs["db.system.name"], "cassandra")
	}
	if attrs["db.system.version"] != "4.1.0" {
		t.Errorf("db.system.version = %v, want %q", attrs["db.system.version"], "4.1.0")
	}
}

// TestEntitySource_MonitorsEdgePresent verifies the monitors edge is included
// when agentstate.GetAgentInstanceID() returns a non-empty value.
func TestEntitySource_MonitorsEdgePresent(t *testing.T) {
	agentstate.SetAgentInstanceID("agent-instance-1")
	t.Cleanup(func() { agentstate.SetAgentInstanceID("") })

	s := newEntitySource("my-cass", "127.0.0.1", "9042")

	obs, ok := s.Observe()
	if !ok {
		t.Fatal("Observe() ok=false; want true with instance_name override")
	}

	if len(obs.Relations) != 1 {
		t.Fatalf("got %d relations, want 1 (monitors edge)", len(obs.Relations))
	}
	rel := obs.Relations[0]
	if rel.Type != "monitors" {
		t.Errorf("relation.Type = %q, want %q", rel.Type, "monitors")
	}
	if rel.FromType != "service.instance" {
		t.Errorf("relation.FromType = %q, want %q", rel.FromType, "service.instance")
	}
	if rel.FromID["service.instance.id"] != "agent-instance-1" {
		t.Errorf("relation.FromID = %v, want service.instance.id=agent-instance-1", rel.FromID)
	}
	if rel.ToType != "db" {
		t.Errorf("relation.ToType = %q, want %q", rel.ToType, "db")
	}
	if rel.ToID["db.instance.id"] != "my-cass" {
		t.Errorf("relation.ToID = %v, want db.instance.id=my-cass", rel.ToID)
	}
}

// TestEntitySource_MonitorsEdgeAbsentWhenNoAgentID verifies that when the
// agent id is not yet set (entity emission disabled or not started), the
// monitors edge is omitted rather than emitting a relation with an empty From.
func TestEntitySource_MonitorsEdgeAbsentWhenNoAgentID(t *testing.T) {
	agentstate.SetAgentInstanceID("")

	s := newEntitySource("my-cass", "127.0.0.1", "9042")

	obs, ok := s.Observe()
	if !ok {
		t.Fatal("Observe() ok=false; want true with instance_name override")
	}
	if len(obs.Relations) != 0 {
		t.Errorf("got %d relations, want 0 when agent id is empty", len(obs.Relations))
	}
}

// TestEntitySource_CollectIntegration verifies the end-to-end path: after a
// Collect cycle with a working fixture that includes LocalHostId, the entity
// source is pinned and Observe() returns a valid db entity with the tech id.
func TestEntitySource_CollectIntegration(t *testing.T) {
	fix := defaultFixture()
	srv := httptest.NewServer(fix.handler())
	defer srv.Close()

	p := newTestProbe(t, srv.URL+"/jolokia")
	p.SetName("cassandra-entity-test")

	_, err := p.Collect()
	if err != nil {
		t.Fatalf("Collect() error = %v", err)
	}

	obs, ok := p.entitySrc.Observe()
	if !ok {
		t.Fatal("entitySrc.Observe() ok=false after successful Collect; want true")
	}
	if len(obs.Entities) == 0 {
		t.Fatal("entitySrc.Observe() returned no entities")
	}
	id := obs.Entities[0].ID["db.instance.id"]
	wantID := "cassandra:a1b2c3d4-e5f6-7890-abcd-ef1234567890"
	if id != wantID {
		t.Errorf("db.instance.id = %q, want %q", id, wantID)
	}
}
