package types

import (
	"sync"

	"senhub-agent.go/internal/agent/services/entity"
)

// NoOpEntitySource is for host-level probes and log conduits that don't
// monitor a distinct remote entity — their data is already attributed to
// the host entity emitted by the entity detector. Using this instead of
// nil satisfies the invariant test without polluting the entity graph.
type NoOpEntitySource struct{}

// Observe implements entity.Source. Always returns ok=true with an empty
// observation so the entity detector treats it as "nothing to report",
// not as a transient failure.
func (NoOpEntitySource) Observe() (entity.Observation, bool) {
	return entity.Observation{}, true
}

// SimpleEntitySource is a thread-safe entity.Source for probes that monitor
// a single remote target. Call SetUp(true, attrs) after a successful Collect
// and SetUp(false, nil) on failure so Toise gets accurate liveness.
type SimpleEntitySource struct {
	entityType string
	id         map[string]any
	mu         sync.RWMutex
	up         bool
	attrs      map[string]any
}

// NewSimpleEntitySource creates a source for a single entity with immutable type+id.
func NewSimpleEntitySource(entityType string, id map[string]any) *SimpleEntitySource {
	return &SimpleEntitySource{entityType: entityType, id: id}
}

// SetUp updates reachability and optional descriptive attributes.
// Call after every Collect: SetUp(true, attrs) on success, SetUp(false, nil) on failure.
func (s *SimpleEntitySource) SetUp(up bool, attrs map[string]any) {
	s.mu.Lock()
	s.up = up
	if attrs != nil {
		s.attrs = attrs
	}
	s.mu.Unlock()
}

// Observe implements entity.Source. Returns the entity when up, ok=false when down
// (keeps Toise's last-known state during transient outages — audit D3 contract).
func (s *SimpleEntitySource) Observe() (entity.Observation, bool) {
	s.mu.RLock()
	up, attrs, id, typ := s.up, s.attrs, s.id, s.entityType
	s.mu.RUnlock()
	if !up {
		return entity.Observation{}, false
	}
	return entity.Observation{
		Entities: []entity.Entity{{
			Type:       typ,
			ID:         id,
			Attributes: attrs,
		}},
	}, true
}
