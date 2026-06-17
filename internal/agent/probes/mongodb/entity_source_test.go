package mongodb

import (
	"context"
	"testing"

	"senhub-agent.go/internal/agent/services/agentstate"
	"senhub-agent.go/internal/agent/services/entity"
)

// ─────────────────────────────────────────────────────────────────────────────
// entity source — identity resolution
// ─────────────────────────────────────────────────────────────────────────────

// TestEntitySource_InstanceNameOverride verifies that when instance_name is
// set the entity is emitted immediately with that exact id and not the
// host:port or a tech id.
func TestEntitySource_InstanceNameOverride(t *testing.T) {
	src := newMongodbEntitySource("mongo.example.com", 27017, "my-mongo")

	// Instance name → pinned at construction; reachable immediately.
	src.setReachable(true, "7.0.1")

	obs, ok := src.Observe()
	if !ok {
		t.Fatal("Observe() returned ok=false; expected entity emission with instance_name")
	}
	if len(obs.Entities) != 1 {
		t.Fatalf("expected 1 entity, got %d", len(obs.Entities))
	}
	got := obs.Entities[0].ID["db.instance.id"]
	if got != "my-mongo" {
		t.Errorf("db.instance.id = %q, want %q", got, "my-mongo")
	}
	if obs.Entities[0].Type != "db" {
		t.Errorf("entity type = %q, want db", obs.Entities[0].Type)
	}
}

// TestEntitySource_TechIDPinned verifies the replica-set tech id path:
//   - before pinning: Observe() returns ok=false (entity not emitted).
//   - after pinTechID: the entity is emitted with the correct id.
//   - a second pinTechID call is a no-op (first pin wins).
func TestEntitySource_TechIDPinned(t *testing.T) {
	src := newMongodbEntitySource("mongo.example.com", 27017, "")

	// Not yet pinned: entity must not be emitted even when reachable.
	src.setReachable(true, "7.0.0")
	if _, ok := src.Observe(); ok {
		t.Fatal("Observe() returned ok=true before tech id is pinned; entity must be suppressed")
	}

	// Pin the tech id (replica set).
	src.pinTechID("mongodb:rs0/mongo.example.com:27017")
	obs, ok := src.Observe()
	if !ok {
		t.Fatal("Observe() returned ok=false after tech id pinned")
	}
	if len(obs.Entities) != 1 {
		t.Fatalf("expected 1 entity, got %d", len(obs.Entities))
	}
	got := obs.Entities[0].ID["db.instance.id"]
	const want = "mongodb:rs0/mongo.example.com:27017"
	if got != want {
		t.Errorf("db.instance.id = %q, want %q", got, want)
	}

	// Second pin must not override the first.
	src.pinTechID("mongodb:rs0/other:27017")
	obs2, _ := src.Observe()
	if obs2.Entities[0].ID["db.instance.id"] != want {
		t.Errorf("second pinTechID changed id to %q; first pin must win", obs2.Entities[0].ID["db.instance.id"])
	}
}

// TestEntitySource_NotEmittedBeforePinWhenNoInstanceName verifies that for
// the tech-id path (no instance_name), Observe() returns ok=false before the
// first successful collect pins the id, even when the probe is reachable.
// This prevents emitting host:port first then re-keying to the real id.
func TestEntitySource_NotEmittedBeforePinWhenNoInstanceName(t *testing.T) {
	src := newMongodbEntitySource("mongo.example.com", 27017, "")
	src.setReachable(true, "7.0.0")

	_, ok := src.Observe()
	if ok {
		t.Error("Observe() must return ok=false before the id is pinned (tech id path)")
	}
}

// TestEntitySource_HostPortFallback verifies that pinHostPort pins host:port
// as db.instance.id and the entity is emitted immediately after.
// This covers standalone MongoDB where replSetGetStatus is unavailable.
func TestEntitySource_HostPortFallback(t *testing.T) {
	src := newMongodbEntitySource("mongo.example.com", 27017, "")
	src.setReachable(true, "7.0.0")

	src.pinHostPort()

	obs, ok := src.Observe()
	if !ok {
		t.Fatal("Observe() returned ok=false after host:port fallback pin")
	}
	if len(obs.Entities) != 1 {
		t.Fatalf("expected 1 entity, got %d", len(obs.Entities))
	}
	got := obs.Entities[0].ID["db.instance.id"]
	const want = "mongo.example.com:27017"
	if got != want {
		t.Errorf("db.instance.id = %q, want %q (host:port fallback)", got, want)
	}
}

