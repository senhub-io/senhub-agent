package couchdb

import (
	"net/url"
	"strconv"
	"sync"

	"senhub-agent.go/internal/agent/probes/dbcommon"
	"senhub-agent.go/internal/agent/services/agentstate"
	"senhub-agent.go/internal/agent/services/entity"
)

// couchdbEntitySource feeds the entity rail with the "db" entity for the
// CouchDB instance this probe monitors (Toise db identity contract).
//
// Identity resolution, by precedence:
//  1. operator config "instance_name" → verbatim, pinned at construction.
//  2. CouchDB server UUID fetched from GET / on first successful collect →
//     "couchdb:<uuid>", pinned lazily; entity NOT emitted until pinned.
//  3. host:port degraded fallback (never reached for CouchDB, which always
//     exposes a UUID, but kept for contract completeness).
//
// Once pinned, the id never changes for the process lifetime.
type couchdbEntitySource struct {
	// pinnedID is the resolved db.instance.id. Empty until resolved.
	// Written once (under mu), then read-only.
	pinnedID string
	// hostPort is the fallback "host:port" string, computed at construction.
	hostPort string
	// host is the monitored db host, used to gate the local-db runs_on edge.
	host string
	// hostID resolves the agent host id for a local-db runs_on edge.
	// nil → dbcommon.HostID.
	hostID func() string

	mu sync.RWMutex
	up bool
	// attrs carries descriptive attributes; db.system.version added on first
	// successful collect via updateVersion.
	attrs map[string]any
}

// newCouchDBEntitySource builds the entity source.
//
// instanceName is the optional operator-provided stable id (config key
// "instance_name"). When non-empty it is used verbatim and pinned immediately
// so the entity is emitted on the very first cycle.
func newCouchDBEntitySource(endpoint, instanceName string) *couchdbEntitySource {
	addr, port := couchdbHostPortFromEndpoint(endpoint)
	hp := addr + ":" + strconv.FormatInt(port, 10)

	s := &couchdbEntitySource{
		hostPort: hp,
		host:     addr,
		hostID:   dbcommon.HostID,
		attrs: map[string]any{
			"db.system.name": "couchdb",
			"server.address": addr,
			"server.port":    port,
		},
	}

	if instanceName != "" {
		// Operator-supplied: pin immediately, emit from the first cycle.
		s.pinnedID = instanceName
	}
	return s
}

// setReachable is called by Collect to report the current connectivity state.
// When up is false the entity is suppressed from Observe until the next
// successful collection (transient outage != gone).
func (s *couchdbEntitySource) setReachable(up bool) {
	s.mu.Lock()
	s.up = up
	s.mu.Unlock()
}

// pinServerUUID pins the db.instance.id from the CouchDB server UUID (GET /).
// It is a no-op after the first successful call — the id is immutable once set.
// Called from the probe's Collect path on every successful fetch until pinned.
func (s *couchdbEntitySource) pinServerUUID(uuid string) {
	if uuid == "" {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.pinnedID == "" {
		s.pinnedID = "couchdb:" + uuid
	}
}

// updateVersion stores the CouchDB server version gathered during a successful
// collection cycle.
func (s *couchdbEntitySource) updateVersion(version string) {
	if version == "" {
		return
	}
	s.mu.Lock()
	attrs := make(map[string]any, len(s.attrs)+1)
	for k, v := range s.attrs {
		attrs[k] = v
	}
	attrs["db.system.version"] = version
	s.attrs = attrs
	s.mu.Unlock()
}

// couchdbHostPortFromEndpoint extracts the host and port from an HTTP endpoint
// URL such as "http://localhost:5984". Falls back to "localhost" / 5984 when
// the URL cannot be parsed or has no explicit port.
func couchdbHostPortFromEndpoint(endpoint string) (host string, port int64) {
	host = "localhost"
	port = 5984

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
	} else {
		switch u.Scheme {
		case "https":
			port = 443
		default:
			port = 5984
		}
	}
	return
}

// Observe implements entity.Source. Returns ok=false when:
//   - the CouchDB instance is currently unreachable (transient outage ≠ gone), or
//   - the stable id has not been pinned yet (tech UUID not fetched yet).
func (s *couchdbEntitySource) Observe() (entity.Observation, bool) {
	s.mu.RLock()
	up := s.up
	pinnedID := s.pinnedID
	attrs := s.attrs
	s.mu.RUnlock()

	if !up || pinnedID == "" {
		return entity.Observation{}, false
	}

	dbID := map[string]any{"db.instance.id": pinnedID}
	obs := entity.Observation{
		Entities: []entity.Entity{{
			Type:       "db",
			ID:         dbID,
			Attributes: attrs,
		}},
	}

	if agentID := agentstate.GetAgentInstanceID(); agentID != "" {
		obs.Relations = append(obs.Relations, entity.Relation{
			Type:     "monitors",
			FromType: "service.instance",
			FromID:   map[string]any{"service.instance.id": agentID},
			ToType:   "db",
			ToID:     map[string]any{"db.instance.id": pinnedID},
		})
	}

	// runs_on edge: db → host when the db is on the agent's own host (loopback).
	// The collapse guard suppresses it for a host:port-derived id.
	if rel, ok := dbcommon.LocalHostRunsOn(dbID, s.host, s.hostID()); ok {
		obs.Relations = append(obs.Relations, rel)
	}

	return obs, true
}
