package opensearch

import (
	"sync"

	"senhub-agent.go/internal/agent/services/entity"
)

// opensearchEntitySource feeds the entity rail with the monitored OpenSearch
// node as a search.engine entity. Observe is non-blocking and returns the
// last reachable state. Before the first successful Collect cycle ok=false so
// the detector does not emit a phantom entity that immediately expires.
type opensearchEntitySource struct {
	id  map[string]any
	mu  sync.RWMutex
	up  bool
	// attrs holds mutable descriptive attributes (version). nil until the first
	// successful response.
	attrs map[string]any
}

// newOpensearchEntitySource creates the entity source from the parsed endpoint
// components. addr and port are immutable identity keys for this instance.
func newOpensearchEntitySource(addr string, port int) *opensearchEntitySource {
	return &opensearchEntitySource{
		id: map[string]any{
			"server.address":     addr,
			"server.port":        port,
			"search.engine.type": "opensearch",
		},
	}
}

// setReachable updates liveness and, when version is non-empty, the version
// attribute. Called from Collect on every cycle.
func (s *opensearchEntitySource) setReachable(up bool, version string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.up = up
	if version != "" {
		s.attrs = map[string]any{"version": version}
	}
}

// Observe implements entity.Source. Returns the search.engine entity when the
// node was reachable on the last collect cycle, ok=false otherwise.
func (s *opensearchEntitySource) Observe() (entity.Observation, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if !s.up {
		return entity.Observation{}, false
	}
	obs := entity.Observation{
		Entities: []entity.Entity{{
			Type:       "search.engine",
			ID:         s.id,
			Attributes: s.attrs,
		}},
	}
	return obs, true
}
