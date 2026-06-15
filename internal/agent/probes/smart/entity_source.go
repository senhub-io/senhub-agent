package smart

import (
	"senhub-agent.go/internal/agent/services/entity"
)

// smartEntitySource implements entity.Source for the S.M.A.R.T. probe.
// It emits a single "service.instance" entity that represents the disk
// health monitoring service running on this host. Disk-level entities
// (individual drives) are not modelled yet — they would require a stable
// identifier scheme (serial number, WWN) across collector cycles.
type smartEntitySource struct{}

// Observe returns one service.instance entity for the local smart
// monitoring instance. The observation is always valid (ok=true) —
// the probe represents the monitoring service itself, not an external
// resource that can be unreachable.
func (s *smartEntitySource) Observe() (entity.Observation, bool) {
	e := entity.Entity{
		Type: "service.instance",
		ID: map[string]any{
			"service.instance.id": "smart://localhost",
		},
		Attributes: map[string]any{
			"service.name": "smart",
		},
	}
	return entity.Observation{Entities: []entity.Entity{e}}, true
}
