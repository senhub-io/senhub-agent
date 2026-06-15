package ceph

import (
	"sync"

	"senhub-agent.go/internal/agent/services/entity"
)

const (
	entityTypeServiceInstance = "service.instance"
	idKeyServiceInstanceID    = "service.instance.id"
)

// cephEntitySource reports the monitored Ceph cluster as a service.instance
// entity. The entity is static (the endpoint never changes at runtime), so
// Observe always returns the same single-entity observation once bootstrapped.
type cephEntitySource struct {
	once sync.Once
	obs  entity.Observation
}

func newCephEntitySource(endpoint string) *cephEntitySource {
	s := &cephEntitySource{}
	s.once.Do(func() {
		id := "ceph://" + endpoint
		s.obs = entity.Observation{
			Entities: []entity.Entity{
				{
					Type: entityTypeServiceInstance,
					ID:   map[string]any{idKeyServiceInstanceID: id},
				},
			},
		}
	})
	return s
}

// Observe returns the static Ceph service.instance entity. Always ok=true:
// the identity is derived from static config, not from a live probe result.
func (s *cephEntitySource) Observe() (entity.Observation, bool) {
	return s.obs, true
}
