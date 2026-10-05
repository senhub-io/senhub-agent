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
	// clusterUID is the kube-system namespace UID — the cluster's stable,
	// self-reported identity. Empty until OnStart resolves it, and empty for
	// good when RBAC denies the read, in which case no cluster entity is
	// emitted.
	clusterUID string
	// inventory is the last observed set of nodes and containers, refreshed by
	// the metric cycle. The entity source is polled independently of Collect,
	// so it reports the last known state rather than reaching for the API on
	// its own schedule — one API read per cycle, not two, and the two rails
	// cannot disagree about what existed at a given instant.
	inventory clusterInventory
	ready     bool
	hostID    string // agent host id, target of the local-target runs_on edge
}

func newK8sEntitySource(clusterEndpoint string) *k8sEntitySource {
	return &k8sEntitySource{clusterEndpoint: clusterEndpoint, ready: true, hostID: dbcommon.HostID()}
}

// setClusterEndpoint refines the cluster identity once the live client config
// resolves the API server host in OnStart. The endpoint is created with a
// best-effort value at construction so EntitySource() is non-NoOp without a
// live cluster (#482); OnStart narrows it to the real API server address.
func (s *k8sEntitySource) setClusterIdentity(clusterEndpoint, clusterUID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.clusterEndpoint = clusterEndpoint
	s.clusterUID = clusterUID
}

// Observe returns the cluster entity. Always ok=true once initialised.
func (s *k8sEntitySource) Observe() (entity.Observation, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.ready {
		return entity.Observation{}, false
	}

	// Without the kube-system UID there is no identifiable key, hence no
	// cluster entity: an address-derived id re-keys on any endpoint change
	// and collides between clusters sharing an address. The inventory of
	// nodes and containers is independent of it and still reported.
	if s.clusterUID == "" {
		obs := entity.Observation{}
		obs.Entities = append(obs.Entities, s.inventory.entities...)
		obs.Relations = append(obs.Relations, s.inventory.relations...)
		return obs, true
	}
	svcID := map[string]any{"service.instance.id": s.clusterUID}
	obs := entity.Observation{
		Entities: []entity.Entity{
			{
				Type: entity.TypeServiceInstance,
				ID:   svcID,
				Attributes: map[string]any{
					"service.name":    "kubernetes",
					"cluster.address": s.clusterEndpoint,
					"k8s.cluster.uid": s.clusterUID,
				},
			},
		},
	}
	// The objects the cluster manages, as observed by the last metric cycle.
	obs.Entities = append(obs.Entities, s.inventory.entities...)
	obs.Relations = append(obs.Relations, s.inventory.relations...)

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
