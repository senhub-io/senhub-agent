package varnish

import (
	"sync"

	"senhub-agent.go/internal/agent/services/entity"
)

// varnishEntitySource implements entity.Source for the Varnish Cache probe.
// It exposes the monitored Varnish instance as a service.instance entity
// (Toise v0.5.0 strict contract) so Toise can inventory it automatically.
// The entity is emitted only when varnishstat succeeds (up=true); a failing
// instance returns ok=false so the detector keeps the previous good snapshot
// alive rather than deleting the entity on each transient error.
type varnishEntitySource struct {
	instanceID string
	mu         sync.RWMutex
	up         bool
	attrs      map[string]any
}

// newVarnishEntitySource builds the entity source from the probe config.
// Varnish has no configurable network address or port — it is always local —
// so the instance ID is the fixed URI "varnish://localhost". When an instance
// name is configured (varnishstat -n), it is stored as a descriptive attribute
// to disambiguate multiple Varnish instances on the same host; it is not part
// of the identity.
func newVarnishEntitySource(instanceName string) *varnishEntitySource {
	s := &varnishEntitySource{
		instanceID: "varnish://localhost",
		attrs: map[string]any{
			"service.name":   "varnish",
			"server.address": "localhost",
		},
	}
	if instanceName != "" {
		s.attrs["varnish.instance.name"] = instanceName
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
	defer s.mu.RUnlock()

	if !s.up {
		return entity.Observation{}, false
	}

	return entity.Observation{
		Entities: []entity.Entity{{
			Type:       "service.instance",
			ID:         map[string]any{"service.instance.id": s.instanceID},
			Attributes: s.attrs,
		}},
	}, true
}
