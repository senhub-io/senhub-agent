package haproxy

import (
	"strconv"
	"sync"

	"senhub-agent.go/internal/agent/services/entity"
)

// haproxyEntitySource exposes the monitored HAProxy instance as a
// service.instance entity on the OTel entity rail. The identity is
// "haproxy://<host>:<port>" — stable and immutable for the lifetime of
// the probe instance. Reachability is updated by Collect on every
// cycle: ok=false before the first successful fetch keeps the detector
// from publishing a stale entity; ok=true after first success keeps
// the entity alive across transient fetch failures (audit D3).
type haproxyEntitySource struct {
	instanceID string

	mu      sync.RWMutex
	reached bool
	attrs   map[string]any
}

func newHAProxyEntitySource(addr string, port int) *haproxyEntitySource {
	instanceID := "haproxy://" + addr + ":" + strconv.Itoa(port)
	return &haproxyEntitySource{
		instanceID: instanceID,
		attrs: map[string]any{
			"service.name":   "haproxy",
			"server.address": addr,
			"server.port":    int64(port),
		},
	}
}

// setReachable updates the liveness flag.
// Called from Collect; safe to call concurrently with Observe.
func (s *haproxyEntitySource) setReachable(up bool) {
	s.mu.Lock()
	s.reached = up
	s.mu.Unlock()
}

// Observe implements entity.Source. Returns the service.instance entity
// when the endpoint has been reached at least once. ok=false before the
// first successful fetch (nothing to publish yet is not "entity gone").
func (s *haproxyEntitySource) Observe() (entity.Observation, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if !s.reached {
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
