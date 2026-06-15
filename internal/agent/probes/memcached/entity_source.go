package memcached

import (
	"strconv"
	"sync"

	"senhub-agent.go/internal/agent/services/agentstate"
	"senhub-agent.go/internal/agent/services/entity"
)

// memcachedEntitySource feeds the entity rail with the monitored Memcached
// instance as a `db` entity. Identity rules (Toise db contract):
//   - instance_name operator config key → verbatim, pinned at construction.
//   - else host:port (Memcached exposes no stable server-side id, so this is
//     the documented db degraded fallback, also pinned at construction —
//     no re-keying risk).
//
// Observe() never blocks: it returns the last cached state. ok=false before
// the first successful collection cycle so the detector does not treat an
// initial empty cache as "server deleted".
type memcachedEntitySource struct {
	// instanceID is the pinned, immutable db.instance.id for this probe.
	instanceID string

	mu    sync.RWMutex
	up    bool
	attrs map[string]any
	ready bool

	host string
	port int64
}

// newMemcachedEntitySource constructs the entity source, pinning the
// db.instance.id immediately:
//   - instanceName (operator "instance_name" config key) if non-empty;
//   - else host:port (Memcached has no stable server-reported id).
func newMemcachedEntitySource(host string, port int, instanceName string) *memcachedEntitySource {
	id := instanceName
	if id == "" {
		id = host + ":" + strconv.FormatInt(int64(port), 10)
	}
	return &memcachedEntitySource{
		instanceID: id,
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
// When the db is reachable, also emits a monitors relation from the agent's
// own service.instance entity to this db entity.
func (s *memcachedEntitySource) Observe() (entity.Observation, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if !s.ready {
		return entity.Observation{}, false
	}
	if !s.up {
		return entity.Observation{}, true
	}

	obs := entity.Observation{
		Entities: []entity.Entity{{
			Type:       "db",
			ID:         map[string]any{"db.instance.id": s.instanceID},
			Attributes: s.attrs,
		}},
	}

	if agentID := agentstate.GetAgentInstanceID(); agentID != "" {
		obs.Relations = append(obs.Relations, entity.Relation{
			Type:     "monitors",
			FromType: "service.instance",
			FromID:   map[string]any{"service.instance.id": agentID},
			ToType:   "db",
			ToID:     map[string]any{"db.instance.id": s.instanceID},
		})
	}

	return obs, true
}
