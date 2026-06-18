package postgresql

import (
	"fmt"
	"sync"

	"senhub-agent.go/internal/agent/probes/dbcommon"
	"senhub-agent.go/internal/agent/services/agentstate"
	"senhub-agent.go/internal/agent/services/entity"
	"senhub-agent.go/internal/agent/services/logger"
)

// pgEntitySource implements entity.Source for a PostgreSQL instance.
// Entity type "db" with identifying attributes following the OTel db.*
// semantic conventions (db.instance.id, db.system.name).
//
// Identity precedence (Toise db identity contract):
//  1. operator config key "instance_name" — pinned at construction, emitted immediately;
//  2. tech-reported stable id "postgresql:<system_identifier>" fetched lazily on first
//     successful collect — entity NOT emitted until pinned;
//  3. host:port degraded fallback — only when neither (1) nor (2) is available;
//     pinned at construction, emitted immediately.
//
// Once pinned, instanceID never changes for the process lifetime.
type pgEntitySource struct {
	cfg          config
	moduleLogger *logger.ModuleLogger

	mu          sync.Mutex
	instanceID  string // db.instance.id — set once, never changed
	environment dbcommon.Environment
	role        dbcommon.Role
	version     string // server version (e.g. "16.1"), "" until first reported
	obs         entity.Observation
	// ready is true once the entity has been emitted at least once. It remains
	// false until instanceID is pinned from the tech source (or immediately when
	// instance_name or the host:port fallback applies).
	ready bool
}

func newPgEntitySource(cfg config, log *logger.ModuleLogger) *pgEntitySource {
	s := &pgEntitySource{
		cfg:          cfg,
		moduleLogger: log,
		environment:  dbcommon.EnvironmentUnknown,
		role:         dbcommon.RoleStandalone,
	}
	// Precedence 1: operator-provided name — stable, pin immediately.
	if cfg.InstanceName != "" {
		s.instanceID = cfg.InstanceName
		s.ready = false // will be set on first update()
	}
	return s
}

// setEnvironment records the detected managed environment. Called by
// collectOverview after parsing the version banner.
func (s *pgEntitySource) setEnvironment(env dbcommon.Environment) {
	s.mu.Lock()
	s.environment = env
	s.mu.Unlock()
}

// setRole records the detected replication role. Called by
// collectReplication each cycle.
func (s *pgEntitySource) setRole(role dbcommon.Role) {
	s.mu.Lock()
	s.role = role
	s.mu.Unlock()
}

// setVersion records the server version so it rides the entity as the
// descriptive db.system.version attribute (toise#216 AT1). Called by
// collectOverview with the parsed short version (e.g. "16.1").
func (s *pgEntitySource) setVersion(v string) {
	s.mu.Lock()
	s.version = v
	s.mu.Unlock()
}

// pinTechID records the stable tech-reported system identifier on the first
// successful call and returns true when the pin was applied (subsequent calls
// are no-ops and return false). The caller (Collect) must call update() after
// this returns true to rebuild the observation.
//
// systemIdentifier is the value from SELECT system_identifier FROM
// pg_control_system(). An empty string is treated as "not available" so the
// source stays in the pending state.
func (s *pgEntitySource) pinTechID(systemIdentifier string) bool {
	if systemIdentifier == "" {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.instanceID != "" {
		// Already pinned (either from instance_name or a prior pinTechID call).
		return false
	}
	s.instanceID = fmt.Sprintf("postgresql:%s", systemIdentifier)
	return true
}

// pinnedID returns the current pinned instance ID (empty string when not yet
// resolved).
func (s *pgEntitySource) pinnedID() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.instanceID
}

// update rebuilds the entity observation from current state. Called at
// the end of each Collect cycle once the instance ID is pinned.
// Returns ok=false and does not update ready when instanceID is still empty.
func (s *pgEntitySource) update(fallbackHostPort string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.instanceID == "" {
		// No stable id yet; apply host:port fallback only for probes where
		// there is genuinely no tech source. For PostgreSQL, the tech source
		// (pg_control_system) is always queried first — reaching here means
		// the query has not succeeded yet, so keep pending rather than emitting
		// a transient host:port that would re-key the entity when the real id
		// arrives.
		//
		// Exception: if instance_name is also absent, apply host:port as the
		// documented db degraded fallback after the probe has been unable to
		// resolve a tech id (signalled by fallbackHostPort being the sentinel
		// value matching cfg.Host:cfg.Port — the probe passes this only when
		// it decides to give up on the tech source).
		if fallbackHostPort == "" {
			return false
		}
		s.instanceID = fallbackHostPort
	}

	dbID := map[string]any{
		"db.instance.id": s.instanceID,
	}

	attrs := map[string]any{
		"db.system.name":         "postgresql",
		"server.address":         s.cfg.Host,
		"server.port":            int64(s.cfg.Port),
		"db.deployment.platform": string(s.environment),
		"replication.role":       s.role.String(),
	}
	if s.version != "" {
		attrs["db.system.version"] = s.version
	}

	obs := entity.Observation{
		Entities: []entity.Entity{
			{
				Type:       "db",
				ID:         dbID,
				Attributes: attrs,
			},
		},
	}

	// monitors edge: agent → db. The edge is embedded on the source entity
	// (the service.instance side). We append it to the observation's Relations
	// so the detector folds it onto the agent entity.
	if agentID := agentstate.GetAgentInstanceID(); agentID != "" {
		obs.Relations = append(obs.Relations, entity.Relation{
			Type:     "monitors",
			FromType: "service.instance",
			FromID:   map[string]any{"service.instance.id": agentID},
			ToType:   "db",
			ToID:     map[string]any{"db.instance.id": s.instanceID},
		})
	}

	s.obs = obs
	s.ready = true
	return true
}

// Observe implements entity.Source. Returns the last good observation
// (ok=false before the first successful update).
func (s *pgEntitySource) Observe() (entity.Observation, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.obs, s.ready
}
