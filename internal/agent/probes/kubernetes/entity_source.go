package kubernetes

import (
	"net"
	"strings"
	"sync"

	"senhub-agent.go/internal/agent/probes/dbcommon"
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
	hostID          string // agent host id, target of the local-target runs_on edge
}

func newK8sEntitySource(clusterEndpoint string) *k8sEntitySource {
	return &k8sEntitySource{clusterEndpoint: clusterEndpoint, ready: true, hostID: dbcommon.HostID()}
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
	// runs_on edge: cluster → host when the API server is local (loopback) — anchors
	// an on-host cluster to the host it runs on instead of leaving it floating with
	// only its monitors anchor. A remote API server yields no edge.
	if rel, ok := entity.LocalRunsOn("service.instance", svcID, hostFromEndpoint(s.clusterEndpoint), s.hostID); ok {
		obs.Relations = append(obs.Relations, rel)
	}
	return obs, true
}

// hostFromEndpoint strips an optional ":port" from the scheme-less cluster
// endpoint (host or host:port) so the runs_on gate sees a bare host. Returns the
// input unchanged when there is no port.
func hostFromEndpoint(endpoint string) string {
	if !strings.Contains(endpoint, ":") {
		return endpoint
	}
	if host, _, err := net.SplitHostPort(endpoint); err == nil {
		return host
	}
	return endpoint
}
