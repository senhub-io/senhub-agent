package kafka

import (
	"errors"
	"testing"

	"senhub-agent.go/internal/agent/services/agentstate"
	"senhub-agent.go/internal/agent/services/entity"
)

// entitySourceFixture constructs a kafkaEntitySource with injected stubs for
// unit-testing without a live Kafka cluster or gopsutil host lookup.
func entitySourceFixture(instanceName string, fetch techIDFetcher) *kafkaEntitySource {
	return newKafkaEntitySource(
		"broker1:9092",
		instanceName,
		fetch,
		func() string { return "test-machine-uuid" },
	)
}

// findEntity returns the first entity from obs, or a zero value.
func findEntity(obs entity.Observation) (entity.Entity, bool) {
	if len(obs.Entities) == 0 {
		return entity.Entity{}, false
	}
	return obs.Entities[0], true
}

func findRelation(obs entity.Observation, relType string) (entity.Relation, bool) {
	for _, r := range obs.Relations {
		if r.Type == relType {
			return r, true
		}
	}
	return entity.Relation{}, false
}

// TestEntitySource_NotEmittedBeforeIDPinned verifies that Observe returns
// ok=false before the id has been resolved (first collect has not run yet).
func TestEntitySource_NotEmittedBeforeIDPinned(t *testing.T) {
	s := entitySourceFixture("", func() (string, error) { return "abc123", nil })
	_, ok := s.Observe()
	if ok {
		t.Error("entity must not be emitted before the id is pinned")
	}
}

// TestEntitySource_InstanceNamePinnedAtConstruction verifies that when
// instance_name is set, the entity is pinned immediately and Observe returns
// ok=true once the broker is marked up (first notifySuccess).
func TestEntitySource_InstanceNamePinnedAtConstruction(t *testing.T) {
	s := entitySourceFixture("my-kafka", nil)

	// Still not up — no collect has run.
	_, ok := s.Observe()
	if ok {
		t.Error("entity must not be emitted before first notifySuccess even with instance_name")
	}

	s.notifySuccess("")
	obs, ok := s.Observe()
	if !ok {
		t.Fatal("entity must be emitted after first notifySuccess with instance_name")
	}
	e, found := findEntity(obs)
	if !found {
		t.Fatal("no entity in observation")
	}
	if got := e.ID["service.instance.id"]; got != "my-kafka" {
		t.Errorf("service.instance.id = %v, want my-kafka", got)
	}
}

// TestEntitySource_TechIDPinned verifies that the cluster id returned by the
// tech-id fetcher is used as "kafka:<id>" and is pinned after the first
// notifySuccess.
func TestEntitySource_TechIDPinned(t *testing.T) {
	fetchCalls := 0
	s := entitySourceFixture("", func() (string, error) {
		fetchCalls++
		return "cluster-xyz", nil
	})

	s.notifySuccess("")
	obs, ok := s.Observe()
	if !ok {
		t.Fatal("entity must be emitted after first notifySuccess with tech id")
	}
	e, _ := findEntity(obs)
	if got := e.ID["service.instance.id"]; got != "kafka:cluster-xyz" {
		t.Errorf("service.instance.id = %v, want kafka:cluster-xyz", got)
	}

	// Second notifySuccess must not re-invoke the fetcher.
	s.notifySuccess("")
	if fetchCalls != 1 {
		t.Errorf("fetcher called %d times, want exactly 1 (pinned after first success)", fetchCalls)
	}

	// Id must remain the same.
	obs2, _ := s.Observe()
	e2, _ := findEntity(obs2)
	if got := e2.ID["service.instance.id"]; got != "kafka:cluster-xyz" {
		t.Errorf("id changed on second observe: %v", got)
	}
}

// TestEntitySource_NotEmittedWhenTechIDFetchFails verifies that the entity is
// withheld (ok=false) when notifySuccess is called but the tech-id fetch
// returns an error (cluster still reachable but id not yet available).
func TestEntitySource_NotEmittedWhenTechIDFetchFails(t *testing.T) {
	s := entitySourceFixture("", func() (string, error) {
		return "", errors.New("transient fetch error")
	})

	s.notifySuccess("")
	_, ok := s.Observe()
	if ok {
		t.Error("entity must not be emitted when tech-id fetch fails (id not pinned yet)")
	}
}

// TestEntitySource_FallbackAfterFirstFailure verifies that after the first
// notifyFailure, the entity source degrades to the host-id fallback and the
// entity is then emitted with "kafka@<host-id>".
func TestEntitySource_FallbackAfterFirstFailure(t *testing.T) {
	s := entitySourceFixture("", func() (string, error) { return "cid", nil })

	s.notifyFailure()
	// After the first failure, entity is still withheld (up=false).
	_, ok := s.Observe()
	if ok {
		t.Error("entity must not be emitted when up=false (target unreachable)")
	}

	// The fallback id should be pinned. Verify by calling notifySuccess
	// (simulate recovery) and checking the id does NOT change to tech id.
	s.notifySuccess("")
	obs, ok := s.Observe()
	if !ok {
		t.Fatal("entity must be emitted after recovery when fallback is pinned")
	}
	e, _ := findEntity(obs)
	// Must be the host-id fallback, not the tech id (degraded=true).
	if got := e.ID["service.instance.id"]; got != "kafka@test-machine-uuid" {
		t.Errorf("service.instance.id = %v, want kafka@test-machine-uuid", got)
	}
}

