package opensearch

import (
	"strconv"
	"sync"

	"senhub-agent.go/internal/agent/services/entity"
)

// opensearchEntitySource feeds the entity rail with the monitored OpenSearch
// node as a "db" entity (Toise v0.5.0 strict contract). Observe is
// non-blocking and returns the last reachable state. Before the first
// successful Collect cycle ok=false so the detector does not emit a phantom
// entity that immediately expires.
type opensearchEntitySource struct {
	instanceID string
	addr       string
	port       int
	mu         sync.RWMutex
	up         bool
	// attrs holds mutable descriptive attributes (version). nil until the first
	// successful response.
	attrs map[string]any
}

// newOpensearchEntitySource creates the entity source from the parsed endpoint
// components. addr and port are immutable identity keys for this instance.
func newOpensearchEntitySource(addr string, port int) *opensearchEntitySource {
	return &opensearchEntitySource{
		instanceID: "opensearch://" + addr + ":" + strconv.FormatInt(int64(port), 10),
		addr:       addr,
		port:       port,
	}
}

// setReachable updates liveness and, when version is non-empty, the version
// attribute. Called from Collect on every cycle.
func (s *opensearchEntitySource) setReachable(up bool, version string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.up = up
	if up {
		attrs := map[string]any{
			"db.system.name": "opensearch",
			"server.address": s.addr,
			"server.port":    s.port,
		}
		if version != "" {
			attrs["db.system.version"] = version
		}
		s.attrs = attrs
	}
}

// Observe implements entity.Source. Returns the db entity when the node was
// reachable on the last collect cycle, ok=false otherwise.
func (s *opensearchEntitySource) Observe() (entity.Observation, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if !s.up {
		return entity.Observation{}, false
	}
	return entity.Observation{
		Entities: []entity.Entity{{
			Type:       "db",
			ID:         map[string]any{"db.instance.id": s.instanceID},
			Attributes: s.attrs,
		}},
	}, true
}