// TestEntitySource_HostPortFallbackIsPinnedOnce verifies that pinHostPort is
// idempotent — a second call (e.g. if Collect is called again) must not
// override an already-pinned id.
func TestEntitySource_HostPortFallbackIsPinnedOnce(t *testing.T) {
	src := newMongodbEntitySource("mongo.example.com", 27017, "")
	src.setReachable(true, "7.0.0")
	src.pinTechID("mongodb:rs0/mongo.example.com:27017")

	// Now try to pin host:port — must be rejected because id is already pinned.
	src.pinHostPort()

	obs, _ := src.Observe()
	got := obs.Entities[0].ID["db.instance.id"]
	if got == "mongo.example.com:27017" {
		t.Error("pinHostPort must not override an already-pinned tech id")
	}
}

// TestEntitySource_NotEmittedWhenUnreachable verifies that Observe() returns
// ok=false when the probe is unreachable, even after the id is pinned.
func TestEntitySource_NotEmittedWhenUnreachable(t *testing.T) {
	src := newMongodbEntitySource("mongo.example.com", 27017, "my-mongo")
	// instance_name → pinned at construction, but reachable=false.
	src.setReachable(false, "")

	if _, ok := src.Observe(); ok {
		t.Error("Observe() must return ok=false when MongoDB is unreachable")
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// entity source — descriptive attributes
// ─────────────────────────────────────────────────────────────────────────────

// TestEntitySource_DescriptiveAttrs verifies that the entity carries
// db.system.name, server.address, and server.port as descriptive attributes.
func TestEntitySource_DescriptiveAttrs(t *testing.T) {
	src := newMongodbEntitySource("mongo.example.com", 27017, "my-mongo")
	src.setReachable(true, "7.0.1")

	obs, ok := src.Observe()
	if !ok {
		t.Fatal("Observe() returned ok=false")
	}
	attrs := obs.Entities[0].Attributes
	if attrs["db.system.name"] != "mongodb" {
		t.Errorf("db.system.name = %q, want mongodb", attrs["db.system.name"])
	}
	if attrs["server.address"] != "mongo.example.com" {
		t.Errorf("server.address = %q, want mongo.example.com", attrs["server.address"])
	}
	if attrs["server.port"] != int64(27017) {
		t.Errorf("server.port = %v, want 27017", attrs["server.port"])
	}
	if attrs["db.system.version"] != "7.0.1" {
		t.Errorf("db.system.version = %q, want 7.0.1", attrs["db.system.version"])
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// entity source — monitors edge
// ─────────────────────────────────────────────────────────────────────────────

// TestEntitySource_MonitorsEdge_Present verifies that when agentstate has an
// agent instance id, a monitors relation is included in the observation.
func TestEntitySource_MonitorsEdge_Present(t *testing.T) {
	agentstate.SetAgentInstanceID("test-agent-id-1")
	t.Cleanup(func() { agentstate.SetAgentInstanceID("") })

	src := newMongodbEntitySource("mongo.example.com", 27017, "my-mongo")
	src.setReachable(true, "")

	obs, ok := src.Observe()
	if !ok {
		t.Fatal("Observe() returned ok=false")
	}
	if len(obs.Relations) != 1 {
		t.Fatalf("expected 1 relation (monitors edge), got %d", len(obs.Relations))
	}
	rel := obs.Relations[0]
	if rel.Type != "monitors" {
		t.Errorf("relation.Type = %q, want monitors", rel.Type)
	}
	if rel.FromType != "service.instance" {
		t.Errorf("relation.FromType = %q, want service.instance", rel.FromType)
	}
	if rel.FromID["service.instance.id"] != "test-agent-id-1" {
		t.Errorf("relation.FromID.service.instance.id = %q, want test-agent-id-1", rel.FromID["service.instance.id"])
	}
	if rel.ToType != "db" {
		t.Errorf("relation.ToType = %q, want db", rel.ToType)
	}
	if rel.ToID["db.instance.id"] != "my-mongo" {
		t.Errorf("relation.ToID.db.instance.id = %q, want my-mongo", rel.ToID["db.instance.id"])
	}
}

// TestEntitySource_MonitorsEdge_AbsentWhenNoAgentID verifies that the monitors
// relation is omitted when the agent instance id is not set.
func TestEntitySource_MonitorsEdge_AbsentWhenNoAgentID(t *testing.T) {
	agentstate.SetAgentInstanceID("")

	src := newMongodbEntitySource("mongo.example.com", 27017, "my-mongo")
	src.setReachable(true, "")

	obs, ok := src.Observe()
	if !ok {
		t.Fatal("Observe() returned ok=false")
	}
	if len(obs.Relations) != 0 {
		t.Errorf("expected 0 relations when agent id is empty, got %d", len(obs.Relations))
	}
}

// TestEntitySource_MonitorsEdge_ToIDMatchesPinnedID verifies that the monitors
// edge ToID always uses the same pinned id as the entity itself.
func TestEntitySource_MonitorsEdge_ToIDMatchesPinnedID(t *testing.T) {
	agentstate.SetAgentInstanceID("agent-42")
	t.Cleanup(func() { agentstate.SetAgentInstanceID("") })

	src := newMongodbEntitySource("mongo.example.com", 27017, "")
	src.setReachable(true, "")
	src.pinTechID("mongodb:rs0/mongo.example.com:27017")

	obs, ok := src.Observe()
	if !ok {
		t.Fatal("Observe() returned ok=false")
	}
	entityID := obs.Entities[0].ID["db.instance.id"]
	if len(obs.Relations) == 0 {
		t.Fatal("expected monitors relation, got none")
	}
	edgeToID := obs.Relations[0].ToID["db.instance.id"]
	if entityID != edgeToID {
		t.Errorf("monitors edge ToID %q does not match entity id %q", edgeToID, entityID)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// config — instance_name parsing
// ─────────────────────────────────────────────────────────────────────────────

func TestParseConfig_InstanceName(t *testing.T) {
	cfg, err := parseConfig(map[string]interface{}{
		"instance_name": "prod-mongo-primary",
	})
	if err != nil {
		t.Fatalf("parseConfig: %v", err)
	}
	if cfg.InstanceName != "prod-mongo-primary" {
		t.Errorf("InstanceName = %q, want prod-mongo-primary", cfg.InstanceName)
	}
}

func TestParseConfig_InstanceNameDefaultEmpty(t *testing.T) {
	cfg, err := parseConfig(map[string]interface{}{})
	if err != nil {
		t.Fatalf("parseConfig: %v", err)
	}
	if cfg.InstanceName != "" {
		t.Errorf("InstanceName should default to empty, got %q", cfg.InstanceName)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// itoa helper
// ─────────────────────────────────────────────────────────────────────────────

func TestItoa(t *testing.T) {
	cases := []struct {
		in   int64
		want string
	}{
		{0, "0"},
		{27017, "27017"},
		{-1, "-1"},
		{9223372036854775807, "9223372036854775807"},
	}
	for _, tc := range cases {
		got := itoa(tc.in)
		if got != tc.want {
			t.Errorf("itoa(%d) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Observe shape — entity.Source contract
// ─────────────────────────────────────────────────────────────────────────────

// TestEntitySource_ImplementsSource verifies the interface is satisfied at
// compile time (no runtime check needed; this is a static assertion via use).
func TestEntitySource_ImplementsSource(_ *testing.T) {
	var _ entity.Source = (*mongodbEntitySource)(nil)
}

// ─────────────────────────────────────────────────────────────────────────────
// maybeResolveEntityID — injected fetchReplSetID stub
// ─────────────────────────────────────────────────────────────────────────────

// stubFetch returns a fetchReplSetID func that always returns (id, nil).
func stubFetch(id string) func(context.Context) (string, error) {
	return func(context.Context) (string, error) { return id, nil }
}

// TestMaybeResolveEntityID_ReplicaSet verifies that when fetchReplSetID
// returns a non-empty tech id, it is pinned as db.instance.id.
func TestMaybeResolveEntityID_ReplicaSet(t *testing.T) {
	p := newProbeForTest(t)
	p.fetchReplSetID = stubFetch("mongodb:rs0/mongo.example.com:27017")

	p.entitySrc.setReachable(true, "7.0.0")
	p.maybeResolveEntityID()

	obs, ok := p.entitySrc.Observe()
	if !ok {
		t.Fatal("Observe() returned ok=false after replica-set id pinned")
	}
	got := obs.Entities[0].ID["db.instance.id"]
	const want = "mongodb:rs0/mongo.example.com:27017"
	if got != want {
		t.Errorf("db.instance.id = %q, want %q", got, want)
	}
}

// TestMaybeResolveEntityID_Standalone verifies that when fetchReplSetID
// returns an empty tech id (standalone MongoDB), host:port is pinned as
// db.instance.id.
func TestMaybeResolveEntityID_Standalone(t *testing.T) {
	p := newProbeForTest(t)
	p.fetchReplSetID = stubFetch("") // standalone: no replSet, no error.

	p.entitySrc.setReachable(true, "7.0.0")
	p.maybeResolveEntityID()

	obs, ok := p.entitySrc.Observe()
	if !ok {
		t.Fatal("Observe() returned ok=false after host:port fallback")
	}
	got := obs.Entities[0].ID["db.instance.id"]
	// newProbeForTest uses uri "mongodb://localhost:27017".
	const want = "localhost:27017"
	if got != want {
		t.Errorf("db.instance.id = %q, want %q (host:port fallback)", got, want)
	}
}

// TestMaybeResolveEntityID_TransportErrorDeferredTechID verifies that when
// fetchReplSetID returns an error, neither host:port nor tech-id is pinned.
// The entity must not be emitted (ok=false) so the next cycle retries.
func TestMaybeResolveEntityID_TransportErrorDeferredTechID(t *testing.T) {
	p := newProbeForTest(t)
	p.fetchReplSetID = func(context.Context) (string, error) {
		return "", context.DeadlineExceeded
	}

	p.entitySrc.setReachable(true, "7.0.0")
	p.maybeResolveEntityID()

	if _, ok := p.entitySrc.Observe(); ok {
		t.Error("Observe() must return ok=false when id resolution failed; should retry next cycle")
	}
}

// TestMaybeResolveEntityID_InstanceNameSkipsFetch verifies that when
// instance_name is configured, maybeResolveEntityID is a no-op (the id was
// already pinned at construction).
func TestMaybeResolveEntityID_InstanceNameSkipsFetch(t *testing.T) {
	fetchCalled := false
	params := map[string]interface{}{
		"uri":           "mongodb://localhost:27017",
		"timeout":       5,
		"interval":      30,
		"instance_name": "operator-chosen",
	}
	probe, err := NewMongoDBProbe(params, newTestLogger())
	if err != nil {
		t.Fatalf("NewMongoDBProbe: %v", err)
	}
	p := probe.(*mongoDBProbe)
	p.fetchReplSetID = func(context.Context) (string, error) {
		fetchCalled = true
		return "mongodb:rs0/localhost:27017", nil
	}

	p.entitySrc.setReachable(true, "7.0.0")
	p.maybeResolveEntityID()

	obs, ok := p.entitySrc.Observe()
	if !ok {
		t.Fatal("Observe() returned ok=false when instance_name is set")
	}
	if obs.Entities[0].ID["db.instance.id"] != "operator-chosen" {
		t.Errorf("db.instance.id = %q, want operator-chosen", obs.Entities[0].ID["db.instance.id"])
	}
	if fetchCalled {
		t.Error("fetchReplSetID must not be called when id is already pinned via instance_name")
	}
}
