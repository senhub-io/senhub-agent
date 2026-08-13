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
	// rekey announces the 0.5.4 identity migration for a local instance whose
	// id was host-scoped. nil when nothing was re-keyed (remote target).
	rekey *dbcommon.RekeyAnnouncer

	mu  sync.Mutex
	obs entity.Observation
	ok  bool
}

// newEntityObserver builds the observer and pins the db.instance.id
// immediately. It never returns nil.
// hostID resolves the agent host; it is a parameter rather than a package
// call so the pinned identity is reproducible in tests — the id is frozen at
// construction and a test cannot re-pin it afterwards.
func newEntityObserver(cfg probeConfig, hostID func() string) *entityObserver {
	if hostID == nil {
		hostID = dbcommon.HostID
	}
	id := cfg.InstanceName
	if id == "" {
		// Redis has no persistent server id, so this fallback is the norm
		// rather than a degraded case: every default install listens on
		// loopback and would otherwise share one identity fleet-wide (#740).
		id = dbcommon.FallbackInstanceID("redis", cfg.Host, cfg.Port, hostID())
	}
	return &entityObserver{
		pinnedID: id,
		hostID:   hostID,
		// Only when the fallback applies: an operator-named instance was never
		// keyed on address:port, so it has nothing to retire.
		rekey: rekeyFor(cfg, hostID),
	}
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

	// The 0.5.4 identity migration: retire the pre-scoping node explicitly and
	// alias it to this one, so the consumer reads "someone decided" rather than
	// "the agent went quiet". Self-limiting to a few cycles.
	e.rekey.Announce()
	if rel, ok := e.rekey.SameAs(); ok {
		obs.Relations = append(obs.Relations, rel)
	}

	e.mu.Lock()
	e.obs = obs
	e.ok = true
	e.mu.Unlock()
}

// rekeyFor builds the migration announcer only when this observer actually
// uses the host-scoped fallback. An operator-supplied instance_name was never
// keyed on address:port, so announcing a retirement for it would name a node
// the consumer has never seen.
func rekeyFor(cfg probeConfig, hostID func() string) *dbcommon.RekeyAnnouncer {
	if cfg.InstanceName != "" {
		return nil
	}
	return dbcommon.NewRekeyAnnouncer("redis", cfg.Host, cfg.Port, hostID())
}
