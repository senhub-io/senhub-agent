package oracle

import (
	"testing"

	"senhub-agent.go/internal/agent/services/agentstate"
)

func TestEntitySource_Observe(t *testing.T) {
	instance := "oracle://db.example.com:1521/ORCL"
	src := newEntitySource(instance)

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

// TestEntitySource_MonitorsEdge verifies the db is anchored to the agent via a
// monitors edge whose ToID exactly matches the entity identity, and that the
// edge is skipped when the agent id is unset.
func TestEntitySource_MonitorsEdge(t *testing.T) {
	instance := "oracle://db.example.com:1521/ORCL"
	src := newEntitySource(instance)

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
		if len(obs.Relations) != 0 {
			t.Errorf("no edge expected when agent id is unset, got %+v", obs.Relations)
		}
	})
}
