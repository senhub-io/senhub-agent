package apache

import (
	"strconv"
	"sync"

	"senhub-agent.go/internal/agent/services/entity"
)

// apacheEntitySource feeds the entity rail. Observe() never blocks: it returns
// the last cached snapshot. The cache is refreshed from each successful
// mod_status fetch in Collect(). ok=false before the first successful fetch so
// the detector does not treat an empty initial cache as "server deleted".
type apacheEntitySource struct {
	instanceID string
	host       string
	port       int64
	mu         sync.Mutex
	cache      entity.Observation
	ready      bool
}

// newApacheEntitySource constructs the source from the resolved host and port
// extracted from the probe endpoint URL.
func newApacheEntitySource(addr string, port int) *apacheEntitySource {
	return &apacheEntitySource{
		instanceID: "apache://" + addr + ":" + strconv.FormatInt(int64(port), 10),
		host:       addr,
		port:       int64(port),
	}
}

// setReachable updates the cached entity observation. up=true replaces the
// cache with a live entity; up=false clears it (empty observation with ok=true
// signals "server gone" — detector emits a delete). version is included in
// the attributes when non-empty.
func (s *apacheEntitySource) setReachable(up bool, version string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !up {
		s.cache = entity.Observation{}
		s.ready = true
		return
	}
	attrs := map[string]any{
		"service.name":   "apache",
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
func (s *apacheEntitySource) Observe() (entity.Observation, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.cache, s.ready
}
