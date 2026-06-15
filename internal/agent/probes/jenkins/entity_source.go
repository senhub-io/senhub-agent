package jenkins

import (
	"senhub-agent.go/internal/agent/services/entity"
)

// Entity rail (#185): the monitored Jenkins controller as a service.instance
// entity — "the thing this probe monitors". Identity is the controller's
// host:port carried as a jenkins:// URI, immutable for the probe's lifetime
// (a leased IP or a build number would belong in attributes, never in ID).
// The entity is observer-independent: two agents watching the same controller
// must agree on it, so no agent-local fact rides in the identity.
const (
	entityTypeServiceInstance = "service.instance"
	idKeyServiceInstanceID    = "service.instance.id"
)

// entitySource emits the Jenkins controller as a single service.instance
// entity. The observation is static for the probe's lifetime (the controller's
// identity does not change between cycles), so Observe never blocks and never
// fails — it returns the same cached observation every cycle.
type entitySource struct {
	obs entity.Observation
}

// newEntitySource builds the source from the controller's host:port instance.
// The service.instance.id is "jenkins://<host>:<port>" — the frozen identity
// shape for this probe's entity.
func newEntitySource(instance string) *entitySource {
	id := map[string]any{idKeyServiceInstanceID: "jenkins://" + instance}
	return &entitySource{
		obs: entity.Observation{
			Entities: []entity.Entity{
				{Type: entityTypeServiceInstance, ID: id},
			},
		},
	}
}

// Observe returns the controller entity. Always ok=true: the controller's
// identity is known at construction and never decays, so there is no transient
// view to protect.
func (s *entitySource) Observe() (entity.Observation, bool) {
	return s.obs, true
}
