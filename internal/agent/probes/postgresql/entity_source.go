package postgresql

import (
	"fmt"
	"sync"

	"senhub-agent.go/internal/agent/probes/dbcommon"
	"senhub-agent.go/internal/agent/services/entity"
	"senhub-agent.go/internal/agent/services/logger"
)

// pgEntitySource implements entity.Source for a PostgreSQL instance.
// Entity type "db" with identifying attributes following the OTel db.*
// semantic conventions (db.instance.id, db.system.name).
type pgEntitySource struct {
	cfg          config
	moduleLogger *logger.ModuleLogger

	mu          sync.Mutex
	instanceID  string // db.instance.id (set once from host:port)
	environment dbcommon.Environment
	role        dbcommon.Role
	obs         entity.Observation
	ready       bool
}

func newPgEntitySource(cfg config, log *logger.ModuleLogger) *pgEntitySource {
	return &pgEntitySource{
		cfg:          cfg,
		moduleLogger: log,
		environment:  dbcommon.EnvironmentUnknown,
		role:         dbcommon.RoleStandalone,
	}
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

// update rebuilds the entity observation from current state. Called at
// the end of each Collect cycle so the detector picks up fresh attrs.
func (s *pgEntitySource) update(instance string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.instanceID == "" {
		s.instanceID = fmt.Sprintf("postgresql://%s", instance)
	}

	dbID := map[string]any{
		"db.instance.id":  s.instanceID,
		"db.system.name": "postgresql",
	}

	attrs := map[string]any{
		"server.address": s.cfg.Host,
		"server.port":    int64(s.cfg.Port),
		"environment":    string(s.environment),
		"role":           s.role.String(),
	}

	s.obs = entity.Observation{
		Entities: []entity.Entity{
			{
				Type:       "db",
				ID:         dbID,
				Attributes: attrs,
			},
		},
	}
	s.ready = true
}

// Observe implements entity.Source. Returns the last good observation
// (ok=false before the first successful update).
func (s *pgEntitySource) Observe() (entity.Observation, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.obs, s.ready
}
