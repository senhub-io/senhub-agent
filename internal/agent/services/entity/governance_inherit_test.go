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

	out := inheritHostGovernance(obs, "h1", hostGov)

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

func TestInheritHostGovernanceIsANoOpWithNothingToGive(t *testing.T) {
	id := map[string]any{"service.instance.id": "s"}
	obs := Observation{
		Entities:  []Entity{{Type: "service.instance", ID: id}},
		Relations: []Relation{{Type: "runs_on", FromType: "service.instance", FromID: id, ToType: "host", ToID: map[string]any{"host.id": "h1"}}},
	}
	out := inheritHostGovernance(obs, "h1", map[string]any{"unrelated.key": "x"})
	if out.Entities[0].Attributes != nil {
		t.Errorf("a host with no governance gives nothing, got %v", out.Entities[0].Attributes)
	}
	if out := inheritHostGovernance(obs, "", map[string]any{"entity.location.site": "x"}); out.Entities[0].Attributes != nil {
		t.Error("an unknown host id must inherit nothing")
	}
}

// The host's ownership descends to the things an operator reasons about,
// and to nothing remote. Pins the decision taken on #854: a blind
// inheritance would give a database read from another machine the owner
// of the collector's own VM, which is wrong rather than noisy.
func TestOwnershipDescendsToTheTypesAnOperatorReasonsAbout(t *testing.T) {
	svc := map[string]any{"service.instance.id": "app"}
	listener := map[string]any{"service.listener.id": "l1"}
	remote := map[string]any{"db.id": "far"}
	local := map[string]any{"host.id": "h1"}

	obs := Observation{
		Entities: []Entity{
			{Type: "service.instance", ID: svc},
			{Type: "service.listener", ID: listener},
			{Type: "db", ID: remote},
		},
		Relations: []Relation{
			{Type: "runs_on", FromType: "service.instance", FromID: svc, ToType: "host", ToID: local},
			{Type: "runs_on", FromType: "service.listener", FromID: listener, ToType: "host", ToID: local},
			// The database is read from here but does not run here.
			{Type: "monitors", FromType: "service.instance", FromID: svc, ToType: "db", ToID: remote},
		},
	}
	gov := map[string]any{
		"entity.owner.team":        "sre",
		"service.criticality":      "critical",
		"entity.label.application": "opspilot",
		"entity.location.site":     "par",
	}
	out := inheritHostGovernance(obs, "h1", gov)

	byType := map[string]map[string]any{}
	for _, e := range out.Entities {
		byType[e.Type] = e.Attributes
	}
	if got := byType["service.instance"]["entity.owner.team"]; got != "sre" {
		t.Errorf("a service instance must inherit the owner, got %v", got)
	}
	if got := byType["service.instance"]["entity.label.application"]; got != "opspilot" {
		t.Errorf("the head-of-chain label travels with ownership, got %v", got)
	}
	if _, set := byType["service.listener"]["entity.owner.team"]; set {
		t.Error("a listener reaches its host in one hop; copying the owner onto it duplicates a fact")
	}
	if got := byType["service.listener"]["entity.location.site"]; got != "par" {
		t.Errorf("a listener is still where the host is, got %v", got)
	}
	if _, set := byType["db"]["entity.owner.team"]; set {
		t.Error("a database that does not run here must not inherit this host's owner")
	}
}

// A source that knows better than the host it runs on keeps its word.
func TestTheSourcesOwnValueWins(t *testing.T) {
	svc := map[string]any{"service.instance.id": "app"}
	obs := Observation{
		Entities: []Entity{{Type: "service.instance", ID: svc, Attributes: map[string]any{"entity.owner.team": "payments"}}},
		Relations: []Relation{
			{Type: "runs_on", FromType: "service.instance", FromID: svc, ToType: "host", ToID: map[string]any{"host.id": "h1"}},
		},
	}
	out := inheritHostGovernance(obs, "h1", map[string]any{"entity.owner.team": "sre", "service.criticality": "high"})
	if got := out.Entities[0].Attributes["entity.owner.team"]; got != "payments" {
		t.Errorf("the source's own owner must win, got %v", got)
	}
	if got := out.Entities[0].Attributes["service.criticality"]; got != "high" {
		t.Errorf("a key the source does not set is still filled, got %v", got)
	}
}
