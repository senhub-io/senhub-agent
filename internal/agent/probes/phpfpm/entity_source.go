package phpfpm

import (
	"sync"

	"senhub-agent.go/internal/agent/services/entity"
)

// phpfpmEntitySource feeds the entity rail with a single runtime.php_fpm
// entity for each configured pool endpoint. Identity (server.address +
// server.port) is derived from the Endpoint URL at construction time and
// never changes. Reachability is updated by the collect cycle.
type phpfpmEntitySource struct {
	id   map[string]any
	mu   sync.RWMutex
	up   bool
	attrs map[string]any
}

func newPhpfpmEntitySource(addr string, port int) *phpfpmEntitySource {
	return &phpfpmEntitySource{
		id: map[string]any{
			"server.address": addr,
			"server.port":    int64(port),
		},
	}
}

// setReachable is called by the collect cycle: true when the status page
// responded, false on any fetch error. version may be empty when unknown.
func (s *phpfpmEntitySource) setReachable(up bool, version string) {
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
func (s *phpfpmEntitySource) Observe() (entity.Observation, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if !s.up {
		return entity.Observation{}, false
	}
	obs := entity.Observation{
		Entities: []entity.Entity{{
			Type:       "runtime.php_fpm",
			ID:         s.id,
			Attributes: s.attrs,
		}},
	}
	return obs, true
}
