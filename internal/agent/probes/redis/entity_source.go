package redis

import (
	"sync"

	"senhub-agent.go/internal/agent/probes/dbcommon"
	"senhub-agent.go/internal/agent/services/agentstate"
	"senhub-agent.go/internal/agent/services/entity"
)

// entityObserver implements entity.Source for the redis probe. It emits a
// db entity whose db.instance.id is determined at construction by the
// following precedence:
//
//  1. operator config key "instance_name" (verbatim, set at construction)
//  2. host:port (the documented db degraded fallback — Redis has no
//     persistent stable id: INFO server run_id changes on every restart
//     and MUST NOT be used as identity)
//
// Because Redis provides no stable tech-side id, the id is pinned at
// construction from host:port (or instance_name when set). The db entity
// is emitted immediately after the first successful collect (ok=true after
// the first update call). The id never changes for the process lifetime.
//
// The Toise contract for db entities is frozen: a changing id re-keys the
// entity in the consumer. "server.address" and "server.port" are kept as
// descriptive attributes (not identity) alongside "db.system.name":"redis".
//
// A "monitors" edge from the agent's service.instance to this db entity is
// appended when agentstate.GetAgentInstanceID() is non-empty.
type entityObserver struct {
	// pinnedID is the db.instance.id, set once at construction and never
	// changed afterwards. No mutex is needed for pinnedID itself.
	pinnedID string
	// hostID resolves the agent host for a local-db runs_on; nil → dbcommon.HostID.
	hostID func() string

	mu  sync.Mutex
	obs entity.Observation
	ok  bool
}

// newEntityObserver builds the observer and pins the db.instance.id
// immediately. It never returns nil.
func newEntityObserver(cfg probeConfig, hostPort string) *entityObserver {
	id := hostPort
	if cfg.InstanceName != "" {
		id = cfg.InstanceName
	}
	return &entityObserver{pinnedID: id, hostID: dbcommon.HostID}
}

// Observe returns the last cached entity observation. ok is false before the
// first successful collect cycle.
func (e *entityObserver) Observe() (entity.Observation, bool) {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.obs, e.ok
}

// update rebuilds the cached observation from the most-recent INFO map.
// It must be called after every successful collect cycle. The db.instance.id
// is the pinnedID and never changes.
func (e *entityObserver) update(cfg probeConfig, info map[string]string) {
	attrs := map[string]any{
		"db.system.name": "redis",
		"server.address": cfg.Host,
		"server.port":    int64(cfg.Port),
	}
	if ver := info["redis_version"]; ver != "" {
		attrs["db.system.version"] = ver
	}

	dbID := map[string]any{"db.instance.id": e.pinnedID}

	obs := entity.Observation{
		Entities: []entity.Entity{
			{
				Type:       "db",
				ID:         dbID,
				Attributes: attrs,
			},
		},
	}

	// monitors edge: from this agent's service.instance to the db entity.
	if agentID := agentstate.GetAgentInstanceID(); agentID != "" {
		obs.Relations = append(obs.Relations, entity.Relation{
			Type:     "monitors",
			FromType: "service.instance",
			FromID:   map[string]any{"service.instance.id": agentID},
			ToType:   "db",
			ToID:     dbID,
		})
	}

	// runs_on edge: db → host when the db is local (loopback) — anchors a local
	// db to the host it runs on (enterprise#36).
	if rel, ok := dbcommon.LocalHostRunsOn(dbID, cfg.Host, e.hostID()); ok {
		obs.Relations = append(obs.Relations, rel)
	}

	e.mu.Lock()
	e.obs = obs
	e.ok = true
	e.mu.Unlock()
}
