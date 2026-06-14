package chrony

import (
	"sync"

	"senhub-agent.go/internal/agent/services/entity"
)

// chronyEntitySource feeds the entity rail with a single ntp.server entity
// for the local chrony daemon. Identity (server.address=localhost) is fixed at
// construction time — chrony is always local; there is no configurable host.
// Reachability is updated by the collect cycle: setReachable(true) after a
// successful chronyc tracking run, setReachable(false) on any subprocess error.
type chronyEntitySource struct {
	id    map[string]any
	mu    sync.RWMutex
	up    bool
	attrs map[string]any
}

func newChronyEntitySource() *chronyEntitySource {
	return &chronyEntitySource{
		id: map[string]any{
			"server.address": "localhost",
		},
	}
}

// setReachable is called by the collect cycle: true when chronyc returned
// valid output, false on any subprocess error. version may be empty.
func (s *chronyEntitySource) setReachable(up bool, version string) {
	s.mu.Lock()
	s.up = up
	if up && version != "" {
		s.attrs = map[string]any{"version": version}
	}
	s.mu.Unlock()
}

// Observe implements entity.Source. Returns ok=false until the first
// successful collection cycle so a transient startup error does not
// immediately delete the entity in the consumer.
func (s *chronyEntitySource) Observe() (entity.Observation, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if !s.up {
		return entity.Observation{}, false
	}
	obs := entity.Observation{
		Entities: []entity.Entity{{
			Type:       "ntp.server",
			ID:         s.id,
			Attributes: s.attrs,
		}},
	}
	return obs, true
}
