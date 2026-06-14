package consul

import (
	"sync"

	"senhub-agent.go/internal/agent/services/entity"
)

// consulEntitySource feeds the entity rail for a Consul probe. Observe()
// never blocks: it returns the last cached snapshot set during Collect.
//
// Entity model:
//   - service_mesh.node  — the Consul agent being monitored
//                          ID = {server.address, server.port, service_mesh.system}
//   - service.instance   — one per healthy registered service
//                          ID = {server.address, service_mesh.system, service.name}
//
// Relations:
//   - service_mesh.node  registers  service.instance
type consulEntitySource struct {
	// immutable identity fields set at construction time
	nodeID map[string]any

	mu       sync.RWMutex
	up       bool
	version  string
	services []string
}

// newConsulEntitySource builds the entity source. addr and port are extracted
// from the configured endpoint URL before construction.
func newConsulEntitySource(addr, port string) *consulEntitySource {
	return &consulEntitySource{
		nodeID: map[string]any{
			"server.address":      addr,
			"server.port":         port,
			"service_mesh.system": "consul",
		},
	}
}

// setReachable updates the liveness flag and optional version string.
// Called from Collect on success or failure.
func (s *consulEntitySource) setReachable(up bool, version string) {
	s.mu.Lock()
	s.up = up
	if version != "" {
		s.version = version
	}
	s.mu.Unlock()
}

// updateSnapshot replaces the list of healthy service names discovered during
// a Collect cycle. Called after a successful health-state fetch so Observe
// can build registers relations.
func (s *consulEntitySource) updateSnapshot(services []string) {
	s.mu.Lock()
	s.services = services
	s.mu.Unlock()
}

// Observe returns the full current observation. Returns ok=false when the
// agent was unreachable on the last Collect so the detector keeps the
// previous snapshot rather than emitting deletes on a transient failure.
func (s *consulEntitySource) Observe() (entity.Observation, bool) {
	s.mu.RLock()
	up := s.up
	version := s.version
	services := s.services
	nodeID := s.nodeID
	s.mu.RUnlock()

	if !up {
		return entity.Observation{}, false
	}

	addr, _ := nodeID["server.address"].(string)
	meshSystem, _ := nodeID["service_mesh.system"].(string)

	nodeAttrs := map[string]any{}
	if version != "" {
		nodeAttrs["version"] = version
	}

	obs := entity.Observation{
		Entities: []entity.Entity{
			{
				Type:       "service_mesh.node",
				ID:         nodeID,
				Attributes: nodeAttrs,
			},
		},
	}

	// Emit service.instance entities and node→service registers relations.
	seen := make(map[string]bool, len(services))
	for _, svc := range services {
		if svc == "" || seen[svc] {
			continue
		}
		seen[svc] = true
		svcID := map[string]any{
			"server.address":      addr,
			"service_mesh.system": meshSystem,
			"service.name":        svc,
		}
		obs.Entities = append(obs.Entities, entity.Entity{
			Type: "service.instance",
			ID:   svcID,
		})
		obs.Relations = append(obs.Relations, entity.Relation{
			Type:     "registers",
			FromType: "service_mesh.node",
			FromID:   nodeID,
			ToType:   "service.instance",
			ToID:     svcID,
		})
	}

	return obs, true
}
