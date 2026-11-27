package systemd

import (
	"senhub-agent.go/internal/agent/probes/hostservice"
	"senhub-agent.go/internal/agent/services/entity"
)

const (
	systemdManager = "systemd"
)

// hostServiceUnit is the subset of a systemd unit needed to build its
// host.service entity. It is deliberately OS-independent; the D-Bus collector
// maps dbus.UnitStatus and UnitFileState into this value.
type hostServiceUnit struct {
	Name          string
	ActiveState   string
	UnitFileState string
}

// buildHostServiceEntity builds the host.service entity and its runs_on
// relation to the host.
func buildHostServiceEntity(hostID, hostName string, u hostServiceUnit) (entity.Entity, entity.Relation) {
	id := map[string]any{
		"host.id":           hostID,
		"host.service.name": u.Name,
	}

	ent := entity.Entity{
		Type: entity.TypeHostService,
		ID:   id,
		Attributes: map[string]any{
			"host.service.manager":    systemdManager,
			"host.service.start_mode": u.UnitFileState,
			"host.name":               hostName,
			hostservice.StatusKey:     hostservice.StatusSystemd(u.ActiveState),
		},
	}
	rel := entity.Relation{
		Type:     "runs_on",
		FromType: entity.TypeHostService,
		FromID:   id,
		ToType:   "host",
		ToID:     map[string]any{"host.id": hostID},
	}
	return ent, rel
}

func legacyServiceInstanceID(hostID, name string) string {
	return "systemd://" + hostID + "/" + name
}
