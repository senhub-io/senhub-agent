package ipmi

import (
	"fmt"
	"sync"

	"senhub-agent.go/internal/agent/services/entity"
)

const (
	entityTypeServiceInstance = "service.instance"
	idKeyServiceInstanceID    = "service.instance.id"
)

// ipmiEntitySource feeds the entity rail with a single service.instance
// entity representing the BMC / IPMI endpoint being supervised.
//
// The entity is synthesised from the probe configuration; no
// out-of-band IPMI queries are needed for identity.
type ipmiEntitySource struct {
	mu          sync.Mutex
	observation entity.Observation
	ready       bool
}

// newEntitySource builds the entity source from the probe config.
// The returned source immediately holds a valid observation (no
// async sweep needed — the identity is derived from config).
func newEntitySource(cfg ipmiConfig) *ipmiEntitySource {
	instanceID := "ipmi://localhost"
	if cfg.Mode == "remote" && cfg.RemoteHost != "" {
		instanceID = fmt.Sprintf("ipmi://%s", cfg.RemoteHost)
	}

	obs := entity.Observation{
		Entities: []entity.Entity{
			{
				Type: entityTypeServiceInstance,
				ID:   map[string]any{idKeyServiceInstanceID: instanceID},
				Attributes: map[string]any{
					"service.name": "ipmi",
				},
			},
		},
	}

	return &ipmiEntitySource{observation: obs, ready: true}
}

// Observe returns the cached observation. Always returns ready=true
// because the identity is known at construction time.
func (s *ipmiEntitySource) Observe() (entity.Observation, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.observation, s.ready
}
