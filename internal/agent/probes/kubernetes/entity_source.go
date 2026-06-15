package kubernetes

import (
	"sync"

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

// Observe returns the cluster entity. Always ok=true once initialised.
func (s *k8sEntitySource) Observe() (entity.Observation, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.ready {
		return entity.Observation{}, false
	}

	clusterID := "kubernetes://" + s.clusterEndpoint
	obs := entity.Observation{
		Entities: []entity.Entity{
			{
				Type: "service.instance",
				ID:   map[string]any{"service.instance.id": clusterID},
				Attributes: map[string]any{
					"service.name":    "kubernetes",
					"cluster.address": s.clusterEndpoint,
				},
			},
		},
	}
	return obs, true
}

// registerEntitySource is a thin indirection to allow unit tests to inject a
// no-op. In production it calls entity.RegisterSource.
var registerEntitySource = entity.RegisterSource
