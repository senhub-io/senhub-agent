package zookeeper

import (
	"sync"

	"senhub-agent.go/internal/agent/services/entity"
)

// entityObserver implements entity.Source for the zookeeper probe.
// It reports the monitored ZooKeeper node as a coordination.service entity
// and updates liveness and version after each Collect cycle.
type entityObserver struct {
	mu  sync.Mutex
	obs entity.Observation
	ok  bool
}

// Observe implements entity.Source.
// Returns ok=false when the last collection failed (transient outage):
// the detector reuses the previous good observation rather than
// emitting a delete — audit D3 contract.
func (e *entityObserver) Observe() (entity.Observation, bool) {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.obs, e.ok
}

// setUp records the result of the last Collect cycle.
// addr and port are the immutable identity of the ZooKeeper node.
// version is the ZooKeeper version string from the mntr response; pass "" when unknown.
func (e *entityObserver) setUp(addr string, port int, up bool, version string) {
	if !up {
		e.mu.Lock()
		e.ok = false
		e.mu.Unlock()
		return
	}

	id := map[string]any{
		"server.address":       addr,
		"server.port":          port,
		"coordination.system":  "zookeeper",
	}

	attrs := map[string]any{}
	if version != "" {
		attrs["version"] = version
	}

	obs := entity.Observation{
		Entities: []entity.Entity{
			{
				Type:       "coordination.service",
				ID:         id,
				Attributes: attrs,
			},
		},
	}

	e.mu.Lock()
	e.obs = obs
	e.ok = true
	e.mu.Unlock()
}
