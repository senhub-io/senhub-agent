package winservices

import (
	"sync"

	"senhub-agent.go/internal/agent/services/entity"
)

// Entity rail (#185): the host's Windows service-control surface is reported
// as a single service.instance entity so a backend can anchor the
// per-service metrics (windows.service.state / windows.service.status) to a
// host. The individual services are NOT entities: their state is
// high-cardinality, churning metric data, not stable topology.
//
// The id is winservices@<host.id>. It used to be the constant
// "winservices://localhost", which contains no host component at all and was
// therefore byte-identical on every machine: every Windows host in a fleet
// collapsed onto ONE service.instance node, and since each host still drew its
// own runs_on edge to itself, that single node fanned out to all of them and
// joined them transitively (#742). The form now follows the contract for a
// local thing with no stable id of its own: <service.name>@<host.id>.

const (
	entityTypeServiceInstance = "service.instance"
	entityTypeHost            = "host"
	idKeyServiceInstanceID    = "service.instance.id"
	idKeyHost                 = "host.id"
	serviceName               = "winservices"
	legacyServiceInstanceID   = "winservices://localhost"
	relRunsOn                 = "runs_on"
)

// winServicesEntitySource feeds the entity rail with the static
// service.instance entity and its runs_on → host relation. Observe never
// blocks; the entity itself is stable for the lifetime of the probe, but the
// relation is only emitted once a host ID is known.
type winServicesEntitySource struct {
	// rekey retires the pre-#742 constant identity; built on first use, once
	// the host id is known.
	rekey *entity.RekeyAnnouncer

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

	// No host id, no entity. The identity is built from it, and inventing one
	// is what produced the fleet-wide collapse in the first place — better a
	// visible gap than a node that is wrong on every machine.
	if hostID == "" {
		return entity.Observation{}, false
	}
	instanceID := serviceName + "@" + hostID
	serviceID := map[string]any{idKeyServiceInstanceID: instanceID}
	obs := entity.Observation{
		Entities: []entity.Entity{
			{
				Type:       entityTypeServiceInstance,
				ID:         serviceID,
				Attributes: map[string]any{"service.name": "winservices"},
			},
		},
	}
	obs.Relations = []entity.Relation{
		{
			Type:     relRunsOn,
			FromType: entityTypeServiceInstance,
			FromID:   serviceID,
			ToType:   entityTypeHost,
			ToID:     map[string]any{idKeyHost: hostID},
		},
	}

	// Retire the collapsed node explicitly rather than letting it expire, so
	// the consumer reads "someone decided" instead of "the producer went
	// quiet". Built lazily because the legacy id is fixed but the new one
	// needs the host id, which is not known at construction.
	s.mu.Lock()
	if s.rekey == nil {
		s.rekey = entity.NewRekeyAnnouncer(entityTypeServiceInstance,
			idKeyServiceInstanceID, legacyServiceInstanceID, instanceID)
	}
	rekey := s.rekey
	s.mu.Unlock()

	rekey.Announce()
	if rel, ok := rekey.SameAs(); ok {
		obs.Relations = append(obs.Relations, rel)
	}

	return obs, true
}
