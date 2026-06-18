package nats

import (
	"sync"

	"senhub-agent.go/internal/agent/services/agentstate"
	"senhub-agent.go/internal/agent/services/common"
	"senhub-agent.go/internal/agent/services/entity"
)

// natsEntitySource exposes the monitored NATS server as a service.instance
// entity in the topology graph (Toise / entity rail, #185).
//
// Identity precedence (D1 option A — tech-reported, stable):
//
//  1. Operator config "instance_name" → used verbatim (pinned at construction).
//  2. Server-reported identity from /varz: prefer "server_name" when non-empty,
//     else "server_id" (a NUID, always present). Formatted as "nats:<id>".
//     Pinned lazily on the first successful /varz parse.
//  3. Precedence-2 fallback "nats@<host.id>" when the tech id is genuinely
//     unavailable. Pinned only after deciding to degrade (target unreachable).
//  4. Last resort: "nats".
//
// The entity is NOT emitted (Observe returns ok=false) until the id is pinned,
// so no transient network-derived id is ever emitted that a later tech-id fetch
// would re-key.
type natsEntitySource struct {
	endpoint     string
	instanceName string // empty → derive from tech id

	getHostID func() string // injectable for tests

	mu      sync.Mutex
	pinned  bool
	id      string
	version string         // service.version from /varz, "" until reported
	attrs   map[string]any // descriptive: server.address / server.port
}

func newNATSEntitySource(endpoint, instanceName string) *natsEntitySource {
	return &natsEntitySource{
		endpoint:     endpoint,
		instanceName: instanceName,
		getHostID:    defaultGetHostID,
	}
}

// defaultGetHostID resolves the local host identity for the precedence-2
// fallback. Returns "" on error (further degrades to "nats").
func defaultGetHostID() string {
	hi, err := common.GetHostIdentity()
	if err != nil || hi.ID == "" {
		return ""
	}
	return hi.ID
}

// pinFromVarz is called by the probe after a successful /varz parse. It pins
// the service.instance.id on the first call; subsequent calls are no-ops (the
// id is immutable for the process lifetime). serverName is "" when the NATS
// server has not been given an explicit name; serverID is the always-present
// NUID.
func (s *natsEntitySource) pinFromVarz(serverName, serverID, version string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if version != "" {
		s.version = version // descriptive — refresh even after the id is pinned
	}
	if s.pinned {
		return
	}

	if s.instanceName != "" {
		s.id = s.instanceName
	} else {
		raw := serverID
		if serverName != "" {
			raw = serverName
		}
		if raw != "" {
			s.id = "nats:" + raw
		} else {
			// tech id absent in /varz — degrade now rather than wait
			s.id = s.fallbackID()
		}
	}

	s.pinned = true
}

// pinFallback is called when the probe decides the tech id will never arrive
// (e.g. repeated unreachable target). It pins the precedence-2 fallback and
// allows Observe to start emitting.
func (s *natsEntitySource) pinFallback() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.pinned {
		return
	}
	s.id = s.fallbackID()
	s.pinned = true
}

// fallbackID returns "nats@<host.id>" or "nats" as last resort. Must be called
// with s.mu held.
func (s *natsEntitySource) fallbackID() string {
	hostID := s.getHostID()
	if hostID != "" {
		return "nats@" + hostID
	}
	return "nats"
}

// Observe implements entity.Source. Returns the service.instance entity for
// the monitored NATS server, plus a "monitors" edge from the agent entity.
// Returns ok=false until the id is pinned (no entity is emitted for an unknown
// id — the consumer must never receive a transient id it later has to re-key).
func (s *natsEntitySource) Observe() (entity.Observation, bool) {
	s.mu.Lock()
	pinned := s.pinned
	id := s.id
	version := s.version
	s.mu.Unlock()

	if !pinned {
		return entity.Observation{}, false
	}

	attrs := map[string]any{"service.name": "nats"}
	if version != "" {
		attrs["service.version"] = version
	}
	e := entity.Entity{
		Type:       "service.instance",
		ID:         map[string]any{"service.instance.id": id},
		Attributes: attrs,
	}

	obs := entity.Observation{Entities: []entity.Entity{e}}

	agentID := agentstate.GetAgentInstanceID()
	if agentID != "" {
		obs.Relations = append(obs.Relations, entity.Relation{
			Type:     "monitors",
			FromType: "service.instance",
			FromID:   map[string]any{"service.instance.id": agentID},
			ToType:   "service.instance",
			ToID:     map[string]any{"service.instance.id": id},
		})
	}

	return obs, true
}
