package solr

import (
	"net/url"
	"strconv"
	"sync"

	"senhub-agent.go/internal/agent/services/entity"
)

// solrEntitySource feeds the entity rail with the Solr instance the probe
// monitors. It exposes the instance as a "db" entity (Toise v0.5.0 strict
// contract) so Toise can inventory it automatically.
//
// The entity is only reported when the instance is reachable (up=true). An
// unreachable Solr is not reported as a tombstone — the detector's staleness
// TTL handles expiry when the cached state becomes too old.
type solrEntitySource struct {
	instanceID string
	host       string
	port       int64

	mu    sync.RWMutex
	up    bool
	attrs map[string]any
}

// newSolrEntitySource builds the entity source from the probe's resolved
// endpoint URL. The instance ID is computed once and never changes for the
// lifetime of the probe instance.
func newSolrEntitySource(endpoint string) *solrEntitySource {
	addr, port := hostPort(endpoint)
	return &solrEntitySource{
		instanceID: "solr://" + addr + ":" + strconv.FormatInt(port, 10),
		host:       addr,
		port:       port,
	}
}

// setReachable records the current reachability and, when up, the optional
// version string. Called from Collect on each cycle; safe for concurrent use.
func (s *solrEntitySource) setReachable(up bool, version string) {
	s.mu.Lock()
	s.up = up
	if up {
		attrs := map[string]any{
			"db.system.name": "solr",
			"server.address": s.host,
			"server.port":    s.port,
		}
		if version != "" {
			attrs["db.system.version"] = version
		}
		s.attrs = attrs
	} else {
		s.attrs = nil
	}
	s.mu.Unlock()
}

// Observe implements entity.Source. It returns the current Solr entity when
// the instance was reachable on the last collection cycle, and (obs, false)
// when the instance is unreachable (transient failure — keep the last good
// state rather than deleting the entity).
func (s *solrEntitySource) Observe() (entity.Observation, bool) {
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

// hostPort parses an endpoint URL (e.g. "http://localhost:8983") and returns
// the host and port as scalars suitable for an entity ID. When the URL cannot
// be parsed or has no explicit port, sensible defaults are returned.
func hostPort(endpoint string) (string, int64) {
	u, err := url.Parse(endpoint)
	if err != nil || u.Hostname() == "" {
		return "localhost", 8983
	}
	host := u.Hostname()
	portStr := u.Port()
	if portStr == "" {
		switch u.Scheme {
		case "https":
			return host, 443
		default:
			return host, 8983
		}
	}
	var port int64
	for _, c := range portStr {
		if c < '0' || c > '9' {
			return host, 8983
		}
		port = port*10 + int64(c-'0')
	}
	return host, port
}
