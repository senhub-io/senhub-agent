package opensearch

import (
	"strconv"
	"sync"

	"senhub-agent.go/internal/agent/probes/dbcommon"
	"senhub-agent.go/internal/agent/services/agentstate"
	"senhub-agent.go/internal/agent/services/entity"
)

// opensearchEntitySource feeds the entity rail with the monitored OpenSearch
// cluster as a "db" entity under the Toise identity contract.
//
// Identity precedence (immutable once pinned):
//  1. operator config key "instance_name" — pinned at construction.
//  2. "opensearch:<cluster_uuid>" — fetched from GET / on the first successful
//     collect cycle, then pinned for the process lifetime.
//  3. "host:port" — acceptable last resort only when the probe genuinely has
//     no stable tech id (not used for opensearch: cluster_uuid is always
//     available from a live cluster).
//
// Immutability rule: the db entity is NOT emitted before the id is pinned
// (Observe returns ok=false) so Toise never receives a host:port id that later
// re-keys to the real cluster id.
type opensearchEntitySource struct {
	// addr / port are the target coordinates — descriptive only (never identity).
	addr string
	port int
	// hostID resolves the agent host id for a local-db runs_on edge.
	// nil → dbcommon.HostID.
	hostID func() string

	mu sync.RWMutex

	// pinnedID is the immutable db.instance.id. It is either set at
	// construction (when instance_name is provided) or set lazily on the first
	// successful setPinnedID call. Empty means "not yet pinned".
	pinnedID string
	// idPinned records whether pinnedID has been set (construction-time or
	// lazy). Separate bool so a zero-length instance_name does not mean pinned.
	idPinned bool

	up    bool
	attrs map[string]any
}

// newOpensearchEntitySource creates the entity source. When instanceName is
// non-empty the id is pinned immediately; otherwise the first successful
// cluster-uuid fetch pins it.
func newOpensearchEntitySource(addr string, port int, instanceName string) *opensearchEntitySource {
	s := &opensearchEntitySource{
		addr:   addr,
		port:   port,
		hostID: dbcommon.HostID,
	}
	if instanceName != "" {
		s.pinnedID = instanceName
		s.idPinned = true
	}
	return s
}

// isPinned reports whether the db.instance.id has been pinned. The probe uses
// this to skip the GET / fetch once it is no longer needed.
func (s *opensearchEntitySource) isPinned() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.idPinned
}

// setPinnedID pins the cluster-uuid-derived id on the first successful call.
// Subsequent calls are no-ops (identity is immutable). clusterUUID is the raw
// value returned by GET /; the method prefixes it with "opensearch:".
func (s *opensearchEntitySource) setPinnedID(clusterUUID string) {
	if clusterUUID == "" {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.idPinned {
		return
	}
	s.pinnedID = "opensearch:" + clusterUUID
	s.idPinned = true
}

// setReachable updates liveness and descriptive attributes. Called from
// Collect on every cycle. version may be empty when only cluster health was
// available.
func (s *opensearchEntitySource) setReachable(up bool, version string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.up = up
	if up {
		attrs := map[string]any{
			"db.system.name": "opensearch",
			"server.address": s.addr,
			"server.port":    s.port,
		}
		if version != "" {
			attrs["db.system.version"] = version
		}
		s.attrs = attrs
	}
}

// Observe implements entity.Source. Returns the db entity plus a monitors
// edge when:
//   - the id has been pinned (cluster_uuid fetched at least once), AND
//   - the cluster was reachable on the last collect cycle.
//
// Returns ok=false before the id is pinned so Toise never sees a transient
// host:port id that later re-keys to the real cluster id.
func (s *opensearchEntitySource) Observe() (entity.Observation, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if !s.idPinned || !s.up {
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

	if agentID := agentstate.GetAgentInstanceID(); agentID != "" {
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

// hostPort returns the "host:port" fallback string for tests and diagnostics.
func (s *opensearchEntitySource) hostPort() string {
	return s.addr + ":" + strconv.Itoa(s.port)
}
