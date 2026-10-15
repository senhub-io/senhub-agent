package systemd

import (
	"testing"

	"senhub-agent.go/internal/agent/services/entity"
)

func TestBuildHostServiceEntity(t *testing.T) {
	e, rel := buildHostServiceEntity("host-123", "web-01", hostServiceUnit{
		Name:          "nginx.service",
		ActiveState:   "active",
		UnitFileState: "enabled",
	})

	if e.Type != entity.TypeHostService {
		t.Fatalf("entity type = %q, want %q", e.Type, entity.TypeHostService)
	}
	if got := e.ID["host.id"]; got != "host-123" {
		t.Fatalf("entity host.id = %v, want host-123", got)
	}
	if got := e.ID["host.service.name"]; got != "nginx.service" {
		t.Fatalf("entity host.service.name = %v, want nginx.service", got)
	}
	if got := e.Attributes["host.name"]; got != "web-01" {
		t.Fatalf("entity host.name = %v, want web-01", got)
	}
	if got := e.Attributes["host.service.manager"]; got != "systemd" {
		t.Fatalf("entity manager = %v, want systemd", got)
	}
	if got := e.Attributes["host.service.state"]; got != "active" {
		t.Fatalf("entity state = %v, want active", got)
	}
	if got := e.Attributes["host.service.start_mode"]; got != "enabled" {
		t.Fatalf("entity start_mode = %v, want enabled", got)
	}

	if rel.Type != "runs_on" {
		t.Fatalf("relation type = %q, want runs_on", rel.Type)
	}
	if rel.FromType != entity.TypeHostService {
		t.Fatalf("relation FromType = %q, want %q", rel.FromType, entity.TypeHostService)
	}
	if rel.ToType != "host" {
		t.Fatalf("relation ToType = %q, want host", rel.ToType)
	}
	if got := rel.ToID["host.id"]; got != "host-123" {
		t.Fatalf("relation ToID host.id = %v, want host-123", got)
	}
}

func TestLegacyServiceInstanceID(t *testing.T) {
	if got, want := legacyServiceInstanceID("host-123", "nginx.service"), "systemd://host-123/nginx.service"; got != want {
		t.Fatalf("legacyServiceInstanceID = %q, want %q", got, want)
	}
}
