package envoy

import (
	"sync"

	"senhub-agent.go/internal/agent/services/entity"
)

// envoyEntitySource feeds the entity rail with the single Envoy instance this
// probe monitors. Entity type: proxy. Identity is immutable (server.address,
// server.port, proxy.type). Reachability is updated by Collect after each
// scrape attempt so Toise sees accurate liveness (audit D3).
type envoyEntitySource struct {
	id map[string]any

	mu    sync.RWMutex
	up    bool
	attrs map[string]any
}

func newEnvoyEntitySource(addr, port string) *envoyEntitySource {
	return &envoyEntitySource{
		id: map[string]any{
			"server.address": addr,
			"server.port":    port,
			"proxy.type":     "envoy",
		},
	}
}

// setReachable updates the probe's liveness state after each scrape.
// Call with up=true on success (passing the Envoy version when available),
// up=false on any failure so Toise does not receive stale state events.
func (s *envoyEntitySource) setReachable(up bool, version string) {
	s.mu.Lock()
	s.up = up
	if up && version != "" {
		s.attrs = map[string]any{"version": version}
	}
	s.mu.Unlock()
}

// Observe implements entity.Source. Returns ok=false when Envoy was not
// reachable on the last cycle, so the detector reuses its cached snapshot
// rather than emitting a delete event on a transient outage (audit D3).
func (s *envoyEntitySource) Observe() (entity.Observation, bool) {
	s.mu.RLock()
	up, attrs, id := s.up, s.attrs, s.id
	s.mu.RUnlock()
	if !up {
		return entity.Observation{}, false
	}
	return entity.Observation{
		Entities: []entity.Entity{{
			Type:       "proxy",
			ID:         id,
			Attributes: attrs,
		}},
	}, true
}
