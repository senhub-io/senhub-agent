package entity_test

import (
	"testing"

	"senhub-agent.go/probesdk/entity"
)

// fakeSource is the shape an enterprise probe would implement against the
// mirror: build Entity/Relation values and return them from Observe.
type fakeSource struct{}

func (fakeSource) Observe() entity.Observation {
	return entity.Observation{
		Entities: []entity.Entity{{
			Type: "db",
			ID:   map[string]any{"db.instance.id": "postgresql:7311168095704935424"},
			Attributes: map[string]any{
				"db.system.name": "postgresql",
				"server.address": "10.0.1.5",
				"server.port":    int64(5432),
			},
		}},
		Relations: []entity.Relation{{
			Type:     "monitors",
			FromType: "service.instance", FromID: map[string]any{"service.instance.id": "agent-1"},
			ToType: "db", ToID: map[string]any{"db.instance.id": "postgresql:7311168095704935424"},
		}},
	}
}

// TestMirror_UsableAsSource proves the public API surface compiles and a probe
// can build observations + register a source through the mirror alone.
func TestMirror_UsableAsSource(t *testing.T) {
	var s entity.Source = fakeSource{}
	entity.RegisterSource(s)

	obs := s.Observe()
	if len(obs.Entities) != 1 || obs.Entities[0].Type != "db" {
		t.Fatalf("observation entities = %+v, want one db entity", obs.Entities)
	}
	if len(obs.Relations) != 1 || obs.Relations[0].Type != "monitors" {
		t.Fatalf("observation relations = %+v, want one monitors relation", obs.Relations)
	}
}
