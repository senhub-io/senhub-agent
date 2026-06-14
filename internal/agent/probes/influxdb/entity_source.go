package influxdb

import (
	"net"
	"net/url"
	"strconv"
	"sync"

	"senhub-agent.go/internal/agent/services/entity"
)

// influxdbEntitySource feeds the entity rail with the monitored InfluxDB
// instance as a db.influxdb entity. Observe is non-blocking; the probe
// updates reachability and the version banner on each Collect cycle.
type influxdbEntitySource struct {
	id map[string]any

	mu      sync.RWMutex
	up      bool
	attrs   map[string]any
}

// newInfluxdbEntitySource builds the entity source from the probe's config.
// The ID is immutable: server.address, server.port, and db.system.name are
// extracted from cfg.Endpoint at construction time.
func newInfluxdbEntitySource(cfg probeConfig) *influxdbEntitySource {
	addr, portStr := endpointParts(cfg.Endpoint)
	port, _ := strconv.ParseInt(portStr, 10, 64)

	id := map[string]any{
		"server.address":  addr,
		"server.port":     port,
		"db.system.name": "influxdb",
	}
	return &influxdbEntitySource{id: id}
}

// setReachable updates the liveness state and optional version banner.
// Called from Collect after the /health check.
func (s *influxdbEntitySource) setReachable(up bool, version string) {
	s.mu.Lock()
	s.up = up
	if version != "" {
		s.attrs = map[string]any{"version": version}
	} else if !up {
		s.attrs = nil
	}
	s.mu.Unlock()
}

// Observe implements entity.Source. Returns the db.influxdb entity when the
// instance is reachable. Returning ok=false when down preserves the consumer's
// last good snapshot instead of treating the instance as deleted on a transient
// failure (audit D3).
func (s *influxdbEntitySource) Observe() (entity.Observation, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if !s.up {
		return entity.Observation{}, false
	}
	obs := entity.Observation{
		Entities: []entity.Entity{{
			Type:       "db.influxdb",
			ID:         s.id,
			Attributes: s.attrs,
		}},
	}
	return obs, true
}

// endpointParts extracts the host and port from an InfluxDB endpoint URL.
// Falls back to the host portion of the URL and the default InfluxDB port
// when the URL carries no explicit port.
func endpointParts(endpoint string) (host, port string) {
	u, err := url.Parse(endpoint)
	if err != nil {
		return endpoint, "8086"
	}
	h, p, err := net.SplitHostPort(u.Host)
	if err != nil {
		// No explicit port — use the default InfluxDB port.
		return u.Hostname(), "8086"
	}
	return h, p
}