// TestEntitySource_LastResortFallback verifies that when both the tech id and
// the host id are unavailable, the entity gets id "kafka".
func TestEntitySource_LastResortFallback(t *testing.T) {
	s := newKafkaEntitySource(
		"broker1:9092",
		"",
		func() (string, error) { return "", errors.New("unavailable") },
		func() string { return "" }, // host id unavailable
	)

	s.notifyFailure()
	s.notifySuccess("") // recover, but degraded already with "kafka"
	obs, ok := s.Observe()
	if !ok {
		t.Fatal("entity must be emitted after recovery")
	}
	e, _ := findEntity(obs)
	if got := e.ID["service.instance.id"]; got != "kafka" {
		t.Errorf("service.instance.id = %v, want kafka", got)
	}
}

// TestEntitySource_MonitorsEdgePresent verifies that the monitors relation is
// appended when the agent instance id is set.
func TestEntitySource_MonitorsEdgePresent(t *testing.T) {
	agentstate.SetAgentInstanceID("agent-instance-001")
	t.Cleanup(func() { agentstate.SetAgentInstanceID("") })

	s := entitySourceFixture("my-kafka", nil)
	s.notifySuccess("")

	obs, ok := s.Observe()
	if !ok {
		t.Fatal("entity must be emitted")
	}
	rel, found := findRelation(obs, "monitors")
	if !found {
		t.Fatal("monitors relation must be present when agent id is set")
	}
	if rel.Type != "monitors" {
		t.Errorf("relation type = %v, want monitors", rel.Type)
	}
	if rel.FromType != "service.instance" || rel.ToType != "service.instance" {
		t.Errorf("relation endpoints wrong: from=%v to=%v", rel.FromType, rel.ToType)
	}
	if got := rel.FromID["service.instance.id"]; got != "agent-instance-001" {
		t.Errorf("From id = %v, want agent-instance-001", got)
	}
	if got := rel.ToID["service.instance.id"]; got != "my-kafka" {
		t.Errorf("To id = %v, want my-kafka", got)
	}
}

// TestEntitySource_MonitorsEdgeAbsentWhenNoAgentID verifies that the monitors
// relation is omitted when the agent id is empty (entity emission disabled or
// not yet started).
func TestEntitySource_MonitorsEdgeAbsentWhenNoAgentID(t *testing.T) {
	agentstate.SetAgentInstanceID("")

	s := entitySourceFixture("my-kafka", nil)
	s.notifySuccess("")

	obs, ok := s.Observe()
	if !ok {
		t.Fatal("entity must be emitted")
	}
	if _, found := findRelation(obs, "monitors"); found {
		t.Error("monitors relation must NOT be present when agent id is empty")
	}
}

// TestEntitySource_DescriptiveAttrs verifies that service.name, server.address
// and server.port are present as descriptive attributes (not part of the id).
func TestEntitySource_DescriptiveAttrs(t *testing.T) {
	s := newKafkaEntitySource("kafka-host:9093", "my-kafka", nil, func() string { return "" })
	s.notifySuccess("3.5.0")

	obs, ok := s.Observe()
	if !ok {
		t.Fatal("entity must be emitted")
	}
	e, _ := findEntity(obs)
	if e.Attributes["service.name"] != "kafka" {
		t.Errorf("service.name = %v, want kafka", e.Attributes["service.name"])
	}
	if e.Attributes["server.address"] != "kafka-host" {
		t.Errorf("server.address = %v, want kafka-host", e.Attributes["server.address"])
	}
	if e.Attributes["server.port"] != int64(9093) {
		t.Errorf("server.port = %v, want 9093", e.Attributes["server.port"])
	}
	if e.Attributes["service.version"] != "3.5.0" {
		t.Errorf("service.version = %v, want 3.5.0", e.Attributes["service.version"])
	}
	// Id must not contain host/port.
	if id := e.ID["service.instance.id"]; id != "my-kafka" {
		t.Errorf("id contains network info: %v", id)
	}
}

// TestEntitySource_IDIsImmutable verifies that repeated notifySuccess calls
// with different cluster ids (should not happen in practice) do not change the
// pinned id (immutability).
func TestEntitySource_IDIsImmutable(t *testing.T) {
	call := 0
	s := entitySourceFixture("", func() (string, error) {
		call++
		switch call {
		case 1:
			return "first-id", nil
		default:
			return "second-id", nil
		}
	})

	s.notifySuccess("") // pins "kafka:first-id"
	obs1, _ := s.Observe()
	e1, _ := findEntity(obs1)
	id1 := e1.ID["service.instance.id"]

	s.notifySuccess("") // must not change the id
	obs2, _ := s.Observe()
	e2, _ := findEntity(obs2)
	id2 := e2.ID["service.instance.id"]

	if id1 != "kafka:first-id" {
		t.Errorf("initial id = %v, want kafka:first-id", id1)
	}
	if id1 != id2 {
		t.Errorf("id changed from %v to %v (immutability violation)", id1, id2)
	}
}
