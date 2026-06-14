package memcached

import (
	"sync"

	"senhub-agent.go/internal/agent/services/entity"
)

const (
	entityTypeMemcached = "db.memcached"
	idKeyServerAddress  = "server.address"
	idKeyServerPort     = "server.port"
)

// memcachedEntitySource feeds the entity rail with the monitored Memcached
// instance. Observe() never blocks: it returns the last cached state. ok=false
// before the first successful collection cycle so the detector does not treat
// an initial empty cache as "server deleted".
type memcachedEntitySource struct {
	id map[string]any

	mu    sync.Mutex
	up    bool
	attrs map[string]any
	ready bool
}

func newMemcachedEntitySource(host string, port int) *memcachedEntitySource {
	return &memcachedEntitySource{
		id: map[string]any{
			idKeyServerAddress: host,
			idKeyServerPort:    int64(port),
		},
	}
}

// setReachable updates the reachability state and optional descriptive
// attributes. Called from Collect() after each stats fetch attempt.
func (s *memcachedEntitySource) setReachable(up bool, version string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.up = up
	s.ready = true
	if up && version != "" {
		s.attrs = map[string]any{"version": version}
	} else if !up {
		s.attrs = nil
	}
}

// Observe returns the current entity state. Non-blocking; safe to call from
// the detector goroutine. Returns ok=false until the first Collect cycle
// completes (to distinguish "not yet observed" from "server gone").
func (s *memcachedEntitySource) Observe() (entity.Observation, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.ready {
		return entity.Observation{}, false
	}
	if !s.up {
		return entity.Observation{}, true
	}
	obs := entity.Observation{
		Entities: []entity.Entity{
			{
				Type:       entityTypeMemcached,
				ID:         s.id,
				Attributes: s.attrs,
			},
		},
	}
	return obs, true
}
