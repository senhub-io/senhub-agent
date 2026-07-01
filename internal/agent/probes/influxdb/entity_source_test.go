package influxdb

import (
	"testing"

	"senhub-agent.go/internal/agent/services/agentstate"
	"senhub-agent.go/internal/agent/services/entity"
)

// makeSource is a convenience helper to build an entity source from
// minimal config without going through the full probe constructor.
func makeSource(endpoint, instanceName string) *influxdbEntitySource {
	return newInfluxdbEntitySource(probeConfig{
		Endpoint:     endpoint,
		InstanceName: instanceName,
	})
}

// TestEntitySource_InstanceNameOverride verifies that operator-supplied
// instance_name is used verbatim as db.instance.id, taking precedence
// over host:port.
func TestEntitySource_InstanceNameOverride(t *testing.T) {
	src := makeSource("http://10.0.0.1:8086", "my-influx-prod")
	src.setReachable(true, "2.7.0")

	obs, ok := src.Observe()
	if !ok {
		t.Fatal("Observe returned ok=false, want true")
	}
	if len(obs.Entities) != 1 {
		t.Fatalf("want 1 entity, got %d", len(obs.Entities))
	}
	e := obs.Entities[0]
	if e.Type != "db" {
		t.Errorf("entity.Type = %q, want \"db\"", e.Type)
	}
	got, ok2 := e.ID["db.instance.id"]
	if !ok2 {
		t.Fatal("db.instance.id missing from entity ID")
	}
	if got != "my-influx-prod" {
		t.Errorf("db.instance.id = %q, want \"my-influx-prod\"", got)
	}
}

// TestEntitySource_HostPortFallback verifies that host:port is used as
// the db.instance.id when instance_name is not set (the documented db
// degraded fallback for probes without a stable tech id).
func TestEntitySource_HostPortFallback(t *testing.T) {
	src := makeSource("http://10.0.0.5:8086", "")
	src.setReachable(true, "")

	obs, ok := src.Observe()
	if !ok {
		t.Fatal("Observe returned ok=false, want true")
	}
	if len(obs.Entities) != 1 {
		t.Fatalf("want 1 entity, got %d", len(obs.Entities))
	}
	got, _ := obs.Entities[0].ID["db.instance.id"]
	if got != "10.0.0.5:8086" {
		t.Errorf("db.instance.id = %q, want \"10.0.0.5:8086\"", got)
	}
}

// TestEntitySource_NotEmittedWhenDown verifies that Observe returns ok=false
// while the instance is unreachable. This prevents emitting the entity (and
// thus the monitors edge) when the instance is known to be down, which would
// cause the consumer to receive an entity it cannot yet confirm is live.
func TestEntitySource_NotEmittedWhenDown(t *testing.T) {
	src := makeSource("http://10.0.0.1:8086", "")
	// Never called setReachable(true, …): up defaults to false.

	_, ok := src.Observe()
	if ok {
		t.Fatal("Observe returned ok=true for a down instance, want false")
	}
}

// TestEntitySource_MonitorsEdgePresent verifies that a monitors relation is
// included in the observation when the agent instance id is known.
func TestEntitySource_MonitorsEdgePresent(t *testing.T) {
	agentstate.SetAgentInstanceID("test-agent-id-001")
	t.Cleanup(func() { agentstate.SetAgentInstanceID("") })

	src := makeSource("http://10.0.0.1:8086", "my-influx")
	src.setReachable(true, "2.7.0")

	obs, ok := src.Observe()
	if !ok {
		t.Fatal("Observe returned ok=false")
	}
	if len(obs.Relations) == 0 {
		t.Fatal("want monitors relation, got none")
	}
	var found *entity.Relation
	for i := range obs.Relations {
		if obs.Relations[i].Type == "monitors" {
			found = &obs.Relations[i]
			break
		}
	}
	if found == nil {
		t.Fatal("monitors relation not found")
	}
	if found.FromType != "service.instance" {
		t.Errorf("FromType = %q, want \"service.instance\"", found.FromType)
	}
	if v, _ := found.FromID["service.instance.id"]; v != "test-agent-id-001" {
		t.Errorf("FromID[service.instance.id] = %v, want \"test-agent-id-001\"", v)
	}
	if found.ToType != "db" {
		t.Errorf("ToType = %q, want \"db\"", found.ToType)
	}
	if v, _ := found.ToID["db.instance.id"]; v != "my-influx" {
		t.Errorf("ToID[db.instance.id] = %v, want \"my-influx\"", v)
	}
}

