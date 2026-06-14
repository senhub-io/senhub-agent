package activemq

import (
	"sync"

	"senhub-agent.go/internal/agent/services/entity"
)

// activemqEntitySource implements entity.Source for the ActiveMQ probe.
// It reports the broker as a messaging.broker entity and, after each
// successful Collect, emits the known destinations (queues and topics) as
// messaging.destination entities with contains relations from the broker.
//
// The entity type and ID keys are frozen with the Toise team:
//
//	type:                messaging.broker
//	id keys:             server.address, server.port, messaging.system
//	destination type:    messaging.destination
//	destination id keys: messaging.system, messaging.destination.name
//	relation type:       contains
type activemqEntitySource struct {
	// Immutable broker identity, set once in the constructor.
	brokerID map[string]any

	mu           sync.RWMutex
	up           bool
	attrs        map[string]any
	destinations []destinationSnapshot
}

type destinationSnapshot struct {
	name     string
	destType string // "queue" or "topic"
}

func newActivemqEntitySource(addr string, port int) *activemqEntitySource {
	return &activemqEntitySource{
		brokerID: map[string]any{
			"server.address":   addr,
			"server.port":      int64(port),
			"messaging.system": "activemq",
		},
	}
}

// setReachable updates the broker liveness and optional descriptive attrs.
// Call after every Collect: setReachable(true, version) on success,
// setReachable(false, "") on fatal error (audit D3 — Toise keeps last-good
// view during transient outages instead of deleting the whole broker tree).
func (s *activemqEntitySource) setReachable(up bool, version string) {
	s.mu.Lock()
	s.up = up
	if version != "" {
		s.attrs = map[string]any{"messaging.activemq.version": version}
	}
	s.mu.Unlock()
}

// updateSnapshot replaces the destination list after a successful Collect.
// Destinations are per-broker children (queues and topics) reported as
// messaging.destination entities with contains relations from the broker.
func (s *activemqEntitySource) updateSnapshot(dests []destinationSnapshot) {
	s.mu.Lock()
	s.destinations = dests
	s.mu.Unlock()
}

// Observe implements entity.Source. Non-blocking; returns the last cached
// snapshot. Returns ok=false when the broker is unreachable so the detector
// reuses the previous observation rather than deleting the entity tree.
func (s *activemqEntitySource) Observe() (entity.Observation, bool) {
	s.mu.RLock()
	up, attrs, dests := s.up, s.attrs, s.destinations
	s.mu.RUnlock()

	if !up {
		return entity.Observation{}, false
	}

	broker := entity.Entity{
		Type:       "messaging.broker",
		ID:         s.brokerID,
		Attributes: attrs,
	}
	obs := entity.Observation{
		Entities: []entity.Entity{broker},
	}

	for _, d := range dests {
		destID := map[string]any{
			"messaging.system":           "activemq",
			"messaging.destination.name": d.name,
		}
		destAttrs := map[string]any{
			"messaging.destination.kind": d.destType,
		}
		obs.Entities = append(obs.Entities, entity.Entity{
			Type:       "messaging.destination",
			ID:         destID,
			Attributes: destAttrs,
		})
		obs.Relations = append(obs.Relations, entity.Relation{
			Type:     "contains",
			FromType: "messaging.broker",
			FromID:   s.brokerID,
			ToType:   "messaging.destination",
			ToID:     destID,
		})
	}

	return obs, true
}
