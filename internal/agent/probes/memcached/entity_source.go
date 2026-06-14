package memcached

import (
	"strconv"
	"sync"

	"senhub-agent.go/internal/agent/services/entity"
)

// memcachedEntitySource feeds the entity rail with the monitored Memcached
// instance. Observe() never blocks: it returns the last cached state. ok=false
// before the first successful collection cycle so the detector does not treat
// an initial empty cache as "server deleted".
type memcachedEntitySource struct {
	instanceID string

	mu    sync.RWMutex
	up    bool
	attrs map[string]any
	ready bool

	host string
	port int64
}

func newMemcachedEntitySource(host string, port int) *memcachedEntitySource {
	return &memcachedEntitySource{
		instanceID: "memcached://" + host + ":" + strconv.FormatInt(int64(port), 10),
		host:       host,
		port:       int64(port),
	}
}

// setReachable updates the reachability state and optional descriptive
// attributes. Called from Collect() after each stats fetch attempt.
func (s *memcachedEntitySource) setReachable(up bool, version string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.up = up
	s.ready = true
	if up {
		s.attrs = map[string]any{
			"db.system.name":    "memcached",
			"server.address":    s.host,
			"server.port":       s.port,
			"db.system.version": version,
		}
	} else {
		s.attrs = nil
	}
}

// Observe returns the current entity state. Non-blocking; safe to call from
// the detector goroutine. Returns ok=false until the first Collect cycle
// completes (to distinguish "not yet observed" from "server gone").
func (s *memcachedEntitySource) Observe() (entity.Observation, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if !s.ready {
		return entity.Observation{}, false
	}
	if !s.up {
		return entity.Observation{}, true
	}
	return entity.Observation{
		Entities: []entity.Entity{{
			Type:       "db",
			ID:         map[string]any{"db.instance.id": s.instanceID},
			Attributes: s.attrs,
		}},
	}, true
}
