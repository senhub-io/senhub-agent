package swarm

import (
	"sync"

	"senhub-agent.go/internal/agent/services/agentstate"
	"senhub-agent.go/internal/agent/services/entity"
)

// The swarm cluster as a graph entity.
//
// What is emitted: the cluster itself, as a service.instance keyed on the
// Swarm cluster id. That id is assigned by Swarm at `docker swarm init` and
// survives every node joining, leaving or being promoted — it is what the
// cluster says about itself, not what an observer says about it (ADR 0018).
//
// What is deliberately NOT emitted, and why each would be a mistake:
//
//   - Swarm NODES as `host` entities. A node reports its hostname, never its
//     machine-id, and the agent's own host entity is keyed on machine-id. A
//     host minted from a hostname would be a second, non-converging node for a
//     machine that already has one — the exact duplicate the Kubernetes probe
//     avoided by refusing to emit a node without its MachineID. The right fix
//     is an agent inside the node, which then emits the real host itself.
//
//   - Overlay NETWORKS as entities. There is no registered type for a network
//     segment in the consumer's vocabulary — network.device, .interface,
//     .address, .route and .endpoint all name something else. Inventing a type
//     name would have it rejected at the frontier and silently dropped, which
//     is worse than not sending it. Asking the consumer to register one is the
//     open question; until it is answered the segments live as metric labels,
//     where they are queryable but not traversable.
//
//   - Swarm SERVICES as service.instance entities. Tempting, since a service
//     is exactly that — but its tasks are containers the `docker` probe
//     already emits on each node, and a service entity built here would have
//     no relation to them (this probe cannot see container ids per node with
//     any stability). A service that runs on nothing is a floating node; the
//     relation has to exist before the entity is worth emitting.
type entitySource struct {
	mu        sync.Mutex
	clusterID string
	name      string
	ready     bool
	nodes     int
	services  int
}

func newEntitySource() *entitySource { return &entitySource{} }

// update records the cluster identity after a successful manager cycle.
// Called with an empty clusterID when the node cannot see the cluster, which
// withdraws the entity rather than freezing the last good snapshot.
func (s *entitySource) update(clusterID, name string, nodes, services int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.clusterID = clusterID
	s.name = name
	s.nodes = nodes
	s.services = services
	s.ready = clusterID != ""
}

// Observe returns the cluster entity and its monitors edge.
func (s *entitySource) Observe() (entity.Observation, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.ready {
		return entity.Observation{}, false
	}

	svcID := map[string]any{"service.instance.id": "swarm://" + s.clusterID}
	attrs := map[string]any{
		"service.name":        "docker-swarm",
		"swarm.cluster.id":    s.clusterID,
		"swarm.node.count":    int64(s.nodes),
		"swarm.service.count": int64(s.services),
	}
	if s.name != "" {
		attrs["swarm.cluster.name"] = s.name
	}

	obs := entity.Observation{
		Entities: []entity.Entity{{
			Type:       entity.TypeServiceInstance,
			ID:         svcID,
			Attributes: attrs,
		}},
	}

	// monitors edge: agent → cluster. Without it the cluster floats with no
	// path to any host, which the consumer's anti-orphan guard drops. Skipped
	// when the agent id is unset (entity emission off), since a From that never
	// materialises is buffered and then discarded.
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
