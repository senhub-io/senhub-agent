package pulsar

import (
	"net/url"
	"sync"

	"senhub-agent.go/internal/agent/services/entity"
)

// pulsarEntitySource feeds the entity rail with the Apache Pulsar broker this
// probe monitors. It reports a single messaging.broker entity with the broker
// host and port as identity. The entity is emitted only while the broker
// readiness endpoint responds (up=true); a transient failure returns ok=false
// so the tracker reuses the last good snapshot rather than emitting a delete.
type pulsarEntitySource struct {
	id   map[string]any
	mu   sync.RWMutex
	up   bool
	port any // int64
}

// newPulsarEntitySource derives the entity identity from the probe's endpoint
// URL. The port defaults to 8080 when the URL has no explicit port, matching
// the Apache Pulsar Admin REST API default.
func newPulsarEntitySource(endpoint string) *pulsarEntitySource {
	host, port := hostPortFromEndpoint(endpoint)
	return &pulsarEntitySource{
		id: map[string]any{
			"server.address":   host,
			"server.port":      port,
			"messaging.system": "pulsar",
		},
	}
}

// hostPortFromEndpoint extracts host and port (as int64) from an HTTP(S) URL.
// Missing port defaults to 8080, the Pulsar Admin REST API default.
func hostPortFromEndpoint(rawURL string) (host string, port int64) {
	u, err := url.Parse(rawURL)
	if err != nil || u.Hostname() == "" {
		return rawURL, 8080
	}
	host = u.Hostname()
	p := u.Port()
	if p == "" {
		return host, 8080
	}
	var n int64
	for _, c := range p {
		if c < '0' || c > '9' {
			return host, 8080
		}
		n = n*10 + int64(c-'0')
	}
	return host, n
}

// setReachable is called from Collect to report whether the broker readiness
// endpoint responded successfully this cycle.
func (s *pulsarEntitySource) setReachable(up bool) {
	s.mu.Lock()
	s.up = up
	s.mu.Unlock()
}

// Observe implements entity.Source. It returns the Pulsar messaging.broker
// entity when the broker is reachable, or (_, false) on a transient failure
// so the detector keeps the last good snapshot alive.
func (s *pulsarEntitySource) Observe() (entity.Observation, bool) {
	s.mu.RLock()
	up := s.up
	s.mu.RUnlock()

	if !up {
		return entity.Observation{}, false
	}
	return entity.Observation{
		Entities: []entity.Entity{{
			Type: "messaging.broker",
			ID:   s.id,
		}},
	}, true
}
