package chrony

import (
	"sync"

	"senhub-agent.go/internal/agent/services/entity"
)

// chronyEntitySource feeds the entity rail with a single service.instance entity
// for the local chrony daemon. Identity is fixed at construction time — chrony
// is always local; there is no configurable host or port.
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
			"service.instance.id": "chrony://localhost",
		},
	}
}

// setReachable is called by the collect cycle: true when chronyc returned
// valid output, false on any subprocess error. version may be empty.
func (s *chronyEntitySource) setReachable(up bool, version string) {
	s.mu.Lock()
	s.up = up
	if up {
		attrs := map[string]any{
			"service.name":   "chrony",
			"server.address": "localhost",
		}
		if version != "" {
			attrs["service.version"] = version
		}
		s.attrs = attrs
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
	return entity.Observation{
		Entities: []entity.Entity{{
			Type:       "service.instance",
			ID:         s.id,
			Attributes: s.attrs,
		}},
	}, true
}
