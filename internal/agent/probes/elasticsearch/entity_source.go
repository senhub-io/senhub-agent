package elasticsearch

import (
	"net/url"
	"strconv"
	"sync"

	"senhub-agent.go/internal/agent/probes/dbcommon"
	"senhub-agent.go/internal/agent/services/agentstate"
	"senhub-agent.go/internal/agent/services/entity"
)

// elasticsearchEntitySource feeds the entity rail with the "db" entity for
// the Elasticsearch cluster this probe monitors (Toise db identity contract).
//
// Identity precedence (immutable once pinned):
//  1. operator config key "instance_name" → verbatim, pinned at construction.
//  2. cluster_uuid from GET / → "elasticsearch:<uuid>", pinned on the first
//     successful collect (entity suppressed until then).
//  3. host:port fallback (never used for ES because cluster_uuid is always
//     available, but kept as the documented db degraded fallback).
//
// The monitors edge (service.instance → db) is appended whenever the
// agentstate package reports a non-empty agent instance id.
type elasticsearchEntitySource struct {
	// descriptive attributes: server.address, server.port, db.system.name.
	// Initialised at construction; version appended on first successful collect.
	staticAttrs map[string]any
	// host is the monitored db host, used to gate the local-db runs_on edge.
	host string
	// hostID resolves the agent host id for a local-db runs_on edge.
	// nil → dbcommon.HostID.
	hostID func() string

	mu sync.RWMutex
	// instanceID holds the pinned db.instance.id.  Empty until pinned.
	instanceID string
	// pinned is true once instanceID will never change.
	pinned bool
	// up reflects the last connectivity state reported by Collect.
	up bool
	// attrs is a copy of staticAttrs extended with db.system.version once
	// a version banner is observed.
	attrs map[string]any
}

// newElasticsearchEntitySource builds the entity source.
//
// instanceName is the operator's override (config key "instance_name"); when
// non-empty it is pinned immediately and the entity is ready to emit from the
// first successful collect.  When empty, the source waits for pinClusterUUID
// to be called before emitting anything.
//
// host and port are the network coordinates extracted from the configured
// endpoint; they are kept as descriptive attributes regardless of which id
// rung wins.
func newElasticsearchEntitySource(instanceName, host string, port int64) *elasticsearchEntitySource {
	staticAttrs := map[string]any{
		"db.system.name": "elasticsearch",
		"server.address": host,
		"server.port":    port,
	}

	s := &elasticsearchEntitySource{
		staticAttrs: staticAttrs,
		attrs:       staticAttrs,
		host:        host,
		hostID:      dbcommon.HostID,
	}
	if instanceName != "" {
		s.instanceID = instanceName
		s.pinned = true
	}
	return s
}

// pinClusterUUID records the cluster_uuid returned by GET / and builds the
// canonical db.instance.id "elasticsearch:<uuid>".  It is a no-op after the
// first call (immutability guarantee).
func (s *elasticsearchEntitySource) pinClusterUUID(uuid string) {
	if uuid == "" {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.pinned {
		return
	}
	s.instanceID = "elasticsearch:" + uuid
	s.pinned = true
}

// setReachable is called by Collect to report the current connectivity state.
// When up is false the entity is suppressed from Observe (transient outage ≠
// gone — the detector holds the last good snapshot).
func (s *elasticsearchEntitySource) setReachable(up bool) {
	s.mu.Lock()
	s.up = up
	s.mu.Unlock()
}

// updateVersion stores the ES version banner in the descriptive attrs set
// (copy-on-write so Observe never races against a write).
func (s *elasticsearchEntitySource) updateVersion(version string) {
	if version == "" {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	a := make(map[string]any, len(s.staticAttrs)+1)
	for k, v := range s.staticAttrs {
		a[k] = v
	}
	a["db.system.version"] = version
	s.attrs = a
}

// Observe implements entity.Source.  Returns ok=false when:
//   - the cluster id has not yet been pinned (cluster_uuid not yet fetched), or
//   - the instance is currently unreachable.
//
// Both conditions cause the detector to keep the last good snapshot rather than
// emitting a delete (transient outage / pending first collect ≠ gone).
func (s *elasticsearchEntitySource) Observe() (entity.Observation, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if !s.pinned || !s.up {
		return entity.Observation{}, false
	}

	dbID := map[string]any{"db.instance.id": s.instanceID}
	obs := entity.Observation{
		Entities: []entity.Entity{{
			Type:       "db",
			ID:         dbID,
			Attributes: s.attrs,
		}},
	}

	if agentID := agentstate.GetAgentInstanceID(); agentID != "" {
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
	if rel, ok := dbcommon.LocalHostRunsOn(dbID, s.host, s.hostID()); ok {
		obs.Relations = append(obs.Relations, rel)
	}

	return obs, true
}

// hostPortFromEndpoint extracts the host and port from an HTTP endpoint URL
// such as "http://localhost:9200".  Falls back to "localhost" / 9200 when the
// URL cannot be parsed or has no explicit port.
func hostPortFromEndpoint(endpoint string) (host string, port int64) {
	host = "localhost"
	port = 9200

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
