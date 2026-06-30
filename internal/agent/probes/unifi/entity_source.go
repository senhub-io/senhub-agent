package unifi

import (
	"net/url"
	"sync"

	"senhub-agent.go/internal/agent/services/agentstate"
	"senhub-agent.go/internal/agent/services/common"
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

	// serverAddr is the host parsed from the endpoint; runs_on→host is emitted
	// only when it is loopback. The id is endpoint-derived (it embeds this host),
	// so the collapse guard suppresses the runs_on even on loopback — wired for
	// correctness, the gate alone decides.
	serverAddr string
	hostID     func() string // agent host id resolver, overridable in tests

	mu        sync.Mutex
	observed  bool
	reachable bool
}

func newEntitySource(endpoint string) *unifiEntitySource {
	return &unifiEntitySource{
		id:         "unifi://" + endpoint,
		serverAddr: hostFromEndpoint(endpoint),
		hostID:     unifiHostID,
	}
}

// hostFromEndpoint extracts the host from an endpoint URL (e.g.
// "https://localhost:8443" → "localhost"), returning the raw endpoint when it
// is not a parseable URL.
func hostFromEndpoint(endpoint string) string {
	if u, err := url.Parse(endpoint); err == nil && u.Hostname() != "" {
		return u.Hostname()
	}
	return endpoint
}

// unifiHostID resolves the agent host's stable machine-id, or "" when unreadable.
func unifiHostID() string {
	hi, err := common.GetHostIdentity()
	if err != nil {
		return ""
	}
	return hi.ID
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

	// runs_on edge: controller → host when the endpoint is local (loopback). The
	// id is endpoint-derived, so the collapse guard refuses it on loopback (the
	// id is identical on every host); wired anyway so the gate decides.
	if rel, ok := entity.LocalRunsOn(entityTypeServiceInstance, svcID, s.serverAddr, s.hostID()); ok {
		obs.Relations = append(obs.Relations, rel)
	}
	return obs, true
}
