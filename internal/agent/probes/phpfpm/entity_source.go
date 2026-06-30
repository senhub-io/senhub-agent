package phpfpm

import (
	"sync"

	"senhub-agent.go/internal/agent/services/agentstate"
	"senhub-agent.go/internal/agent/services/entity"
)

// phpfpmEntitySource feeds the entity rail with a single service.instance
// entity for the monitored PHP-FPM endpoint. The instance ID is stable:
// either the operator-supplied instance_name or "phpfpm@<host-id>" (the
// agent's machine-id, which does not change on rename or IP reassignment).
// server.address / server.port are kept as descriptive attributes only —
// they are NOT part of the identity.
type phpfpmEntitySource struct {
	instanceID string
	serverAddr string // host from the endpoint; a runs_on→host is emitted only when it is loopback
	hostID     string // agent host id, target of the local-target runs_on edge
	mu         sync.RWMutex
	up         bool
	attrs      map[string]any
}

// newPhpfpmEntitySource builds the entity source.
//
// instanceName is the operator-supplied name (from the "instance_name" config
// key); when empty, hostID is used to form "phpfpm@<hostID>" (or "phpfpm" as
// a last resort when hostID is also empty). addr and port are carried as
// descriptive attributes only.
func newPhpfpmEntitySource(instanceName, hostID string, addr string, port int) *phpfpmEntitySource {
	instanceID := resolveInstanceID(instanceName, hostID)
	return &phpfpmEntitySource{
		instanceID: instanceID,
		serverAddr: addr,
		hostID:     hostID,
		attrs: map[string]any{
			"service.name":   "phpfpm",
			"server.address": addr,
			"server.port":    int64(port),
		},
	}
}

// resolveInstanceID applies the precedence rule for service.instance.id:
//  1. operator-supplied instance_name (verbatim)
//  2. "phpfpm@<hostID>" when hostID is non-empty
//  3. "phpfpm" (last resort)
func resolveInstanceID(instanceName, hostID string) string {
	if instanceName != "" {
		return instanceName
	}
	if hostID != "" {
		return "phpfpm@" + hostID
	}
	return "phpfpm"
}

// setReachable is called by the collect cycle: true when the status page
// responded, false on any fetch error. version may be empty when unknown.
func (s *phpfpmEntitySource) setReachable(up bool, version string) {
	s.mu.Lock()
	s.up = up
	if up {
		attrs := map[string]any{
			"service.name":   s.attrs["service.name"],
			"server.address": s.attrs["server.address"],
			"server.port":    s.attrs["server.port"],
		}
		if version != "" {
			attrs["service.version"] = version
		}
		s.attrs = attrs
	}
	s.mu.Unlock()
}

// Observe implements entity.Source. Returns ok=false until the first
// successful collection cycle so a transient startup error does not
// immediately delete the entity in the consumer.
// When up==true it also emits a monitors relation from the agent's own
// service.instance to this target (skipped when the agent id is not yet
// known — an unresolvable From endpoint would be buffered then dropped).
func (s *phpfpmEntitySource) Observe() (entity.Observation, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if !s.up {
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

	// runs_on edge: phpfpm → host when the monitored endpoint is local (loopback),
	// so a locally-monitored pool hangs off the host it runs on instead of
	// floating with only its monitors anchor. A remote endpoint yields no edge.
	if rel, ok := entity.LocalRunsOn("service.instance", svcID, s.serverAddr, s.hostID); ok {
		obs.Relations = append(obs.Relations, rel)
	}
	return obs, true
}
