package phpfpm

import (
	"strconv"
	"sync"

	"senhub-agent.go/internal/agent/services/entity"
)

// phpfpmEntitySource feeds the entity rail with a single service.instance
// entity for each configured pool endpoint. The instance ID
// ("php-fpm://<host>:<port>") is derived from the Endpoint URL at
// construction time and never changes. Reachability is updated by the
// collect cycle.
type phpfpmEntitySource struct {
	instanceID string
	mu         sync.RWMutex
	up         bool
	attrs      map[string]any
}

func newPhpfpmEntitySource(addr string, port int) *phpfpmEntitySource {
	instanceID := "php-fpm://" + addr + ":" + strconv.Itoa(port)
	return &phpfpmEntitySource{
		instanceID: instanceID,
		attrs: map[string]any{
			"service.name":   "php-fpm",
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
	if up {
		attrs := map[string]any{
			"service.name":   s.attrs["service.name"],
			"server.address": s.attrs["server.address"],
			"server.port":    s.attrs["server.port"],
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
func (s *phpfpmEntitySource) Observe() (entity.Observation, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if !s.up {
		return entity.Observation{}, false
	}
	return entity.Observation{
		Entities: []entity.Entity{{
			Type:       "service.instance",
			ID:         map[string]any{"service.instance.id": s.instanceID},
			Attributes: s.attrs,
		}},
	}, true
}
