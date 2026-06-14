package rabbitmq

import (
	"strconv"
	"sync"

	"senhub-agent.go/internal/agent/services/entity"
)

// rabbitmqEntitySource feeds the entity rail for the rabbitmq probe.
// It reports the broker as a service.instance entity (Toise v0.5.0 contract).
//
// The source is registered on OnStart and unregistered on OnShutdown.
// Observe is non-blocking and returns the last cached snapshot.
type rabbitmqEntitySource struct {
	instanceID string
	attrs      map[string]any

	mu sync.RWMutex
	up bool
}

// newRabbitmqEntitySource constructs the entity source from the parsed endpoint
// components. addr is the hostname and port is the management port as int64.
func newRabbitmqEntitySource(addr string, port int64) *rabbitmqEntitySource {
	instanceID := "rabbitmq://" + addr + ":" + strconv.FormatInt(port, 10)
	return &rabbitmqEntitySource{
		instanceID: instanceID,
		attrs: map[string]any{
			"service.name":   "rabbitmq",
			"server.address": addr,
			"server.port":    port,
		},
	}
}

// setReachable marks the broker as reachable or not for the next Observe call.
// An optional version string enriches the service.version attribute when the
// broker is reachable.
func (s *rabbitmqEntitySource) setReachable(up bool, version string) {
	s.mu.Lock()
	s.up = up
	if up && version != "" {
		attrs := make(map[string]any, len(s.attrs)+1)
		for k, v := range s.attrs {
			attrs[k] = v
		}
		attrs["service.version"] = version
		s.attrs = attrs
	}
	s.mu.Unlock()
}

// Observe implements entity.Source. It returns the rabbitmq service.instance
// entity when the management API is reachable, or (_, false) on a transient
// failure so the detector keeps the last good snapshot alive.
func (s *rabbitmqEntitySource) Observe() (entity.Observation, bool) {
	s.mu.RLock()
	up := s.up
	attrs := s.attrs
	s.mu.RUnlock()

	if !up {
		return entity.Observation{}, false
	}
	return entity.Observation{
		Entities: []entity.Entity{{
			Type:       "service.instance",
			ID:         map[string]any{"service.instance.id": s.instanceID},
			Attributes: attrs,
		}},
	}, true
}
