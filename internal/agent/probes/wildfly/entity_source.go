package wildfly

import (
	"net/url"
	"sync"

	"senhub-agent.go/internal/agent/services/entity"
)

// wildflyEntitySource feeds the entity rail with the WildFly / JBoss server
// this probe monitors. It reports a single app.server entity with the endpoint
// host and port as identity. The entity is emitted only while the management
// API is reachable (up=true); a transient failure returns ok=false so the
// tracker reuses the last good snapshot rather than emitting a delete.
type wildflyEntitySource struct {
	id      map[string]any
	mu      sync.RWMutex
	up      bool
	attrs   map[string]any
}

// newWildflyEntitySource derives the entity identity from the probe's endpoint
// URL. The port defaults to 9990 (WildFly management default) when the URL
// carries no explicit port.
func newWildflyEntitySource(endpoint string) *wildflyEntitySource {
	host, port := hostPortFromEndpoint(endpoint)
	return &wildflyEntitySource{
		id: map[string]any{
			"server.address":  host,
			"server.port":     port,
			"app.server.type": "wildfly",
		},
	}
}

// hostPortFromEndpoint extracts host and port (as int64) from an HTTP(S) URL.
// Missing port defaults to 9990 (WildFly management API conventional default).
func hostPortFromEndpoint(rawURL string) (host string, port int64) {
	u, err := url.Parse(rawURL)
	if err != nil || u.Hostname() == "" {
		return rawURL, 9990
	}
	host = u.Hostname()
	p := u.Port()
	if p == "" {
		return host, 9990
	}
	var n int64
	for _, c := range p {
		if c < '0' || c > '9' {
			return host, 9990
		}
		n = n*10 + int64(c-'0')
	}
	return host, n
}

// setReachable is called from Collect to report whether the management API
// responded successfully this cycle. When version is non-empty it is stored as
// a descriptive attribute (updated on every successful cycle).
func (s *wildflyEntitySource) setReachable(up bool, version string) {
	s.mu.Lock()
	s.up = up
	if up && version != "" {
		s.attrs = map[string]any{"version": version}
	}
	s.mu.Unlock()
}

// Observe implements entity.Source. It returns the app.server entity when the
// management endpoint is reachable, or (_, false) on a transient failure so
// the detector keeps the last good snapshot alive.
func (s *wildflyEntitySource) Observe() (entity.Observation, bool) {
	s.mu.RLock()
	up := s.up
	attrs := s.attrs
	s.mu.RUnlock()

	if !up {
		return entity.Observation{}, false
	}
	return entity.Observation{
		Entities: []entity.Entity{{
			Type:       "app.server",
			ID:         s.id,
			Attributes: attrs,
		}},
	}, true
}
