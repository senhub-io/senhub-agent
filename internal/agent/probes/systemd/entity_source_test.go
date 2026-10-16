//go:build linux

package systemd

import (
	"testing"

	"senhub-agent.go/internal/agent/services/entity"
)

func TestEntitySource_EmitsHostServiceEntities(t *testing.T) {
	src := newEntitySource("web-01")
	src.setUnits([]hostServiceUnit{
		{Name: "nginx.service", ActiveState: "active", UnitFileState: "enabled"},
		{Name: "nginx.socket", ActiveState: "active", UnitFileState: "enabled"},
	}, "machine-abc", "web-01")

	obs, ok := src.Observe()
	if !ok {
		t.Fatal("Observe() ok=false")
	}
	if len(obs.Entities) != 2 {
		t.Fatalf("entities = %d, want 2", len(obs.Entities))
	}

	for i, want := range []string{"nginx.service", "nginx.socket"} {
		e := obs.Entities[i]
		if e.Type != entity.TypeHostService {
			t.Errorf("entities[%d].Type = %q, want %q", i, e.Type, entity.TypeHostService)
		}
		if got := e.ID["host.id"]; got != "machine-abc" {
			t.Errorf("entities[%d].ID[host.id] = %v, want machine-abc", i, got)
		}
		if got := e.ID["host.service.name"]; got != want {
			t.Errorf("entities[%d].ID[host.service.name] = %v, want %q", i, got, want)
		}
		if got := e.Attributes["host.service.manager"]; got != "systemd" {
			t.Errorf("entities[%d] manager = %v, want systemd", i, got)
		}
		if _, ok := e.Attributes["host.service.running"]; ok {
			t.Errorf("entities[%d]: the running state is a series, not an entity attribute", i)
		}
		if got := e.Attributes["host.service.start_mode"]; got != "enabled" {
			t.Errorf("entities[%d] start_mode = %v, want enabled", i, got)
		}
		if got := e.Attributes["host.name"]; got != "web-01" {
			t.Errorf("entities[%d] host.name = %v, want web-01", i, got)
		}
	}

	var runsOn, sameAs int
	for _, rel := range obs.Relations {
		if rel.Type == "runs_on" {
			runsOn++
			if rel.ToType != "host" || rel.ToID["host.id"] != "machine-abc" {
				t.Errorf("runs_on must target host.id: %+v", rel)
			}
		}
		if rel.Type == entity.RelSameAs {
			sameAs++
			if rel.ToType != "service.instance" {
				t.Errorf("same_as target type = %q, want service.instance", rel.ToType)
			}
		}
	}
	if runsOn != 2 {
		t.Errorf("runs_on relations = %d, want 2", runsOn)
	}
	if sameAs != 2 {
		t.Errorf("same_as relations = %d, want 2", sameAs)
	}
}

func TestEntitySource_NoHostIDNoEntity(t *testing.T) {
	src := newEntitySource("web-01")
	src.setUnits([]hostServiceUnit{
		{Name: "nginx.service", ActiveState: "active", UnitFileState: "enabled"},
	}, "", "web-01")

	obs, ok := src.Observe()
	if !ok {
		t.Fatal("Observe() ok=false")
	}
	if len(obs.Entities) != 0 || len(obs.Relations) != 0 {
		t.Errorf("no host.id must yield no entity, got %+v", obs)
	}
}
