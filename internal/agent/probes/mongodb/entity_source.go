package mongodb

import (
	"sync"

	"senhub-agent.go/internal/agent/probes/dbcommon"
	"senhub-agent.go/internal/agent/services/agentstate"
	"senhub-agent.go/internal/agent/services/entity"
)

// mongodbEntitySource feeds the entity rail with the MongoDB db entity this
// probe monitors, following the Toise db identity contract (#472, #470, #433).
//
// Identity resolution (db.instance.id), by precedence:
//  1. operator config key "instance_name" → pinned at construction, emit immediately.
//  2. tech-reported stable id "mongodb:<setName>/<selfMember>" from replSetGetStatus
//     → fetched lazily on the first successful collect, pinned once resolved;
//     the entity is NOT emitted before the id is pinned (ok=false until then).
//  3. host:port fallback — for standalone MongoDB where replSetGetStatus is
//     unavailable; pinned at construction, emit immediately (nothing better to wait for).
//
// The pinned id never changes for the process lifetime. "server.address" and
// "server.port" remain as descriptive attributes (no longer the identity).
type mongodbEntitySource struct {
	// hostPort is the credential-free host:port derived from the URI.
	// Used as descriptive attrs and as the fallback id when no stable tech id
	// is available.
	hostPort string
	// addr and port are the parsed components of hostPort, kept as typed values
	// for the descriptive attrs.
	addr string
	port int64
	// hostID resolves the agent host id for a local-db runs_on edge.
	// nil → dbcommon.HostID.
	hostID func() string

	mu sync.RWMutex
	// pinnedID is the resolved db.instance.id. Set once; never changed.
	// Empty string means "not yet resolved".
	pinnedID string
	// pinned marks that pinnedID has been set (distinguishes "" from "not ready").
	pinned bool

	// up tracks whether the last collect succeeded; entities are suppressed
	// when the target is unreachable (transient outage, not a delete).
	up bool

	// attrs holds descriptive attributes; version is added on first success.
	attrs map[string]any
}

// newMongodbEntitySource builds the entity source.
//
// When instanceName is non-empty it is used as db.instance.id immediately (precedence 1).
// When instanceName is empty and hostPort is provided as fallback, it is pinned
// only when the caller has determined that no stable tech id is available
// (precedence 3). Precedence 2 (replset id) is handled via pinTechID.
func newMongodbEntitySource(addr string, port int64, instanceName string) *mongodbEntitySource {
	hp := hostPort(addr, port)
	s := &mongodbEntitySource{
		hostPort: hp,
		addr:     addr,
		port:     port,
		hostID:   dbcommon.HostID,
		attrs: map[string]any{
			"db.system.name": "mongodb",
			"server.address": addr,
			"server.port":    port,
		},
	}
	if instanceName != "" {
		s.pinnedID = instanceName
		s.pinned = true
	}
	return s
}

// isPinned reports whether db.instance.id has been resolved. Used by
// maybeResolveEntityID to skip the replSetGetStatus round-trip after the first
// successful resolution (instance_name, tech id, or host:port fallback).
func (s *mongodbEntitySource) isPinned() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.pinned
}

// pinHostPort pins host:port as the fallback db.instance.id. Called when the
// probe has confirmed that no stable tech id (replSetGetStatus) is available.
// No-op when an id is already pinned.
func (s *mongodbEntitySource) pinHostPort() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.pinned {
		s.pinnedID = s.hostPort
		s.pinned = true
	}
}

// pinTechID pins the MongoDB replica-set identity as db.instance.id. Called
// by Collect on the first successful replSetGetStatus response. No-op when an
// id is already pinned (instance_name or prior call).
func (s *mongodbEntitySource) pinTechID(id string) {
	if id == "" {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.pinned {
		s.pinnedID = id
		s.pinned = true
	}
}

// setReachable is called by Collect to report the current connectivity state.
// When up is true and version is non-empty, it is stored as a descriptive attr.
// When up is false the entity is suppressed from Observe until the next
// successful collection (transient outage ≠ delete, audit D3).
func (s *mongodbEntitySource) setReachable(up bool, version string) {
	s.mu.Lock()
	s.up = up
	if up && version != "" {
		s.attrs["db.system.version"] = version
	}
	s.mu.Unlock()
}

// Observe implements entity.Source. Returns ok=false when:
//   - the id is not yet pinned (waiting for the first replSetGetStatus), or
//   - the MongoDB instance is currently unreachable.
//
// When ok=false the detector preserves the last good snapshot rather than
// emitting a delete (transient outage or startup before first collect).
func (s *mongodbEntitySource) Observe() (entity.Observation, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if !s.pinned || !s.up {
		return entity.Observation{}, false
	}

	dbID := map[string]any{"db.instance.id": s.pinnedID}

	obs := entity.Observation{
		Entities: []entity.Entity{{
			Type:       "db",
			ID:         dbID,
			Attributes: s.attrs,
		}},
	}

	agentID := agentstate.GetAgentInstanceID()
	if agentID != "" {
		obs.Relations = append(obs.Relations, entity.Relation{
			Type:     "monitors",
			FromType: "service.instance",
			FromID:   map[string]any{"service.instance.id": agentID},
			ToType:   "db",
			ToID:     dbID,
		})
	}

	// runs_on edge: db → host when the db is on the agent's own host (loopback).
	// The collapse guard suppresses it for a host:port-derived id.
	if rel, ok := dbcommon.LocalHostRunsOn(dbID, s.addr, s.hostID()); ok {
		obs.Relations = append(obs.Relations, rel)
	}

	return obs, true
}

// hostPort formats addr and port as "addr:port".
func hostPort(addr string, port int64) string {
	if port == 0 {
		return addr
	}
	return addr + ":" + itoa(port)
}

// itoa converts an int64 to its decimal string representation without
// importing strconv (which is already used in mongodb_probe.go for baseTags).
func itoa(n int64) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	buf := [20]byte{}
	pos := len(buf)
	for n > 0 {
		pos--
		buf[pos] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		pos--
		buf[pos] = '-'
	}
	return string(buf[pos:])
}
