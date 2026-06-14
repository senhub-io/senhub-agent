package elasticsearch

import (
	"net/url"
	"strconv"
	"sync"

	"senhub-agent.go/internal/agent/services/entity"
)

// elasticsearchEntitySource feeds the entity rail with the "db" entity
// for the Elasticsearch instance this probe monitors (Toise v0.5.0 strict contract).
// Observe is non-blocking; setReachable and updateSnapshot are called from Collect.
type elasticsearchEntitySource struct {
	instanceID string
	mu         sync.RWMutex
	up         bool
	// attrs holds descriptive attributes; initialised at construction, version added on first successful collect.
	attrs map[string]any
}

// newElasticsearchEntitySource builds the entity source from the probe endpoint URL.
// The instance identity is built once at construction and never changes.
func newElasticsearchEntitySource(endpoint string) *elasticsearchEntitySource {
	addr, port := hostPortFromEndpoint(endpoint)
	instanceID := "elasticsearch://" + addr + ":" + strconv.FormatInt(port, 10)
	return &elasticsearchEntitySource{
		instanceID: instanceID,
		attrs: map[string]any{
			"db.system.name": "elasticsearch",
			"server.address": addr,
			"server.port":    port,
		},
	}
}

// setReachable is called by Collect to report the current connectivity state.
// When up is false the entity is suppressed from Observe until the next
// successful collection (transient outage != gone).
func (s *elasticsearchEntitySource) setReachable(up bool) {
	s.mu.Lock()
	s.up = up
	s.mu.Unlock()
}

// updateSnapshot stores mutable state gathered during a successful collection
// cycle. The clusterName parameter is accepted but ignored — the cluster
// relation requires a registered Toise relation type that is not yet available.
func (s *elasticsearchEntitySource) updateSnapshot(_ string, version string) {
	s.mu.Lock()
	if version != "" {
		attrs := make(map[string]any, len(s.attrs)+1)
		for k, v := range s.attrs {
			attrs[k] = v
		}
		attrs["db.system.version"] = version
		s.attrs = attrs
	}
	s.mu.Unlock()
}

// Observe implements entity.Source. Returns ok=false when the Elasticsearch
// instance is currently unreachable so the detector preserves the last good
// snapshot rather than emitting a delete (transient outage != gone).
func (s *elasticsearchEntitySource) Observe() (entity.Observation, bool) {
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

// hostPortFromEndpoint extracts the host and port from an HTTP endpoint URL such
// as "http://localhost:9200". Falls back to "localhost" / 9200 when the URL
// cannot be parsed or has no explicit port.
func hostPortFromEndpoint(endpoint string) (host string, port int64) {
	host = "localhost"
	port = 9200

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
