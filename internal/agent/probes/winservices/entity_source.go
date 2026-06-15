package winservices

import "senhub-agent.go/internal/agent/services/entity"

// Entity rail (#185): the host's Windows service-control surface is reported
// as a single service.instance entity so a backend can anchor the
// per-service metrics (windows.service.state / windows.service.status) to a
// host. The id is winservices://localhost — the SCM the agent queries is the
// local machine's. The individual services are NOT entities: their state is
// high-cardinality, churning metric data, not stable topology.

const (
	entityTypeServiceInstance = "service.instance"
	idKeyServiceInstanceID    = "service.instance.id"
	serviceInstanceID         = "winservices://localhost"
)

// winServicesEntitySource feeds the entity rail with the static
// service.instance entity. Observe never blocks and never changes: the
// service-control surface exists for the lifetime of the probe.
type winServicesEntitySource struct {
	observation entity.Observation
}

func newEntitySource() *winServicesEntitySource {
	return &winServicesEntitySource{
		observation: entity.Observation{
			Entities: []entity.Entity{
				{
					Type:       entityTypeServiceInstance,
					ID:         map[string]any{idKeyServiceInstanceID: serviceInstanceID},
					Attributes: map[string]any{"service.name": "winservices"},
				},
			},
		},
	}
}

// Observe returns the static service.instance entity. ok is always true: the
// surface is present as soon as the probe runs, and an empty observation
// would be read by the tracker as "the entity is gone".
func (s *winServicesEntitySource) Observe() (entity.Observation, bool) {
	return s.observation, true
}
