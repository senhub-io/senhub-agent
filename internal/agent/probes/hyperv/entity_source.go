// Package hyperv — entity source for the Hyper-V host.
// This file compiles on all platforms; the entity source is used only on Windows.
package hyperv

import (
	"sync"

	"senhub-agent.go/internal/agent/services/entity"
)

const (
	entityTypeServiceInstance = "service.instance"
	entityIDServiceInstanceID = "service.instance.id"
	hypervInstanceID          = "hyperv://localhost"
)

// hypervEntitySource reports the local Hyper-V host as a service.instance entity.
// It is populated by the probe's Collect cycle and stays valid between cycles so
// the detector can re-emit the heartbeat at its own cadence.
type hypervEntitySource struct {
	mu   sync.Mutex
	ok   bool // whether the last WMI collection succeeded
	once bool // whether Collect has run at least once
}

func newHypervEntitySource() *hypervEntitySource {
	return &hypervEntitySource{}
}

// update is called by Collect with the health of the last WMI round.
func (s *hypervEntitySource) update(ok bool) {
	s.mu.Lock()
	s.ok = ok
	s.once = true
	s.mu.Unlock()
}

// Observe returns the cached entity observation. ok=false before the first
// successful collection (nothing to report yet) — the detector reuses the
// previous observation when ok=false (audit D3 guard).
func (s *hypervEntitySource) Observe() (entity.Observation, bool) {
	s.mu.Lock()
	ok := s.ok
	once := s.once
	s.mu.Unlock()

	if !once {
		return entity.Observation{}, false
	}

	obs := entity.Observation{
		Entities: []entity.Entity{
			{
				Type: entityTypeServiceInstance,
				ID:   map[string]any{entityIDServiceInstanceID: hypervInstanceID},
				Attributes: map[string]any{
					"service.name":      "hyperv",
					"service.namespace": "virtualization",
				},
			},
		},
	}
	return obs, ok
}
