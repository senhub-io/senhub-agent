package consul

import (
	"strconv"
	"sync"

	"senhub-agent.go/internal/agent/services/entity"
)

// consulEntitySource feeds the entity rail with the Consul agent instance.
// Observe() never blocks: it returns the last cached snapshot. The cache is
// refreshed on each successful Collect() cycle. ok=false before the first
// successful fetch so the detector does not treat an empty initial cache as
// "server deleted".
type consulEntitySource struct {
	instanceID string
	host       string
	port       int64

	mu    sync.Mutex
	cache entity.Observation
	ready bool
}

// newConsulEntitySource constructs the source from the resolved host and port
// extracted from the probe endpoint URL.
func newConsulEntitySource(host string, port int) *consulEntitySource {
	return &consulEntitySource{
		instanceID: "consul://" + host + ":" + strconv.FormatInt(int64(port), 10),
		host:       host,
		port:       int64(port),
	}
}

// setReachable updates the cached entity observation. up=true replaces the
// cache with a live entity; up=false clears it (empty observation with
// ok=true signals "server gone" — the detector emits a delete event).
// version is included in the attributes when non-empty.
func (s *consulEntitySource) setReachable(up bool, version string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !up {
		s.cache = entity.Observation{}
		s.ready = true
		return
	}
	attrs := map[string]any{
		"service.name":   "consul",
		"server.address": s.host,
		"server.port":    s.port,
	}
	if version != "" {
		attrs["service.version"] = version
	}
	s.cache = entity.Observation{
		Entities: []entity.Entity{{
			Type:       "service.instance",
			ID:         map[string]any{"service.instance.id": s.instanceID},
			Attributes: attrs,
		}},
	}
	s.ready = true
}

// Observe returns the latest cached entity snapshot. Non-blocking; safe to
// call from the detector goroutine. Returns ok=false until the first Collect()
// cycle completes (success or failure).
func (s *consulEntitySource) Observe() (entity.Observation, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.cache, s.ready
}
