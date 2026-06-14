package pulsar

import (
	"net/url"
	"strconv"
	"sync"

	"senhub-agent.go/internal/agent/services/entity"
)

// pulsarEntitySource feeds the entity rail with the Apache Pulsar broker this
// probe monitors. Observe is non-blocking; setReachable is called from Collect.
//
// Entity model (Toise strict v0.5.0):
//   - service.instance — one per configured broker endpoint
//     ID = {service.instance.id: "pulsar://<host>:<port>"}
type pulsarEntitySource struct {
	instanceID string
	host       string
	port       int64

	mu sync.RWMutex
	up bool
}

// newPulsarEntitySource derives the entity identity from the probe's endpoint
// URL. The port defaults to 8080 (Pulsar Admin REST API) when the URL has no
// explicit port, and 8443 for HTTPS.
func newPulsarEntitySource(endpoint string) *pulsarEntitySource {
	host, port := hostPortFromEndpoint(endpoint)
	instanceID := "pulsar://" + host + ":" + strconv.FormatInt(port, 10)
	return &pulsarEntitySource{
		instanceID: instanceID,
		host:       host,
		port:       port,
	}
}

// hostPortFromEndpoint extracts host and port (as int64) from an HTTP(S) URL.
// Missing port defaults to 8080 (Pulsar Admin REST API) for http and 8443 for https.
func hostPortFromEndpoint(rawURL string) (host string, port int64) {
	u, err := url.Parse(rawURL)
	if err != nil || u.Hostname() == "" {
		return "localhost", 8080
	}
	host = u.Hostname()
	p := u.Port()
	if p == "" {
		if u.Scheme == "https" {
			return host, 8443
		}
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

// Observe implements entity.Source. It returns the Pulsar service.instance
// entity when the broker is reachable, or (_, false) on a transient failure
// so the detector keeps the last good snapshot alive (audit D3).
func (s *pulsarEntitySource) Observe() (entity.Observation, bool) {
	s.mu.RLock()
	up := s.up
	s.mu.RUnlock()

	if !up {
		return entity.Observation{}, false
	}
	return entity.Observation{
		Entities: []entity.Entity{{
			Type: "service.instance",
			ID:   map[string]any{"service.instance.id": s.instanceID},
			Attributes: map[string]any{
				"service.name":   "pulsar",
				"server.address": s.host,
				"server.port":    s.port,
			},
		}},
	}, true
}
