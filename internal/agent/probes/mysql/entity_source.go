package mysql

import (
	"fmt"
	"sync"

	"senhub-agent.go/internal/agent/probes/dbcommon"
	"senhub-agent.go/internal/agent/services/entity"
	"senhub-agent.go/internal/agent/services/logger"
)

// mysqlEntitySource reports the monitored MySQL instance as a "db"
// entity (OTel Resource detector style: entity.type="db",
// db.instance.id=mysql://<host>:<port>, db.system.name=mysql).
//
// The entity is emitted once at startup and refreshed every collect cycle.
// The replication role is updated by the probe after each collect cycle.
type mysqlEntitySource struct {
	cfg          config
	moduleLogger *logger.ModuleLogger

	mu   sync.Mutex
	role dbcommon.Role
	ok   bool
}

func newMysqlEntitySource(cfg config, log *logger.ModuleLogger) *mysqlEntitySource {
	return &mysqlEntitySource{cfg: cfg, moduleLogger: log}
}

// updateRole is called by the probe after each collect cycle to keep the
// entity's replication role attribute current.
func (s *mysqlEntitySource) updateRole(role dbcommon.Role) {
	s.mu.Lock()
	s.role = role
	s.ok = true
	s.mu.Unlock()
}

// Observe returns the MySQL instance entity. ok=false before the first
// successful collect cycle — the probe signals ok via updateRole.
func (s *mysqlEntitySource) Observe() (entity.Observation, bool) {
	s.mu.Lock()
	role := s.role
	ok := s.ok
	s.mu.Unlock()

	if !ok {
		return entity.Observation{}, false
	}

	instanceID := fmt.Sprintf("mysql://%s:%d", s.cfg.Host, s.cfg.Port)
	id := map[string]any{
		"db.instance.id":  instanceID,
		"db.system.name":  "mysql",
	}
	attrs := map[string]any{
		"server.address":  s.cfg.Host,
		"server.port":     int64(s.cfg.Port),
		"db.system.name":  "mysql",
		"replication.role": role.String(),
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
	return obs, true
}
