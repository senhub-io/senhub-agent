package mysql

import (
	"fmt"
	"sync"

	"senhub-agent.go/internal/agent/probes/dbcommon"
	"senhub-agent.go/internal/agent/services/agentstate"
	"senhub-agent.go/internal/agent/services/entity"
	"senhub-agent.go/internal/agent/services/logger"
)

// mysqlEntitySource reports the monitored MySQL instance as a "db" entity
// (entity.type="db") following the uniform db identity rule:
//
//  1. operator-supplied instance_name → used verbatim and pinned at construction.
//  2. MySQL-reported @@server_uuid → fetched lazily on the first successful
//     collect cycle as "mysql:<uuid>" and pinned; the entity is NOT emitted
//     until the id is pinned (ok=false).
//  3. host:port degraded fallback → would apply only when neither of the above
//     is available; MySQL always provides @@server_uuid so this path is
//     unreachable for a healthy server, but is included for completeness.
//
// Once pinned the id is immutable for the process lifetime (Toise identity is
// exact + immutable; a changing id would re-key the db node in the consumer).
//
// The monitors edge is appended when agentstate.GetAgentInstanceID() is non-empty
// so the consumer can link this db to the agent that monitors it.
type mysqlEntitySource struct {
	cfg          config
	moduleLogger *logger.ModuleLogger

	mu          sync.Mutex
	role        dbcommon.Role
	version     string // server version banner, "" until the first cycle reports it
	environment string // hosting platform (self_hosted/rds/aurora/…), "" until reported
	pinnedID    string // "" until pinned
	idPinned    bool
	rolePinned  bool // true once the first successful collect cycle has run
}

func newMysqlEntitySource(cfg config, log *logger.ModuleLogger) *mysqlEntitySource {
	s := &mysqlEntitySource{cfg: cfg, moduleLogger: log}

	// Precedence 1: operator config overrides everything; pin immediately.
	if cfg.InstanceName != "" {
		s.pinnedID = cfg.InstanceName
		s.idPinned = true
	}
	return s
}

// isIDPinned reports whether the entity id has already been pinned (either via
// operator instance_name or via a previous pinServerUUID call). The probe uses
// this to skip the one-time @@server_uuid query after the id is locked in.
func (s *mysqlEntitySource) isIDPinned() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.idPinned
}

// pinServerUUID is called by the probe on the first successful collect cycle
// with the value from SELECT @@server_uuid. It pins the id unless already
// pinned (operator override wins). Returns the pinned id.
func (s *mysqlEntitySource) pinServerUUID(uuid string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.idPinned {
		return // operator config wins; never overwrite
	}
	if uuid != "" {
		s.pinnedID = fmt.Sprintf("mysql:%s", uuid)
		s.idPinned = true
		return
	}
	// Degraded fallback: host:port when no stable tech id is available.
	// MySQL always reports @@server_uuid on a healthy connection so this
	// branch is reached only when the query itself failed.
	s.pinnedID = fmt.Sprintf("%s:%d", s.cfg.Host, s.cfg.Port)
	s.idPinned = true
}

// updateRole is called by the probe after each collect cycle to keep the
// entity's replication role attribute current and to signal that the first
// successful cycle has completed.
func (s *mysqlEntitySource) updateRole(role dbcommon.Role) {
	s.mu.Lock()
	s.role = role
	s.rolePinned = true
	s.mu.Unlock()
}

// setVersion records the server version banner (@@version) so it rides the
// entity as the descriptive db.system.version attribute (toise#216 AT1).
func (s *mysqlEntitySource) setVersion(v string) {
	s.mu.Lock()
	s.version = v
	s.mu.Unlock()
}

// setEnvironment records the detected hosting platform (self_hosted/rds/aurora/…)
// so it rides the entity as db.deployment.platform (toise#216 AT3).
func (s *mysqlEntitySource) setEnvironment(env string) {
	s.mu.Lock()
	s.environment = env
	s.mu.Unlock()
}

// Observe returns the MySQL instance entity. ok=false until both the id has
// been pinned and the first successful collect cycle has run (rolePinned), so
// we never emit an entity whose id could change on the next cycle.
func (s *mysqlEntitySource) Observe() (entity.Observation, bool) {
	s.mu.Lock()
	role := s.role
	version := s.version
	environment := s.environment
	idPinned := s.idPinned
	rolePinned := s.rolePinned
	pinnedID := s.pinnedID
	s.mu.Unlock()

	// When instance_name is set the id is pinned at construction; we still
	// wait for the first successful collect (rolePinned) so we don't emit
	// before the probe has confirmed the connection is alive.
	if !idPinned || !rolePinned {
		return entity.Observation{}, false
	}

	id := map[string]any{
		"db.instance.id": pinnedID,
	}
	attrs := map[string]any{
		"db.system.name":   "mysql",
		"server.address":   s.cfg.Host,
		"server.port":      int64(s.cfg.Port),
		"replication.role": role.String(),
	}
	if version != "" {
		attrs["db.system.version"] = version
	}
	if environment != "" {
		attrs["db.deployment.platform"] = environment
	}

	obs := entity.Observation{
		Entities: []entity.Entity{
			{
				Type:       "db",
				ID:         id,
				Attributes: attrs,
			},
		},
	}

	// monitors edge: agent → db, emitted only when the agent id is available.
	if agentID := agentstate.GetAgentInstanceID(); agentID != "" {
		obs.Relations = append(obs.Relations, entity.Relation{
			Type:     "monitors",
			FromType: "service.instance",
			FromID:   map[string]any{"service.instance.id": agentID},
			ToType:   "db",
			ToID:     map[string]any{"db.instance.id": pinnedID},
		})
	}

	return obs, true
}
