package cassandra

import (
	"sync"

	"senhub-agent.go/internal/agent/services/entity"
)

// entityObserver implements entity.Source for the cassandra probe.
// It reports the monitored Cassandra node as a db entity following the
// Toise v0.5.0 strict contract and updates liveness and version after
// each Collect cycle.
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
// addr and port must be the immutable identity of the Cassandra node
// (extracted from jolokia_url at construction time).
// version is the Cassandra release version string; pass "" when unknown.
func (e *entityObserver) setUp(addr string, port string, up bool, version string) {
	if !up {
		e.mu.Lock()
		e.ok = false
		e.mu.Unlock()
		return
	}

	instanceID := "cassandra://" + addr + ":" + port

	attrs := map[string]any{
		"db.system.name": "cassandra",
		"server.address": addr,
		"server.port":    port,
	}
	if version != "" {
		attrs["db.system.version"] = version
	}

	obs := entity.Observation{
		Entities: []entity.Entity{
			{
				Type:       "db",
				ID:         map[string]any{"db.instance.id": instanceID},
				Attributes: attrs,
			},
		},
	}

	e.mu.Lock()
	e.obs = obs
	e.ok = true
	e.mu.Unlock()
}
