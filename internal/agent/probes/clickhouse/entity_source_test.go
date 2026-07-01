package clickhouse

import (
	"testing"

	"senhub-agent.go/internal/agent/services/agentstate"
	"senhub-agent.go/internal/agent/services/entity"
)

// TestEntitySource_InstanceNameOverride verifies that when instance_name is
// set the entity is emitted immediately (on first up=true) with the operator-
// supplied name as db.instance.id, regardless of serverUUID availability.
func TestEntitySource_InstanceNameOverride(t *testing.T) {
	s := newClickhouseEntitySource("my-clickhouse", "http://10.0.0.1:8123")

	// Should be pinned at construction.
	if !s.isPinned() {
		t.Fatal("isPinned() should be true immediately when instance_name is set")
	}

	s.setReachable(true, "")
	obs, ok := s.Observe()
	if !ok {
		t.Fatal("Observe() should return ok=true when up and pinned")
	}
	if len(obs.Entities) != 1 {
		t.Fatalf("expected 1 entity, got %d", len(obs.Entities))
	}
	id, _ := obs.Entities[0].ID["db.instance.id"].(string)
	if id != "my-clickhouse" {
		t.Errorf("db.instance.id = %q, want %q", id, "my-clickhouse")
	}
	if obs.Entities[0].Type != "db" {
		t.Errorf("entity type = %q, want %q", obs.Entities[0].Type, "db")
	}
}

// TestEntitySource_TechIDPinned verifies that a successful pinTechID call
// produces a "clickhouse:<uuid>" id and the entity is emitted after setReachable.
func TestEntitySource_TechIDPinned(t *testing.T) {
	s := newClickhouseEntitySource("", "http://10.0.0.2:8123")

	if s.isPinned() {
		t.Fatal("should not be pinned before first collect")
	}

	// Simulate first successful collect with UUID.
	pinned := s.pinTechID("a1b2c3d4-e5f6-7890-abcd-ef1234567890")
	if pinned != "clickhouse:a1b2c3d4-e5f6-7890-abcd-ef1234567890" {
		t.Errorf("pinned id = %q, want clickhouse:<uuid>", pinned)
	}

	s.setReachable(true, "")
	obs, ok := s.Observe()
	if !ok {
		t.Fatal("Observe() should return ok=true after pinning and setReachable")
	}
	id, _ := obs.Entities[0].ID["db.instance.id"].(string)
	if id != "clickhouse:a1b2c3d4-e5f6-7890-abcd-ef1234567890" {
		t.Errorf("db.instance.id = %q, want clickhouse:<uuid>", id)
	}
}

// TestEntitySource_NotEmittedBeforePin verifies that Observe returns ok=false
// when the server is up but the id has not been pinned yet (instance_name=""
// and no serverUUID fetched yet). This prevents the consumer from keying on a
// transient host:port that would change once the real UUID arrives.
func TestEntitySource_NotEmittedBeforePin(t *testing.T) {
	s := newClickhouseEntitySource("", "http://10.0.0.3:8123")
	s.setReachable(true, "")

	_, ok := s.Observe()
	if ok {
		t.Fatal("Observe() must return ok=false before the id is pinned")
	}
}

// TestEntitySource_HostPortFallback verifies that when no UUID is available
// (pinTechID("") is called), the source pins host:port as a stable fallback
// and immediately emits the entity.
func TestEntitySource_HostPortFallback(t *testing.T) {
	s := newClickhouseEntitySource("", "http://db.example.com:8123")

	pinned := s.pinTechID("") // simulate UUID fetch failure
	if pinned != "db.example.com:8123" {
		t.Errorf("pinned id = %q, want host:port fallback", pinned)
	}

	s.setReachable(true, "")
	obs, ok := s.Observe()
	if !ok {
		t.Fatal("Observe() should return ok=true after fallback pin + up")
	}
	id, _ := obs.Entities[0].ID["db.instance.id"].(string)
	if id != "db.example.com:8123" {
		t.Errorf("db.instance.id = %q, want host:port", id)
	}
}

// TestEntitySource_MonitorsEdgePresent verifies that when an agent instance id
// is set, Observe includes a monitors relation from service.instance to db.
func TestEntitySource_MonitorsEdgePresent(t *testing.T) {
	agentstate.SetAgentInstanceID("agent-001")
	t.Cleanup(func() { agentstate.SetAgentInstanceID("") })

	s := newClickhouseEntitySource("", "http://10.0.0.4:8123")
	s.pinTechID("uuid-001")
	s.setReachable(true, "")

	obs, ok := s.Observe()
	if !ok {
		t.Fatal("Observe() should return ok=true")
	}
	if len(obs.Relations) != 1 {
		t.Fatalf("expected 1 relation, got %d", len(obs.Relations))
	}
	r := obs.Relations[0]
	if r.Type != "monitors" {
		t.Errorf("relation type = %q, want monitors", r.Type)
	}
	if r.FromType != "service.instance" {
		t.Errorf("FromType = %q, want service.instance", r.FromType)
	}
	if r.FromID["service.instance.id"] != "agent-001" {
		t.Errorf("FromID[service.instance.id] = %v, want agent-001", r.FromID["service.instance.id"])
	}
	if r.ToType != "db" {
		t.Errorf("ToType = %q, want db", r.ToType)
	}
	if r.ToID["db.instance.id"] != "clickhouse:uuid-001" {
		t.Errorf("ToID[db.instance.id] = %v, want clickhouse:uuid-001", r.ToID["db.instance.id"])
	}
}

