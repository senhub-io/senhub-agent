package wildfly

import (
	"net/url"
	"strconv"
	"sync"

	"senhub-agent.go/internal/agent/services/entity"
)

// wildflyEntitySource feeds the entity rail with the WildFly instance this
// probe monitors. It reports a single service.instance entity identified by
// the management endpoint host and port. The entity is emitted only while the
// management API is reachable (up=true); a transient failure returns ok=false
// so the tracker reuses the last good snapshot rather than emitting a delete.
type wildflyEntitySource struct {
	instanceID string
	baseAttrs  map[string]any
	mu         sync.RWMutex
	up         bool
	version    string
}

// newWildflyEntitySource derives the entity identity from the probe's endpoint
// URL. The port defaults to 9990 (WildFly HTTP management default) when the
// URL has no explicit port.
func newWildflyEntitySource(endpoint string) *wildflyEntitySource {
	host, port := hostPortFromEndpoint(endpoint)
	instanceID := "wildfly://" + host + ":" + strconv.FormatInt(port, 10)
	return &wildflyEntitySource{
		instanceID: instanceID,
		baseAttrs: map[string]any{
			"service.name":   "wildfly",
			"server.address": host,
			"server.port":    port,
		},
	}
}

// hostPortFromEndpoint extracts host and port (as int64) from an HTTP(S) URL.
// Missing port defaults to 9990 (WildFly management API conventional default).
func hostPortFromEndpoint(rawURL string) (host string, port int64) {
	u, err := url.Parse(rawURL)
	if err != nil || u.Hostname() == "" {
		return "localhost", 9990
	}
	host = u.Hostname()
	p := u.Port()
	if p == "" {
		return host, 9990
	}
	n, err := strconv.ParseInt(p, 10, 64)
	if err != nil {
		return host, 9990
	}
	return host, n
}

// setReachable is called from Collect to report whether the management API
// responded successfully this cycle. When version is non-empty it is stored as
// the service.version attribute (updated on every successful cycle).
func (s *wildflyEntitySource) setReachable(up bool, version string) {
	s.mu.Lock()
	s.up = up
	if up && version != "" {
		s.version = version
	}
	s.mu.Unlock()
}

// Observe implements entity.Source. It returns the wildfly service.instance
// entity when the endpoint is reachable, or (_, false) on a transient failure
// so the detector keeps the last good snapshot alive.
func (s *wildflyEntitySource) Observe() (entity.Observation, bool) {
	s.mu.RLock()
	up := s.up
	version := s.version
	s.mu.RUnlock()

	if !up {
		return entity.Observation{}, false
	}

	attrs := make(map[string]any, len(s.baseAttrs)+1)
	for k, v := range s.baseAttrs {
		attrs[k] = v
	}
	if version != "" {
		attrs["service.version"] = version
	}

	return entity.Observation{
		Entities: []entity.Entity{{
			Type:       "service.instance",
			ID:         map[string]any{"service.instance.id": s.instanceID},
			Attributes: attrs,
		}},
	}, true
}
