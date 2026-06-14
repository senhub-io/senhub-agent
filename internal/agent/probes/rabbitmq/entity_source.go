package rabbitmq

import (
	"sync"

	"senhub-agent.go/internal/agent/services/entity"
)

// rabbitmqEntitySource feeds the entity rail for the rabbitmq probe.
// It reports the broker as a messaging.broker entity, and each discovered
// queue as a messaging.queue entity linked via a "contains" relation.
//
// The source is registered on OnStart and unregistered on OnShutdown.
// Observe is non-blocking and returns the last cached snapshot.
type rabbitmqEntitySource struct {
	// brokerID is the immutable identity of the broker, built once at
	// construction from the configured endpoint.
	brokerID map[string]any

	mu     sync.RWMutex
	up     bool
	attrs  map[string]any  // descriptive, may change per cycle
	queues []queueSnapshot // snapshot from last successful Collect
}

// queueSnapshot holds the minimal identity of a discovered queue.
type queueSnapshot struct {
	name  string
	vhost string
}

const (
	entityTypeBroker = "messaging.broker"
	entityTypeQueue  = "messaging.queue"
	relContains      = "contains"
)

// newRabbitmqEntitySource constructs the source from the parsed endpoint
// components. addr is host (or host:port) and port is the management port as
// an integer so the identity stays stable regardless of URL scheme.
func newRabbitmqEntitySource(addr string, port int64) *rabbitmqEntitySource {
	return &rabbitmqEntitySource{
		brokerID: map[string]any{
			"server.address":   addr,
			"server.port":      port,
			"messaging.system": "rabbitmq",
		},
	}
}

// setReachable marks the broker as reachable or not for the next Observe call.
// Pass a non-empty version string to surface it as a descriptive attribute.
func (s *rabbitmqEntitySource) setReachable(up bool, version string) {
	s.mu.Lock()
	s.up = up
	if up && version != "" {
		s.attrs = map[string]any{"messaging.system.version": version}
	} else if !up {
		s.attrs = nil
	}
	s.mu.Unlock()
}

// updateSnapshot stores the list of queues collected this cycle so Observe can
// emit them as messaging.queue entities linked to the broker.
func (s *rabbitmqEntitySource) updateSnapshot(queues []queueSnapshot) {
	s.mu.Lock()
	s.queues = queues
	s.mu.Unlock()
}

// Observe implements entity.Source. It returns the complete current
// observation: broker entity + one queue entity per discovered queue,
// plus "contains" relations from broker to each queue.
//
// ok=false is returned before the first successful reachability check
// (nothing collected yet is not the same as "broker deleted").
func (s *rabbitmqEntitySource) Observe() (entity.Observation, bool) {
	s.mu.RLock()
	up := s.up
	attrs := s.attrs
	queues := s.queues
	s.mu.RUnlock()

	if !up {
		return entity.Observation{}, false
	}

	broker := entity.Entity{
		Type:       entityTypeBroker,
		ID:         s.brokerID,
		Attributes: attrs,
	}
	obs := entity.Observation{
		Entities: []entity.Entity{broker},
	}

	for _, q := range queues {
		queueID := map[string]any{
			"server.address":   s.brokerID["server.address"],
			"server.port":      s.brokerID["server.port"],
			"messaging.system": "rabbitmq",
			"messaging.destination.name": q.name,
			"messaging.destination.partition.id": q.vhost,
		}
		obs.Entities = append(obs.Entities, entity.Entity{
			Type: entityTypeQueue,
			ID:   queueID,
			Attributes: map[string]any{
				"messaging.destination.kind": "queue",
				"messaging.virtual_host":     q.vhost,
			},
		})
		obs.Relations = append(obs.Relations, entity.Relation{
			Type:     relContains,
			FromType: entityTypeBroker,
			FromID:   s.brokerID,
			ToType:   entityTypeQueue,
			ToID:     queueID,
		})
	}

	return obs, true
}