// TestEntitySource_MonitorsEdgeAbsentWhenNoAgentID verifies that no monitors
// relation is emitted when the agent instance id is empty.
func TestEntitySource_MonitorsEdgeAbsentWhenNoAgentID(t *testing.T) {
	agentstate.SetAgentInstanceID("")

	s := newClickhouseEntitySource("", "http://10.0.0.5:8123")
	s.pinTechID("uuid-002")
	s.setReachable(true, "")

	obs, ok := s.Observe()
	if !ok {
		t.Fatal("Observe() should return ok=true")
	}
	if len(obs.Relations) != 0 {
		t.Errorf("expected no relations when agent id is empty, got %d", len(obs.Relations))
	}
}

// TestEntitySource_DescriptiveAttrs verifies that server.address and
// server.port are present as descriptive attributes (not part of the id).
func TestEntitySource_DescriptiveAttrs(t *testing.T) {
	s := newClickhouseEntitySource("", "http://ch-host:9000")
	s.pinTechID("some-uuid")
	s.setReachable(true, "1.2.3")

	obs, ok := s.Observe()
	if !ok {
		t.Fatal("Observe() should return ok=true")
	}
	attrs := obs.Entities[0].Attributes

	checks := []struct {
		key  string
		want any
	}{
		{"db.system.name", "clickhouse"},
		{"server.address", "ch-host"},
		{"server.port", int64(9000)},
		{"db.system.version", "1.2.3"},
	}
	for _, c := range checks {
		if attrs[c.key] != c.want {
			t.Errorf("attrs[%q] = %v (%T), want %v (%T)", c.key, attrs[c.key], attrs[c.key], c.want, c.want)
		}
	}
	// Descriptive attrs must NOT appear in the ID.
	id := obs.Entities[0].ID
	for k := range id {
		if k != "db.instance.id" {
			t.Errorf("unexpected key in entity ID: %q", k)
		}
	}
}

// TestEntitySource_PinImmutable verifies that once the id is pinned, a second
// pinTechID call (even with a different value) does not change it.
func TestEntitySource_PinImmutable(t *testing.T) {
	s := newClickhouseEntitySource("", "http://10.0.0.6:8123")
	first := s.pinTechID("uuid-first")
	second := s.pinTechID("uuid-second")
	if first != second {
		t.Errorf("second pin changed id: %q → %q (must be immutable)", first, second)
	}
}

// TestEntitySource_RelationToIDMatchesEntityID verifies that the relation
// ToID matches the entity's own ID — the consumer must resolve them to the
// same node.
func TestEntitySource_RelationToIDMatchesEntityID(t *testing.T) {
	agentstate.SetAgentInstanceID("agent-check")
	t.Cleanup(func() { agentstate.SetAgentInstanceID("") })

	s := newClickhouseEntitySource("named-instance", "http://10.0.0.7:8123")
	s.setReachable(true, "")

	obs, ok := s.Observe()
	if !ok {
		t.Fatal("Observe() should return ok=true")
	}
	if len(obs.Entities) == 0 || len(obs.Relations) == 0 {
		t.Fatal("expected entity and relation")
	}
	entityID := obs.Entities[0].ID
	relToID := obs.Relations[0].ToID

	if !mapsEqual(entityID, relToID) {
		t.Errorf("entity ID %v != relation ToID %v", entityID, relToID)
	}
}

// TestEntitySource_LocalDBRunsOnHost: a loopback-reachable clickhouse with a
// globally-unique tech id is anchored to the host with runs_on (enterprise#36);
// a remote db is not.
func TestEntitySource_LocalDBRunsOnHost(t *testing.T) {
	runsOn := func(endpoint string) bool {
		s := newClickhouseEntitySource("", endpoint)
		s.hostID = func() string { return "h-1" }
		s.pinTechID("a1b2c3d4-uuid")
		s.setReachable(true, "")
		obs, _ := s.Observe()
		for _, r := range obs.Relations {
			if r.Type == "runs_on" && r.FromType == "db" && r.ToID["host.id"] == "h-1" {
				return true
			}
		}
		return false
	}
	if !runsOn("http://127.0.0.1:8123") {
		t.Error("loopback db with a tech id must emit runs_on→host")
	}
	if runsOn("http://10.0.0.5:8123") {
		t.Error("remote db must NOT emit runs_on→host")
	}
}

func mapsEqual(a, b map[string]any) bool {
	if len(a) != len(b) {
		return false
	}
	for k, v := range a {
		if b[k] != v {
			return false
		}
	}
	return true
}

// Ensure entity.Relation type fields are used correctly (compile-time check).
var _ entity.Relation = entity.Relation{}
