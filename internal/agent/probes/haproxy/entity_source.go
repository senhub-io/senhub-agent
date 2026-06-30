package haproxy

import (
	"sync"

	"senhub-agent.go/internal/agent/services/agentstate"
	"senhub-agent.go/internal/agent/services/entity"
)

// haproxyEntitySource exposes the monitored HAProxy instance as a
// service.instance entity on the OTel entity rail.
//
// Identity (D1, option A — stable non-network-derived id):
//   - if the operator set instance_name in config → use it verbatim;
//   - else → "haproxy@" + agent stable host id (machine-id / UUID, from
//     GetHostIdentity), computed once at construction so the id never
//     mutates during the process lifetime;
//   - if the host id is unavailable → "haproxy" (last resort, no log).
//
// server.address / server.port are DESCRIPTIVE attributes; they never
// appear in the identity map (IPs and ports are mutable under
// DHCP/failover/VIP/multi-listener).
//
// Reachability is updated by Collect on every cycle: ok=false before
// the first successful fetch keeps the detector from publishing a stale
// entity; ok=true after first success keeps the entity alive across
// transient fetch failures (audit D3).
type haproxyEntitySource struct {
	instanceID string
	attrs      map[string]any
	serverAddr string // host from the endpoint; a runs_on→host is emitted only when it is loopback
	hostID     string // agent host id, target of the local-target runs_on edge

	mu      sync.RWMutex
	reached bool
}

// newHAProxyEntitySource builds the entity source. hostID is the agent's
// stable host identity string (common.GetHostIdentity().ID), passed in by
// the probe constructor so the constructor can be called with a stub in tests.
// instanceName, when non-empty, overrides the host-derived identity.
func newHAProxyEntitySource(addr string, port int, instanceName, hostID string) *haproxyEntitySource {
	id := instanceName
	if id == "" {
		if hostID != "" {
			id = "haproxy@" + hostID
		} else {
			id = "haproxy"
		}
	}
	return &haproxyEntitySource{
		instanceID: id,
		serverAddr: addr,
		hostID:     hostID,
		attrs: map[string]any{
			"service.name":   "haproxy",
			"server.address": addr,
			"server.port":    int64(port),
		},
	}
}

// setReachable updates the liveness flag.
// Called from Collect; safe to call concurrently with Observe.
func (s *haproxyEntitySource) setReachable(up bool) {
	s.mu.Lock()
	s.reached = up
	s.mu.Unlock()
}

// Observe implements entity.Source. Returns the service.instance entity
// and the monitors relation (agent → this target) when the endpoint has
// been reached at least once. ok=false before the first successful fetch
// (nothing to publish yet is not "entity gone").
func (s *haproxyEntitySource) Observe() (entity.Observation, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if !s.reached {
		return entity.Observation{}, false
	}

	svcID := map[string]any{"service.instance.id": s.instanceID}
	obs := entity.Observation{
		Entities: []entity.Entity{{
			Type:       "service.instance",
			ID:         svcID,
			Attributes: s.attrs,
		}},
	}

	if agentID := agentstate.GetAgentInstanceID(); agentID != "" {
		obs.Relations = append(obs.Relations, entity.Relation{
			Type:     "monitors",
			FromType: "service.instance",
			FromID:   map[string]any{"service.instance.id": agentID},
			ToType:   "service.instance",
			ToID:     svcID,
		})
	}

	// runs_on edge: haproxy → host when the monitored endpoint is local (loopback),
	// so a locally-monitored haproxy hangs off the host it runs on instead of
	// floating with only its monitors anchor. A remote endpoint yields no edge.
	if rel, ok := entity.LocalRunsOn("service.instance", svcID, s.serverAddr, s.hostID); ok {
		obs.Relations = append(obs.Relations, rel)
	}

	return obs, true
}
