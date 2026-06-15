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

	mu    sync.Mutex
	units []string // unit names from the last Collect cycle
	ready bool
}

func newEntitySource(hostname string) *systemdEntitySource {
	return &systemdEntitySource{hostname: hostname}
}

// setUnits is called by Collect() after each successful D-Bus query.
func (s *systemdEntitySource) setUnits(names []string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.units = names
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
		obs.Entities = append(obs.Entities, entity.Entity{
			Type: "service.instance",
			ID:   map[string]any{"service.instance.id": id},
			Attributes: map[string]any{
				"systemd.unit":      name,
				"systemd.unit.type": unitTypeSuffix(name),
				"host.name":         s.hostname,
			},
		})
	}
	return obs, true
}
