//go:build linux

package systemd

import "testing"

func TestEntitySource_IdentityIsHostIDBased(t *testing.T) {
	src := newEntitySource("web-01")
	src.setUnits([]string{"nginx.service"}, "machine-abc")

	obs, ok := src.Observe()
	if !ok {
		t.Fatal("Observe() ok=false")
	}
	if len(obs.Entities) != 1 {
		t.Fatalf("entities = %d, want 1", len(obs.Entities))
	}
	if got, want := obs.Entities[0].ID["service.instance.id"], "systemd://machine-abc/nginx.service"; got != want {
		t.Errorf("id = %v, want %q", got, want)
	}
	if len(obs.Relations) != 1 || obs.Relations[0].ToID["host.id"] != "machine-abc" {
		t.Errorf("runs_on must target host.id: %+v", obs.Relations)
	}
}

func TestEntitySource_NoHostIDNoEntity(t *testing.T) {
	src := newEntitySource("web-01")
	src.setUnits([]string{"nginx.service"}, "")

	obs, ok := src.Observe()
	if !ok {
		t.Fatal("Observe() ok=false")
	}
	if len(obs.Entities) != 0 || len(obs.Relations) != 0 {
		t.Errorf("no host.id must yield no entity, got %+v", obs)
	}
}