// TestEntitySource_LocalDBRunsOnHost: a loopback-reachable influxdb with a
// host-unique identity (operator instance_name) is anchored to the host with
// runs_on (enterprise#36). A host:port identity embeds the loopback literal —
// identical on every host — so the collapse guard refuses the runs_on. A remote
// db is never anchored.
func TestEntitySource_LocalDBRunsOnHost(t *testing.T) {
	agentstate.SetAgentInstanceID("")
	runsOn := func(endpoint, instanceName string) bool {
		src := makeSource(endpoint, instanceName)
		src.hostID = func() string { return "h-1" }
		src.setReachable(true, "")
		obs, _ := src.Observe()
		for _, r := range obs.Relations {
			if r.Type == "runs_on" && r.FromType == "db" && r.ToID["host.id"] == "h-1" {
				return true
			}
		}
		return false
	}
	if !runsOn("http://127.0.0.1:8086", "prod-influx") {
		t.Error("loopback db with a host-unique id must emit runs_on→host")
	}
	if runsOn("http://127.0.0.1:8086", "") {
		t.Error("host:port identity must NOT emit runs_on on loopback (collapse guard)")
	}
	if runsOn("http://10.0.0.5:8086", "") {
		t.Error("remote db must NOT emit runs_on→host")
	}
}

// TestEntitySource_MonitorsEdgeAbsentWhenNoAgentID verifies that when the
// agent instance id is not set, no monitors relation is emitted (an
// unresolvable From endpoint must never reach the consumer).
func TestEntitySource_MonitorsEdgeAbsentWhenNoAgentID(t *testing.T) {
	agentstate.SetAgentInstanceID("")
	t.Cleanup(func() { agentstate.SetAgentInstanceID("") })

	src := makeSource("http://10.0.0.1:8086", "my-influx")
	src.setReachable(true, "2.7.0")

	obs, ok := src.Observe()
	if !ok {
		t.Fatal("Observe returned ok=false")
	}
	for _, r := range obs.Relations {
		if r.Type == "monitors" {
			t.Errorf("unexpected monitors relation when agent id is empty")
		}
	}
}

// TestEntitySource_DescriptiveAttrs verifies that server.address, server.port
// and db.system.name are emitted as descriptive (non-identity) attributes.
func TestEntitySource_DescriptiveAttrs(t *testing.T) {
	src := makeSource("http://db.example.com:9999", "prod-influx")
	src.setReachable(true, "2.7.1")

	obs, ok := src.Observe()
	if !ok {
		t.Fatal("Observe returned ok=false")
	}
	if len(obs.Entities) == 0 {
		t.Fatal("no entities in observation")
	}
	attrs := obs.Entities[0].Attributes
	if v, _ := attrs["db.system.name"]; v != "influxdb" {
		t.Errorf("db.system.name = %v, want \"influxdb\"", v)
	}
	if v, _ := attrs["server.address"]; v != "db.example.com" {
		t.Errorf("server.address = %v, want \"db.example.com\"", v)
	}
	if v, _ := attrs["server.port"]; v != int64(9999) {
		t.Errorf("server.port = %v, want 9999", v)
	}
	if v, _ := attrs["db.system.version"]; v != "2.7.1" {
		t.Errorf("db.system.version = %v, want \"2.7.1\"", v)
	}
}

// TestEntitySource_IDPinnedAtConstruction verifies that the entity id is
// chosen once at construction and does not change even if the endpoint
// could theoretically re-resolve differently.
func TestEntitySource_IDPinnedAtConstruction(t *testing.T) {
	src := makeSource("http://10.0.0.1:8086", "pinned-id")
	src.setReachable(true, "2.7.0")

	obs1, _ := src.Observe()
	src.setReachable(false, "")
	src.setReachable(true, "2.8.0")
	obs2, _ := src.Observe()

	id1, _ := obs1.Entities[0].ID["db.instance.id"]
	id2, _ := obs2.Entities[0].ID["db.instance.id"]
	if id1 != id2 {
		t.Errorf("id changed: %q → %q", id1, id2)
	}
}
