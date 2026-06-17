package unifi

import (
	"sync"

	"senhub-agent.go/internal/agent/services/agentstate"
	"senhub-agent.go/internal/agent/services/entity"
)

// Entity rail (#185): the monitored UniFi Controller as a service.instance
// entity, identified by its endpoint URL. The probe is the observer; the
// controller is the observed service. Identity is exact and immutable
// (the endpoint the operator configured), so a backend joins the unifi
// metrics to this entity via the same service.instance.id.
const (
	entityTypeServiceInstance = "service.instance"
	idKeyServiceInstanceID    = "service.instance.id"
)

// unifiEntitySource reports the monitored controller as a single
// service.instance entity. Observe() is non-blocking and serves the last
// reachability state set by the probe's Collect cycle; ok=false before the
// first cycle so an unprobed controller is not reported as deleted.
type unifiEntitySource struct {
	id string

	mu        sync.Mutex
	observed  bool
	reachable bool
}

func newEntitySource(endpoint string) *unifiEntitySource {
	return &unifiEntitySource{id: "unifi://" + endpoint}
}

// markReachable records the outcome of a Collect cycle. The entity is
// emitted with a reachable attribute so a consumer sees the controller's
// liveness without parsing metrics.
func (s *unifiEntitySource) markReachable(reachable bool) {
	s.mu.Lock()
	s.observed = true
	s.reachable = reachable
	s.mu.Unlock()
}

// Observe returns the controller entity. ok=false until the first cycle
// has run (nothing observed yet is not "everything deleted").
func (s *unifiEntitySource) Observe() (entity.Observation, bool) {
	s.mu.Lock()
	observed := s.observed
	reachable := s.reachable
	s.mu.Unlock()

	if !observed {
		return entity.Observation{}, false
	}

	svcID := map[string]any{idKeyServiceInstanceID: s.id}
	obs := entity.Observation{
		Entities: []entity.Entity{
			{
				Type:       entityTypeServiceInstance,
				ID:         svcID,
				Attributes: map[string]any{"unifi.reachable": reachable},
			},
		},
	}
	// monitors edge: agent → controller, anchoring the entity to the agent's
	// monitoring subgraph (else it floats — #506). Emitted only when the agent
	// id is available; a non-materialised From would be buffered then dropped.
	if agentID := agentstate.GetAgentInstanceID(); agentID != "" {
		obs.Relations = append(obs.Relations, entity.Relation{
			Type:     "monitors",
			FromType: entityTypeServiceInstance,
			FromID:   map[string]any{idKeyServiceInstanceID: agentID},
			ToType:   entityTypeServiceInstance,
			ToID:     svcID,
		})
	}
	return obs, true
}
