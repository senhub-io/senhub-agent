package oracle

import (
	"testing"

	"senhub-agent.go/internal/agent/services/agentstate"
	"senhub-agent.go/internal/agent/services/entity"
)

func TestEntitySource_Observe(t *testing.T) {
	instance := "oracle://db.example.com:1521/ORCL"
	src := newEntitySource(instance, "db.example.com")

	obs, ok := src.Observe()
	if !ok {
		t.Fatal("Observe() ok=false, want true")
	}
	if len(obs.Entities) != 1 {
		t.Fatalf("Observe() returned %d entities, want 1", len(obs.Entities))
	}

	e := obs.Entities[0]
	if e.Type != entityTypeDB {
		t.Errorf("entity.Type = %q, want %q", e.Type, entityTypeDB)
	}

	id, ok := e.ID[idKeyDBInstance]
	if !ok {
		t.Fatalf("entity.ID missing %q key", idKeyDBInstance)
	}
	if id != instance {
		t.Errorf("entity.ID[%q] = %q, want %q", idKeyDBInstance, id, instance)
	}

	// Identity is single-key: db.system.name is a descriptive attribute, NOT an
	// identity key (else the monitors ToID would not resolve — #505).
	if len(e.ID) != 1 {
		t.Errorf("entity.ID must be single-key {db.instance.id}, got %v", e.ID)
	}
	if sys := e.Attributes[attrDBSystem]; sys != "oracle" {
		t.Errorf("attribute %q = %v, want oracle", attrDBSystem, sys)
	}
}

// TestEntitySource_Version verifies setVersion surfaces the server version on
// the entity as db.system.version (toise#216 AT1), absent until reported.
func TestEntitySource_Version(t *testing.T) {
	src := newEntitySource("oracle://db.example.com:1521/ORCL", "db.example.com")

	obs, _ := src.Observe()
	if _, has := obs.Entities[0].Attributes["db.system.version"]; has {
		t.Error("db.system.version must be absent before a version is reported")
	}

	src.setVersion("19.0.0.0.0")
	obs, _ = src.Observe()
	if got := obs.Entities[0].Attributes["db.system.version"]; got != "19.0.0.0.0" {
		t.Errorf("db.system.version = %v, want 19.0.0.0.0", got)
	}
}

// TestEntitySource_MonitorsEdge verifies the db is anchored to the agent via a
// monitors edge whose ToID exactly matches the entity identity, and that the
// edge is skipped when the agent id is unset.
func TestEntitySource_MonitorsEdge(t *testing.T) {
	instance := "oracle://db.example.com:1521/ORCL"
	src := newEntitySource(instance, "db.example.com")

	t.Run("emitted with agent id", func(t *testing.T) {
		agentstate.SetAgentInstanceID("agent-key-1")
		t.Cleanup(func() { agentstate.SetAgentInstanceID("") })

		obs, _ := src.Observe()
		var found bool
		for _, r := range obs.Relations {
			if r.Type != "monitors" {
				continue
			}
			found = true
			if r.FromID["service.instance.id"] != "agent-key-1" {
				t.Errorf("monitors From = %v, want agent-key-1", r.FromID)
			}
			if r.ToType != entityTypeDB || r.ToID[idKeyDBInstance] != instance {
				t.Errorf("monitors ToID must match the db identity, got %v", r.ToID)
			}
		}
		if !found {
			t.Errorf("no monitors edge emitted: %+v", obs.Relations)
		}
	})

	t.Run("skipped without agent id", func(t *testing.T) {
		agentstate.SetAgentInstanceID("")
		obs, _ := src.Observe()
		// Remote db (db.example.com) — neither a monitors nor a runs_on edge.
		for _, ty := range oracleRelTypes(obs) {
			if ty == "monitors" {
				t.Errorf("no monitors edge expected when agent id is unset, got %+v", obs.Relations)
			}
		}
	})
}

// TestEntitySource_LocalRunsOn exercises the runs_on wiring. The oracle id is a
// DSN that embeds the monitored host, so the LocalRunsOn collapse guard refuses
// the edge even for a loopback target — a loopback-derived id is not host-unique
// and must not anchor a host. A remote target yields no edge either. The probe
// is wired for consistency with its siblings; the gate guarantees correctness.
func TestEntitySource_LocalRunsOn(t *testing.T) {
	agentstate.SetAgentInstanceID("agent-key-1")
	t.Cleanup(func() { agentstate.SetAgentInstanceID("") })

	// Loopback target, but the DSN id embeds "127.0.0.1" — the guard refuses runs_on.
	local := newEntitySource("oracle://127.0.0.1:1521/ORCL", "127.0.0.1")
	local.hostID = func() string { return "H" }
	obs, _ := local.Observe()
	for _, ty := range oracleRelTypes(obs) {
		if ty == "runs_on" {
			t.Errorf("loopback-embedding id must NOT emit runs_on (collapse guard); relations=%v", oracleRelTypes(obs))
		}
	}

	// Remote target — no runs_on.
	remote := newEntitySource("oracle://10.0.0.5:1521/ORCL", "10.0.0.5")
	remote.hostID = func() string { return "H" }
	robs, _ := remote.Observe()
	for _, ty := range oracleRelTypes(robs) {
		if ty == "runs_on" {
			t.Errorf("remote db must NOT emit runs_on; relations=%v", oracleRelTypes(robs))
		}
	}
}

func oracleRelTypes(obs entity.Observation) []string {
	var ts []string
	for _, r := range obs.Relations {
		ts = append(ts, r.Type)
	}
	return ts
}
