package envoy

import (
	"strconv"
	"sync"

	"senhub-agent.go/internal/agent/services/entity"
)

// envoyEntitySource feeds the entity rail with the single Envoy instance this
// probe monitors. Toise strict v0.5.0 contract:
//
//	Type: "service.instance"
//	ID:   {"service.instance.id": "envoy://<host>:<port>"}
//	Attrs: service.name, server.address, server.port (int64)
//
// Reachability is updated by Collect after each scrape attempt so Toise sees
// accurate liveness (audit D3). No relations are emitted: monitors requires
// agentstate.GetAgentInstanceID() which is not yet available (#433).
type envoyEntitySource struct {
	instanceID string
	attrs      map[string]any

	mu sync.RWMutex
	up bool
}

func newEnvoyEntitySource(addr, port string) *envoyEntitySource {
	portInt, _ := strconv.ParseInt(port, 10, 64)
	instanceID := "envoy://" + addr + ":" + port
	attrs := map[string]any{
		"service.name":   "envoy",
		"server.address": addr,
		"server.port":    portInt,
	}
	return &envoyEntitySource{instanceID: instanceID, attrs: attrs}
}

// setReachable updates the probe's liveness state after each scrape.
// Call with up=true on success (passing the Envoy version when available),
// up=false on any failure so Toise does not receive stale state events.
// The version parameter is accepted for forward-compatibility but not stored
// until service.version is confirmed in the Toise conformance fixture.
func (s *envoyEntitySource) setReachable(up bool, _ string) {
	s.mu.Lock()
	s.up = up
	s.mu.Unlock()
}

// Observe implements entity.Source. Returns ok=false when Envoy was not
// reachable on the last cycle, so the detector reuses its cached snapshot
// rather than emitting a delete event on a transient outage (audit D3).
func (s *envoyEntitySource) Observe() (entity.Observation, bool) {
	s.mu.RLock()
	up := s.up
	s.mu.RUnlock()
	if !up {
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
