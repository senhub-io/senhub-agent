package consul

import (
	"sync"

	"senhub-agent.go/internal/agent/services/agentstate"
	"senhub-agent.go/internal/agent/services/common"
	"senhub-agent.go/internal/agent/services/entity"
)

// consulEntitySource feeds the entity rail with the Consul agent instance
// following Toise model decision D1 (option A): a monitored non-DB service is
// a service.instance with a stable, non-network-derived id, plus a monitors
// edge from the agent.
//
// ID resolution precedence (pinned once, never changed for the process lifetime):
//  1. operator config key "instance_name" (non-empty) → verbatim;
//  2. tech-reported persistent id "consul:<node-uuid>" (from GET /v1/agent/self
//     Config.NodeID); fetched lazily on the first successful collect cycle;
//  3. precedence-2 fallback "consul@<host.id>" when the tech id is genuinely
//     unavailable; then "consul" as last resort.
//
// Immutability contract: the entity is NOT emitted (Observe returns ok=false)
// until the id is pinned so the consumer never sees a network-derived id that
// a later tech-id fetch would re-key.
type consulEntitySource struct {
	// host and port are descriptive attributes only; never used as identity.
	host string
	port int64

	// hostIDFn returns the fallback host.id (injected for testing).
	hostIDFn func() string

	// hostID is the agent host id, resolved once at construction; it is the
	// target of the local-target runs_on edge.
	hostID string

	mu sync.Mutex

	// pinnedID is empty until the id is resolved; once set it is immutable.
	pinnedID string
	// pinnedFromConfig is true when pinnedID came from the instance_name config
	// key and therefore does not need a tech-id fetch.
	pinnedFromConfig bool

	cache entity.Observation
	ready bool // true once we have a stable pinnedID and have produced at least one observation
}

// newConsulEntitySource constructs the entity source. instanceName is the
// optional operator-supplied identity override (config key "instance_name").
// host and port are passed for descriptive attributes only.
func newConsulEntitySource(instanceName string, host string, port int) *consulEntitySource {
	s := &consulEntitySource{
		host: host,
		port: int64(port),
		hostIDFn: func() string {
			id, err := common.GetHostIdentity()
			if err != nil || id.ID == "" {
				return ""
			}
			return id.ID
		},
	}
	s.hostID = s.hostIDFn()
	if instanceName != "" {
		s.pinnedID = instanceName
		s.pinnedFromConfig = true
	}
	return s
}

// setPinnedIDFromConfig is used by tests to confirm the instance_name fast path.
func (s *consulEntitySource) hasPinnedID() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.pinnedID != ""
}

// setReachable is called after each collect cycle. nodeID is the value of
// Config.NodeID from /v1/agent/self (empty if the endpoint was unreachable or
// the field was absent). version may be empty.
//
// On the first successful call with a non-empty nodeID the tech id is pinned.
// If up=false and no id is pinned yet, the call is a no-op (we never emit
// a fallback-id entity before the tech id is confirmed; once the tech id is
// genuinely unavailable after a degraded path we pin the fallback).
func (s *consulEntitySource) setReachable(up bool, nodeID string, version string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if !up {
		if s.pinnedID == "" {
			// Still waiting for first successful collect — do not emit anything.
			return
		}
		// Target went away after we already pinned an id: signal absence.
		s.cache = entity.Observation{}
		s.ready = true
		return
	}

	// up == true: pin the id if not already pinned.
	if s.pinnedID == "" {
		if nodeID != "" {
			s.pinnedID = "consul:" + nodeID
		} else {
			// Tech id unavailable on a successful collect (unusual): degrade to
			// host.id fallback, then last-resort bare "consul".
			if hid := s.hostIDFn(); hid != "" {
				s.pinnedID = "consul@" + hid
			} else {
				s.pinnedID = "consul"
			}
		}
	}

	attrs := map[string]any{
		"service.name":   "consul",
		"server.address": s.host,
		"server.port":    s.port,
	}
	if version != "" {
		attrs["service.version"] = version
	}

	svcID := map[string]any{"service.instance.id": s.pinnedID}
	obs := entity.Observation{
		Entities: []entity.Entity{{
			Type:       "service.instance",
			ID:         svcID,
			Attributes: attrs,
		}},
	}

	// Append monitors edge when the agent identity is known.
	if agentID := agentstate.GetAgentInstanceID(); agentID != "" {
		obs.Relations = append(obs.Relations, entity.Relation{
			Type:     "monitors",
			FromType: "service.instance",
			FromID:   map[string]any{"service.instance.id": agentID},
			ToType:   "service.instance",
			ToID:     svcID,
		})
	}

	// runs_on edge: consul → host when the monitored endpoint is local (loopback),
	// so a locally-monitored consul hangs off the host it runs on instead of
	// floating with only its monitors anchor. A remote endpoint yields no edge.
	if rel, ok := entity.LocalRunsOn("service.instance", svcID, s.host, s.hostID); ok {
		obs.Relations = append(obs.Relations, rel)
	}

	s.cache = obs
	s.ready = true
}

// Observe returns the latest cached entity snapshot. Non-blocking; safe to
// call from the detector goroutine. Returns ok=false until the id has been
// pinned and the first observation produced.
func (s *consulEntitySource) Observe() (entity.Observation, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.cache, s.ready
}
