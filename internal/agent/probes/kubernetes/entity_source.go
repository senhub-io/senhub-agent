package kubernetes

import (
	"sync"

	"senhub-agent.go/internal/agent/services/agentstate"
	"senhub-agent.go/internal/agent/services/entity"
)

// k8sEntitySource reports the Kubernetes cluster as a service.instance entity.
// The entity is minimal: one service.instance with the cluster endpoint as its
// ID. Node entities are potential future work once the topology contract with
// Toise is established.
type k8sEntitySource struct {
	mu              sync.Mutex
	clusterEndpoint string
	ready           bool
}

func newK8sEntitySource(clusterEndpoint string) *k8sEntitySource {
	return &k8sEntitySource{clusterEndpoint: clusterEndpoint, ready: true}
}

// setClusterEndpoint refines the cluster identity once the live client config
// resolves the API server host in OnStart. The endpoint is created with a
// best-effort value at construction so EntitySource() is non-NoOp without a
// live cluster (#482); OnStart narrows it to the real API server address.
func (s *k8sEntitySource) setClusterEndpoint(clusterEndpoint string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.clusterEndpoint = clusterEndpoint
}

// Observe returns the cluster entity. Always ok=true once initialised.
func (s *k8sEntitySource) Observe() (entity.Observation, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.ready {
		return entity.Observation{}, false
	}

	clusterID := "kubernetes://" + s.clusterEndpoint
	svcID := map[string]any{"service.instance.id": clusterID}
	obs := entity.Observation{
		Entities: []entity.Entity{
			{
				Type: "service.instance",
				ID:   svcID,
				Attributes: map[string]any{
					"service.name":    "kubernetes",
					"cluster.address": s.clusterEndpoint,
				},
			},
		},
	}
	// monitors edge: agent → cluster, anchoring the entity to the agent's
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

// registerEntitySource is a thin indirection to allow unit tests to inject a
// no-op. In production it calls entity.RegisterSource.
var registerEntitySource = entity.RegisterSource
