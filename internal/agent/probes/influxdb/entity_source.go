package influxdb

import (
	"net"
	"net/url"
	"strconv"
	"sync"

	"senhub-agent.go/internal/agent/services/entity"
)

// influxdbEntitySource feeds the entity rail with the monitored InfluxDB
// instance as a "db" entity (Toise contract v0.5.0). Observe is non-blocking;
// the probe updates reachability and version on each Collect cycle.
type influxdbEntitySource struct {
	instanceID string
	host       string
	port       int64

	mu    sync.RWMutex
	up    bool
	attrs map[string]any
}

// newInfluxdbEntitySource builds the entity source from the probe's config.
// The instance ID is immutable and follows the db.instance.id convention:
// "influxdb://<host>:<port>".
func newInfluxdbEntitySource(cfg probeConfig) *influxdbEntitySource {
	addr, portStr := endpointParts(cfg.Endpoint)
	port, _ := strconv.ParseInt(portStr, 10, 64)
	instanceID := "influxdb://" + addr + ":" + strconv.FormatInt(port, 10)

	return &influxdbEntitySource{
		instanceID: instanceID,
		host:       addr,
		port:       port,
	}
}

// setReachable updates the liveness state and version banner.
// Called from Collect after the /health check.
func (s *influxdbEntitySource) setReachable(up bool, version string) {
	s.mu.Lock()
	s.up = up
	if up {
		s.attrs = map[string]any{
			"db.system.name":    "influxdb",
			"server.address":    s.host,
			"server.port":       s.port,
			"db.system.version": version,
		}
	} else {
		s.attrs = nil
	}
	s.mu.Unlock()
}

// Observe implements entity.Source. Returns the db entity when the instance is
// reachable. Returning ok=false when down preserves the consumer's last good
// snapshot instead of treating the instance as deleted on a transient failure.
func (s *influxdbEntitySource) Observe() (entity.Observation, bool) {
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
