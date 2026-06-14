package clickhouse

import (
	"net/url"
	"strconv"
	"sync"

	"senhub-agent.go/internal/agent/services/entity"
)

// clickhouseEntitySource feeds the entity rail with the ClickHouse instance
// this probe monitors. Observe is non-blocking; setReachable is called from
// Collect.
type clickhouseEntitySource struct {
	instanceID string
	host       string
	port       int64
	mu         sync.RWMutex
	up         bool
	// attrs holds mutable descriptive attributes updated on each successful collection.
	attrs map[string]any
}

// newClickhouseEntitySource builds the entity source from the probe endpoint.
// The instance ID (Toise v0.5.0 contract: db.instance.id = clickhouse://host:port)
// is derived once at construction and never changes.
func newClickhouseEntitySource(endpoint string) *clickhouseEntitySource {
	addr, port := hostPortFromEndpoint(endpoint)
	instanceID := "clickhouse://" + addr + ":" + strconv.FormatInt(port, 10)
	return &clickhouseEntitySource{
		instanceID: instanceID,
		host:       addr,
		port:       port,
	}
}

// setReachable is called by Collect to report the current connectivity state.
// When up is true, descriptive attributes are refreshed (version when non-empty).
// When up is false the entity is suppressed from Observe until the next
// successful collection.
func (s *clickhouseEntitySource) setReachable(up bool, version string) {
	s.mu.Lock()
	s.up = up
	if up {
		attrs := map[string]any{
			"db.system.name": "clickhouse",
			"server.address": s.host,
			"server.port":    s.port,
		}
		if version != "" {
			attrs["db.system.version"] = version
		}
		s.attrs = attrs
	}
	s.mu.Unlock()
}

// Observe implements entity.Source. Returns ok=false when the ClickHouse
// instance is currently unreachable so the detector preserves the last good
// snapshot rather than emitting a delete (transient outage ≠ gone, audit D3).
func (s *clickhouseEntitySource) Observe() (entity.Observation, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if !s.up {
		return entity.Observation{}, false
	}
	return entity.Observation{
		Entities: []entity.Entity{{
			Type:       "db",
			ID:         map[string]any{"db.instance.id": s.instanceID},
			Attributes: s.attrs,
		}},
	}, true
}

// hostPortFromEndpoint extracts the host and port from an HTTP endpoint such
// as "http://localhost:8123". Falls back to "localhost" / 8123 when the
// endpoint cannot be parsed or has no explicit port.
func hostPortFromEndpoint(endpoint string) (host string, port int64) {
	host = "localhost"
	port = 8123

	u, err := url.Parse(endpoint)
	if err != nil || u.Host == "" {
		return
	}
	if h := u.Hostname(); h != "" {
		host = h
	}
	if p := u.Port(); p != "" {
		if n, err := strconv.ParseInt(p, 10, 64); err == nil {
			port = n
		}
	}
	return
}
