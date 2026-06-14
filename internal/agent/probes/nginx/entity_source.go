package nginx

import (
	"net/url"
	"sync"

	"senhub-agent.go/internal/agent/services/entity"
)

// nginxEntitySource feeds the entity rail with the nginx web server this probe
// monitors. It reports a single web.server entity with the endpoint host and
// port as identity. The entity is emitted only while the stub_status page is
// reachable (up=true); a transient fetch failure returns ok=false so the
// tracker reuses the last good snapshot rather than emitting a delete.
type nginxEntitySource struct {
	id   map[string]any
	mu   sync.RWMutex
	up   bool
	port any // int64 or string
}

// newNginxEntitySource derives the entity identity from the probe's endpoint
// URL. The port defaults to 80 (HTTP) or 443 (HTTPS) when the URL has no
// explicit port, matching standard nginx conventions.
func newNginxEntitySource(endpoint string) *nginxEntitySource {
	host, port := hostPortFromEndpoint(endpoint)
	return &nginxEntitySource{
		id: map[string]any{
			"server.address":  host,
			"server.port":     port,
			"web.server.type": "nginx",
		},
	}
}

// hostPortFromEndpoint extracts host and port (as int64) from an HTTP(S) URL.
// Missing port defaults to 80 for http and 443 for https.
func hostPortFromEndpoint(rawURL string) (host string, port int64) {
	u, err := url.Parse(rawURL)
	if err != nil || u.Hostname() == "" {
		return rawURL, 80
	}
	host = u.Hostname()
	p := u.Port()
	if p == "" {
		if u.Scheme == "https" {
			return host, 443
		}
		return host, 80
	}
	var n int64
	for _, c := range p {
		if c < '0' || c > '9' {
			return host, 80
		}
		n = n*10 + int64(c-'0')
	}
	return host, n
}

// setReachable is called from Collect to report whether the stub_status page
// responded successfully this cycle.
func (s *nginxEntitySource) setReachable(up bool) {
	s.mu.Lock()
	s.up = up
	s.mu.Unlock()
}

// Observe implements entity.Source. It returns the nginx web.server entity
// when the endpoint is reachable, or (_, false) on a transient failure so
// the detector keeps the last good snapshot alive.
func (s *nginxEntitySource) Observe() (entity.Observation, bool) {
	s.mu.RLock()
	up := s.up
	s.mu.RUnlock()

	if !up {
		return entity.Observation{}, false
	}
	return entity.Observation{
		Entities: []entity.Entity{{
			Type: "web.server",
			ID:   s.id,
		}},
	}, true
}
