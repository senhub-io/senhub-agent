package nginx

import (
	"net/url"
	"strconv"
	"sync"

	"senhub-agent.go/internal/agent/services/entity"
)

// nginxEntitySource feeds the entity rail with the nginx instance this probe
// monitors. It reports a single service.instance entity with the endpoint host
// and port as identity. The entity is emitted only while the stub_status page
// is reachable (up=true); a transient fetch failure returns ok=false so the
// tracker reuses the last good snapshot rather than emitting a delete.
type nginxEntitySource struct {
	instanceID string
	attrs      map[string]any
	mu         sync.RWMutex
	up         bool
}

// newNginxEntitySource derives the entity identity from the probe's endpoint
// URL. The port defaults to 80 (HTTP) or 443 (HTTPS) when the URL has no
// explicit port, matching standard nginx conventions.
func newNginxEntitySource(endpoint string) *nginxEntitySource {
	host, port := hostPortFromEndpoint(endpoint)
	instanceID := "nginx://" + host + ":" + strconv.FormatInt(port, 10)
	return &nginxEntitySource{
		instanceID: instanceID,
		attrs: map[string]any{
			"service.name":   "nginx",
			"server.address": host,
			"server.port":    port,
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

// Observe implements entity.Source. It returns the nginx service.instance
// entity when the endpoint is reachable, or (_, false) on a transient failure
// so the detector keeps the last good snapshot alive.
func (s *nginxEntitySource) Observe() (entity.Observation, bool) {
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
