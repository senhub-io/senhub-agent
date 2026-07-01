package clickhouse

import (
	"net/url"
	"strconv"
	"sync"

	"senhub-agent.go/internal/agent/probes/dbcommon"
	"senhub-agent.go/internal/agent/services/agentstate"
	"senhub-agent.go/internal/agent/services/entity"
)

// clickhouseEntitySource feeds the entity rail with the ClickHouse instance
// this probe monitors. The db.instance.id follows a strict precedence:
//
//  1. operator-supplied instance_name (stable, pinned at construction);
//  2. tech-reported serverUUID(), fetched lazily on the first successful
//     collect and never changed afterwards ("clickhouse:<uuid>");
//  3. host:port (last resort when no stable tech id exists — used only when
//     instance_name="" AND the server never provides a UUID).
//
// Per the Toise db identity contract the entity is NOT emitted until the id
// is pinned (ok=false from Observe), so the consumer never keys on a
// transient host:port value that would be replaced later by the real UUID.
// instance_name is the exception: it is stable at construction time, so the
// entity may be emitted immediately.
type clickhouseEntitySource struct {
	// instanceName is set from config; empty when the operator did not supply one.
	instanceName string
	// fallbackID is "host:port", pre-computed and always valid.
	fallbackID string
	host       string
	port       int64
	// hostID resolves the agent host id for a local-db runs_on edge.
	// nil → dbcommon.HostID.
	hostID func() string

	mu       sync.RWMutex
	pinnedID string // empty until pinned
	up       bool
	// attrs holds the last set of descriptive attributes from a successful
	// collection cycle.
	attrs map[string]any
}

// newClickhouseEntitySource constructs the entity source.
// instanceName comes from the operator config key "instance_name" (may be "").
// endpoint is used to extract host:port for both the fallback id and the
// server.address/server.port descriptive attributes.
func newClickhouseEntitySource(instanceName, endpoint string) *clickhouseEntitySource {
	addr, port := hostPortFromEndpoint(endpoint)
	s := &clickhouseEntitySource{
		instanceName: instanceName,
		fallbackID:   addr + ":" + strconv.FormatInt(port, 10),
		host:         addr,
		port:         port,
		hostID:       dbcommon.HostID,
	}
	if instanceName != "" {
		// Pin immediately — operator-supplied name is stable at construction.
		s.pinnedID = instanceName
	}
	return s
}

// isPinned reports whether the instance id has already been resolved.
// Safe to call from Collect without holding any lock.
func (s *clickhouseEntitySource) isPinned() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.pinnedID != ""
}

// pinTechID pins the tech-reported stable id ("clickhouse:<uuid>") on the
// first successful serverUUID fetch. Subsequent calls are no-ops once the id
// is pinned. Returns the pinned id (the caller may use it immediately).
//
// Called from Collect() with the UUID string returned by fetchServerUUID.
// When uuid is empty, it falls back to pinning the host:port degraded id —
// ClickHouse (db type) permits host:port as a last resort when no UUID
// can be obtained, and pinning it avoids holding back the entity indefinitely.
func (s *clickhouseEntitySource) pinTechID(uuid string) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.pinnedID != "" {
		return s.pinnedID
	}
	if uuid != "" {
		s.pinnedID = "clickhouse:" + uuid
	} else {
		// Degrade to host:port — stable for this target for the process lifetime.
		s.pinnedID = s.fallbackID
	}
	return s.pinnedID
}

// setReachable is called by Collect to report the current connectivity state.
// When up is true, descriptive attributes are refreshed. When up is false the
// entity is suppressed from Observe until the next successful collection.
func (s *clickhouseEntitySource) setReachable(up bool, version string) {
	s.mu.Lock()
	s.up = up
	if up {
		attrs := map[string]any{
			"db.system.name": "clickhouse",
			"server.address": s.host,
			"server.port":    s.port,
		}
		if version != "" {
			attrs["db.system.version"] = version
		}
		s.attrs = attrs
	}
	s.mu.Unlock()
}

// Observe implements entity.Source.
//
// Returns ok=false in two cases:
//   - the instance is currently unreachable (transient outage — keep last good
//     snapshot in the consumer, audit D3);
//   - the id has not been pinned yet (instance_name is empty AND no
//     serverUUID has been fetched yet — never emit a mutable host:port id
//     that would re-key the db node when the real UUID arrives).
func (s *clickhouseEntitySource) Observe() (entity.Observation, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if !s.up {
		return entity.Observation{}, false
	}
	if s.pinnedID == "" {
		// First successful collect hasn't happened yet; don't emit.
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
	if rel, ok := dbcommon.LocalHostRunsOn(dbID, s.host, s.hostID()); ok {
		obs.Relations = append(obs.Relations, rel)
	}

	return obs, true
}

// hostPortFromEndpoint extracts the host and port from an HTTP endpoint such
// as "http://localhost:8123". Falls back to "localhost" / 8123 when the
// endpoint cannot be parsed or has no explicit port.
func hostPortFromEndpoint(endpoint string) (host string, port int64) {
	host = "localhost"
	port = 8123

	u, err := url.Parse(endpoint)
	if err != nil || u.Host == "" {
		return
	}
	if h := u.Hostname(); h != "" {
		host = h
	}
	if p := u.Port(); p != "" {
		if n, err := strconv.ParseInt(p, 10, 64); err == nil {
			port = n
		}
	}
	return
}
