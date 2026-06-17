package varnish

import (
	"sync"

	"senhub-agent.go/internal/agent/services/agentstate"
	"senhub-agent.go/internal/agent/services/entity"
)

// varnishEntitySource implements entity.Source for the Varnish Cache probe.
// It exposes the monitored Varnish instance as a service.instance entity
// (Toise D1 option A: stable non-network-derived id) and emits a monitors
// edge from the agent to the target when the agent identity is available.
// The entity is emitted only when varnishstat succeeds (up=true); a failing
// instance returns ok=false so the detector keeps the previous good snapshot
// alive rather than deleting the entity on each transient error.
type varnishEntitySource struct {
	instanceID string
	mu         sync.RWMutex
	up         bool
	attrs      map[string]any
}

// newVarnishEntitySource builds the entity source.
//
// instanceName comes from the "instance_name" config key (operator-set,
// stable). hostID is the OS machine-id resolved once at construction;
// callers pass it in so tests are hermetic (no real gopsutil call).
//
// Identity resolution (precedence, D1 rule):
//  1. instanceName if non-empty — operator-set, stable by definition.
//  2. "varnish@" + hostID — machine-scoped, stable as long as the OS
//     machine-id does not change (reboot / rename safe).
//  3. "varnish" — last resort when hostID could not be resolved.
//
// "varnish://host:port", a URL, a port, or any IP never appear in the id.
func newVarnishEntitySource(instanceName, hostID string) *varnishEntitySource {
	id := resolveInstanceID(instanceName, hostID)
	return &varnishEntitySource{
		instanceID: id,
		attrs: map[string]any{
			"service.name":   "varnish",
			"server.address": "localhost",
		},
	}
}

// resolveInstanceID applies the D1 precedence rule and returns the stable
// service.instance.id. Extracted so tests can exercise the rule directly.
func resolveInstanceID(instanceName, hostID string) string {
	if instanceName != "" {
		return instanceName
	}
	if hostID != "" {
		return "varnish@" + hostID
	}
	return "varnish"
}

// setReachable is called by Collect each cycle. When up is true the entity is
// (re-)emitted; when false ok=false is returned to keep the last good snapshot.
func (s *varnishEntitySource) setReachable(up bool) {
	s.mu.Lock()
	s.up = up
	s.mu.Unlock()
}

// Observe implements entity.Source. Non-blocking; safe to call from the
// detector goroutine.
//
// When up is true it emits the service.instance entity plus a monitors relation
// from the agent's own service.instance entity to this target. The relation is
// omitted when the agent identity is not yet available (entity emission is off
// or has not started): emitting a relation whose From endpoint cannot be
// resolved would be buffered then dropped by the consumer.
func (s *varnishEntitySource) Observe() (entity.Observation, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if !s.up {
		return entity.Observation{}, false
	}

	obs := entity.Observation{
		Entities: []entity.Entity{{
			Type:       "service.instance",
			ID:         map[string]any{"service.instance.id": s.instanceID},
			Attributes: s.attrs,
		}},
	}

	if agentID := agentstate.GetAgentInstanceID(); agentID != "" {
		obs.Relations = append(obs.Relations, entity.Relation{
			Type:     "monitors",
			FromType: "service.instance",
			FromID:   map[string]any{"service.instance.id": agentID},
			ToType:   "service.instance",
			ToID:     map[string]any{"service.instance.id": s.instanceID},
		})
	}

	return obs, true
}
