package modbus

import (
	"sync"

	"senhub-agent.go/internal/agent/services/agentstate"
	"senhub-agent.go/internal/agent/services/entity"
)

// modbusEntitySource reports the Modbus TCP target as a service.instance
// entity so Toise can discover it. The observation is initially empty
// (ok=false) and becomes live after the first successful Collect cycle.
//
// Entity shape (frozen with Toise / ADR 0022):
//
//	type: "service.instance"
//	id:   {"service.instance.id": "modbus://<host>:<port>"}
type modbusEntitySource struct {
	instanceID string

	mu   sync.Mutex
	live bool
}

func newModbusEntitySource(instanceID string) *modbusEntitySource {
	return &modbusEntitySource{instanceID: instanceID}
}

// markLive signals that the Modbus target was reached this cycle.
// Called by the probe after a successful Connect; makes Observe return ok=true.
func (s *modbusEntitySource) markLive() {
	s.mu.Lock()
	s.live = true
	s.mu.Unlock()
}

// Observe returns the service.instance entity for the polled Modbus device.
// ok=false before the first successful connection (nothing to report yet).
func (s *modbusEntitySource) Observe() (entity.Observation, bool) {
	s.mu.Lock()
	live := s.live
	s.mu.Unlock()

	if !live {
		return entity.Observation{}, false
	}

	svcID := map[string]any{"service.instance.id": s.instanceID}
	obs := entity.Observation{
		Entities: []entity.Entity{
			{
				Type: "service.instance",
				ID:   svcID,
			},
		},
	}
	// monitors edge: agent → target, anchoring the entity to the agent's
	// monitoring subgraph (else it floats — #506). Emitted only when the agent
	// id is available; a non-materialised From would be buffered then dropped.
	if agentID := agentstate.GetAgentInstanceID(); agentID != "" {
		obs.Relations = append(obs.Relations, entity.Relation{
			Type:     "monitors",
			FromType: "service.instance",
			FromID:   map[string]any{"service.instance.id": agentID},
			ToType:   "service.instance",
			ToID:     svcID,
		})
	}
	return obs, true
}
