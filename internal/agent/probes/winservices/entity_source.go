package winservices

import (
	"sync"

	"senhub-agent.go/internal/agent/services/entity"
)

// Entity rail (#185): the host's Windows service-control surface is reported
// as a single service.instance entity so a backend can anchor the
// per-service metrics (windows.service.state / windows.service.status) to a
// host. The id is winservices://localhost — the SCM the agent queries is the
// local machine's. The individual services are NOT entities: their state is
// high-cardinality, churning metric data, not stable topology.

const (
	entityTypeServiceInstance = "service.instance"
	entityTypeHost            = "host"
	idKeyServiceInstanceID    = "service.instance.id"
	idKeyHost                 = "host.id"
	serviceInstanceID         = "winservices://localhost"
	relRunsOn                 = "runs_on"
)

// winServicesEntitySource feeds the entity rail with the static
// service.instance entity and its runs_on → host relation. Observe never
// blocks; the entity itself is stable for the lifetime of the probe, but the
// relation is only emitted once a host ID is known.
type winServicesEntitySource struct {
	mu     sync.RWMutex
	hostID string
}

func newEntitySource() *winServicesEntitySource {
	return &winServicesEntitySource{}
}

// setHostID stores the host's stable identity so Observe can attach the
// runs_on → host relation. Called once per Collect cycle; safe for concurrent
// use with Observe (the detector may call Observe from its own goroutine).
func (s *winServicesEntitySource) setHostID(id string) {
	s.mu.Lock()
	s.hostID = id
	s.mu.Unlock()
}

// Observe returns the service.instance entity and, when a host ID is known,
// the runs_on → host relation that attaches it to the host node in Toise.
// ok is always true: the service-control surface exists for the lifetime of
// the probe, and an empty observation would be read by the tracker as "the
// entity is gone".
func (s *winServicesEntitySource) Observe() (entity.Observation, bool) {
	s.mu.RLock()
	hostID := s.hostID
	s.mu.RUnlock()

	serviceID := map[string]any{idKeyServiceInstanceID: serviceInstanceID}
	obs := entity.Observation{
		Entities: []entity.Entity{
			{
				Type:       entityTypeServiceInstance,
				ID:         serviceID,
				Attributes: map[string]any{"service.name": "winservices"},
			},
		},
	}
	if hostID != "" {
		obs.Relations = []entity.Relation{
			{
				Type:     relRunsOn,
				FromType: entityTypeServiceInstance,
				FromID:   serviceID,
				ToType:   entityTypeHost,
				ToID:     map[string]any{idKeyHost: hostID},
			},
		}
	}
	return obs, true
}
