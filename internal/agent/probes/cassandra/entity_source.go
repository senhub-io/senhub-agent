package cassandra

import (
	"sync"

	"senhub-agent.go/internal/agent/probes/dbcommon"
	"senhub-agent.go/internal/agent/services/agentstate"
	"senhub-agent.go/internal/agent/services/entity"
)

// entitySource implements entity.Source for the cassandra probe following the
// Toise db identity contract:
//
//  1. db.instance.id precedence:
//     a. operator config "instance_name" → pinned at construction, emitted immediately.
//     b. tech id "cassandra:<host_id>" from SELECT host_id FROM system.local →
//     fetched lazily on the first successful Collect, then pinned for the
//     process lifetime. No entity is emitted before it is pinned (ok=false).
//     c. host:port → only used when case (a) applies (no Cassandra-reported id
//     expected; this probe always has a stable Cassandra tech id available
//     via JMX/system.local).
//
//  2. Immutability: once a non-empty instanceID is stored it is never replaced.
//
//  3. monitors edge: when agentstate.GetAgentInstanceID() is non-empty and the db
//     id is pinned, the observation includes a relation from the agent
//     service.instance to this db entity.
type entitySource struct {
	mu sync.Mutex

	// hostID resolves the agent host id for a local-db runs_on edge.
	// nil → dbcommon.HostID.
	hostID func() string

	// instanceID is the pinned db.instance.id; empty until pinned.
	instanceID string
	// pinned marks that instanceID has been committed for the process lifetime.
	pinned bool

	// obs is the last good observation (set when pinned is true and up=true).
	obs entity.Observation
	// ok mirrors the contract: false = "no trustworthy observation yet".
	ok bool
}

// newEntitySource returns a new entitySource.
// If instanceName is non-empty (operator override) it pins the id immediately
// and builds an initial observation (no version yet — version is "" until the
// first successful Collect updates it). addr and port are the Jolokia-derived
// host:port used as descriptive server.address / server.port attributes.
func newEntitySource(instanceName, addr, port string) *entitySource {
	s := &entitySource{hostID: dbcommon.HostID}
	if instanceName != "" {
		s.instanceID = instanceName
		s.pinned = true
		s.obs = s.buildObservation(addr, port, "")
		s.ok = true
	}
	return s
}

// Observe implements entity.Source.
// Returns ok=false until the tech id has been pinned (or an instance_name
// override is active), and whenever the last collect cycle reported the node
// as down.
func (s *entitySource) Observe() (entity.Observation, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.obs, s.ok
}

// update records the result of a Collect cycle.
//
//   - up=false → marks the observation stale (ok=false); the detector keeps the
//     last good snapshot per the D3 audit contract.
//   - up=true and techID non-empty → pins the id on first call, rebuilds the
//     observation, sets ok=true.
//   - up=true and techID empty → does nothing until a techID arrives (id not
//     yet pinned from case b above).
//
// addr / port are the Jolokia host:port (descriptive only).
// version is the Cassandra release version string ("" when unknown).
func (s *entitySource) update(techID, addr, port string, up bool, version string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if !up {
		s.ok = false
		return
	}

	// Pin the tech id on the first successful collect if no operator override
	// pinned it at construction.
	if !s.pinned {
		if techID == "" {
			// No stable id yet — refuse to emit until we have one.
			return
		}
		s.instanceID = techID
		s.pinned = true
	}

	s.obs = s.buildObservation(addr, port, version)
	s.ok = true
}

// buildObservation assembles the Observation for the current state.
// Called with s.mu held. instanceID must be pinned before this is called.
func (s *entitySource) buildObservation(addr, port, version string) entity.Observation {
	attrs := map[string]any{
		"db.system.name": "cassandra",
		"server.address": addr,
		"server.port":    port,
	}
	if version != "" {
		attrs["db.system.version"] = version
	}

	dbID := map[string]any{"db.instance.id": s.instanceID}
	obs := entity.Observation{
		Entities: []entity.Entity{
			{
				Type:       "db",
				ID:         dbID,
				Attributes: attrs,
			},
		},
	}

	agentID := agentstate.GetAgentInstanceID()
	if agentID != "" {
		obs.Relations = append(obs.Relations, entity.Relation{
			Type:     "monitors",
			FromType: "service.instance",
			FromID:   map[string]any{"service.instance.id": agentID},
			ToType:   "db",
			ToID:     map[string]any{"db.instance.id": s.instanceID},
		})
	}

	// runs_on edge: db → host when the db is on the agent's own host (loopback).
	// The collapse guard suppresses it for a host:port-derived id.
	if rel, ok := dbcommon.LocalHostRunsOn(dbID, addr, s.hostID()); ok {
		obs.Relations = append(obs.Relations, rel)
	}

	return obs
}
