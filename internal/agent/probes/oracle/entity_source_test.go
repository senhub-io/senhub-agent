package oracle

import (
	"testing"
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

	sys, ok := e.ID[idKeyDBSystem]
	if !ok {
		t.Fatalf("entity.ID missing %q key", idKeyDBSystem)
	}
	if sys != "oracle" {
		t.Errorf("entity.ID[%q] = %q, want oracle", idKeyDBSystem, sys)
	}

	// Relations are empty — static single entity, no topology.
	if len(obs.Relations) != 0 {
		t.Errorf("Observe() returned %d relations, want 0", len(obs.Relations))
	}
}

// TestEntitySource_Idempotent verifies that successive Observe calls return
// the same stable snapshot (no mutation between calls).
func TestEntitySource_Idempotent(t *testing.T) {
	src := newEntitySource("oracle://db.example.com:1521/ORCL")

	obs1, _ := src.Observe()
	obs2, _ := src.Observe()

	if len(obs1.Entities) != len(obs2.Entities) {
		t.Errorf("entity count changed between calls: %d vs %d", len(obs1.Entities), len(obs2.Entities))
	}
}
