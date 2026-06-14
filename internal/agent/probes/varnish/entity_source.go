package varnish

import (
	"sync"

	"senhub-agent.go/internal/agent/services/entity"
)

// varnishEntitySource implements entity.Source for the Varnish Cache probe.
// It exposes the monitored Varnish instance as a cache.server entity so Toise
// can inventory it automatically. The entity is emitted only when varnishstat
// succeeds (up=true); a failing instance is not reported (ok=false keeps the
// previous good snapshot alive in the detector rather than deleting the entity
// on each transient error).
type varnishEntitySource struct {
	id   map[string]any
	mu   sync.RWMutex
	up   bool
	// attrs carries mutable, observer-independent descriptive attributes
	// (instance name when configured). The id map is immutable after
	// construction so it needs no lock.
	attrs map[string]any
}

// newVarnishEntitySource builds the entity source from the probe config.
// The instance name, when set, disambiguates several Varnish instances on the
// same host (varnishstat -n); it becomes a descriptive attribute, not part of
// the identity (the identity is the local address + type pair).
func newVarnishEntitySource(instanceName string) *varnishEntitySource {
	s := &varnishEntitySource{
		id: map[string]any{
			"server.address": "localhost",
			"server.type":    "varnish",
		},
	}
	if instanceName != "" {
		s.attrs = map[string]any{"instance.name": instanceName}
	}
	return s
}

// setReachable is called by Collect each cycle. When up is true the entity is
// (re-)emitted; when false ok=false is returned to keep the last good snapshot.
func (s *varnishEntitySource) setReachable(up bool) {
	s.mu.Lock()
	s.up = up
	s.mu.Unlock()
}

// Observe implements entity.Source. Non-blocking; safe to call from the
// detector goroutine.
func (s *varnishEntitySource) Observe() (entity.Observation, bool) {
	s.mu.RLock()
	up := s.up
	attrs := s.attrs
	s.mu.RUnlock()

	if !up {
		return entity.Observation{}, false
	}

	return entity.Observation{
		Entities: []entity.Entity{{
			Type:       "cache.server",
			ID:         s.id,
			Attributes: attrs,
		}},
	}, true
}
