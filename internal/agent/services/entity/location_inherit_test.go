package entity

import "testing"

func TestInheritHostLocationOnlyReachesWhatRunsOnThisHost(t *testing.T) {
	hostGov := map[string]any{
		"entity.location.site":     "paris",
		"entity.location.rack":     "R12",
		"entity.owner.team":        "infra",
		"service.criticality":      "critical",
		"entity.label.application": "hosting",
	}
	localID := map[string]any{"db.instance.id": "local"}
	remoteID := map[string]any{"db.instance.id": "remote"}
	obs := Observation{
		Entities: []Entity{
			{Type: "host", ID: map[string]any{"host.id": "h1"}, Attributes: map[string]any{}},
			{Type: "db.instance", ID: localID, Attributes: map[string]any{"entity.location.rack": "R7"}},
			{Type: "db.instance", ID: remoteID, Attributes: map[string]any{}},
		},
		Relations: []Relation{
			{Type: "runs_on", FromType: "db.instance", FromID: localID, ToType: "host", ToID: map[string]any{"host.id": "h1"}},
			{Type: "runs_on", FromType: "db.instance", FromID: remoteID, ToType: "host", ToID: map[string]any{"host.id": "other"}},
		},
	}
	original := obs.Entities[1].Attributes

	out := inheritHostLocation(obs, "h1", hostGov)

	local := out.Entities[1].Attributes
	if local["entity.location.site"] != "paris" {
		t.Errorf("site must descend to the local entity, got %v", local)
	}
	if local["entity.location.rack"] != "R7" {
		t.Errorf("the entity's own rack must win, got %v", local["entity.location.rack"])
	}
	for _, k := range []string{"entity.owner.team", "service.criticality", "entity.label.application"} {
		if _, set := local[k]; set {
			t.Errorf("%s must not be inherited", k)
		}
	}
	if len(out.Entities[2].Attributes) != 0 {
		t.Errorf("an entity on another host must be left alone, got %v", out.Entities[2].Attributes)
	}
	if len(out.Entities[0].Attributes) != 0 {
		t.Errorf("the host entity must be left alone, got %v", out.Entities[0].Attributes)
	}
	if _, mutated := original["entity.location.site"]; mutated {
		t.Error("the source's map must not be mutated")
	}
}

func TestInheritHostLocationIsANoOpWithoutLocation(t *testing.T) {
	id := map[string]any{"service.instance.id": "s"}
	obs := Observation{
		Entities:  []Entity{{Type: "service.instance", ID: id}},
		Relations: []Relation{{Type: "runs_on", FromType: "service.instance", FromID: id, ToType: "host", ToID: map[string]any{"host.id": "h1"}}},
	}
	out := inheritHostLocation(obs, "h1", map[string]any{"entity.owner.team": "infra"})
	if out.Entities[0].Attributes != nil {
		t.Errorf("no location on the host means nothing to inherit, got %v", out.Entities[0].Attributes)
	}
	if out := inheritHostLocation(obs, "", map[string]any{"entity.location.site": "x"}); out.Entities[0].Attributes != nil {
		t.Error("an unknown host id must inherit nothing")
	}
}
