//go:build linux

package systemd

import (
	"fmt"
	"sync"

	"senhub-agent.go/internal/agent/services/entity"
)

// systemdEntitySource emits one service.instance entity per supervised
// unit. The entity type follows the OpenTelemetry service resource
// conventions: {service.instance.id} uniquely identifies the unit on
// the host.
//
// The entity cache is rebuilt on every Observe() call from the probe's
// last collected unit list. This is intentionally simple: the probe
// poll cycle drives the refresh; there is no separate sweep goroutine.
type systemdEntitySource struct {
	hostname string

	mu     sync.Mutex
	units  []string // unit names from the last Collect cycle
	hostID string   // stable host.id used in runs_on relations
	ready  bool
}

func newEntitySource(hostname string) *systemdEntitySource {
	return &systemdEntitySource{hostname: hostname}
}

// setUnits is called by Collect() after each successful D-Bus query.
// hostID is the stable host identity (machine-id / UUID) used to
// attach each service.instance to its host via a runs_on relation.
func (s *systemdEntitySource) setUnits(names []string, hostID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.units = names
	s.hostID = hostID
	s.ready = true
}

// Observe returns the last cached entity snapshot. Non-blocking.
func (s *systemdEntitySource) Observe() (entity.Observation, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.ready {
		return entity.Observation{}, false
	}

	obs := entity.Observation{}
	for _, name := range s.units {
		id := fmt.Sprintf("systemd://%s/%s", s.hostname, name)
		entityID := map[string]any{"service.instance.id": id}
		obs.Entities = append(obs.Entities, entity.Entity{
			Type: "service.instance",
			ID:   entityID,
			Attributes: map[string]any{
				"systemd.unit":      name,
				"systemd.unit.type": unitTypeSuffix(name),
				"host.name":         s.hostname,
			},
		})
		if s.hostID != "" {
			obs.Relations = append(obs.Relations, entity.Relation{
				Type:     "runs_on",
				FromType: "service.instance",
				FromID:   entityID,
				ToType:   "host",
				ToID:     map[string]any{"host.id": s.hostID},
			})
		}
	}
	return obs, true
}
