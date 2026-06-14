package elasticsearch

import (
	"net/url"
	"strconv"
	"sync"

	"senhub-agent.go/internal/agent/services/entity"
)

// elasticsearchEntitySource feeds the entity rail with the search.engine instance
// this probe monitors, plus a relation to its cluster when the cluster name is known.
// Observe is non-blocking; setReachable and updateSnapshot are called from Collect.
type elasticsearchEntitySource struct {
	id  map[string]any
	mu  sync.RWMutex
	up  bool
	// attrs holds mutable descriptive attributes (e.g. version).
	attrs       map[string]any
	clusterName string
}

// newElasticsearchEntitySource builds the entity source from the probe endpoint URL.
// The identity (server.address, server.port, search.engine.type) is extracted once
// at construction and never changes for the lifetime of the source.
func newElasticsearchEntitySource(endpoint string) *elasticsearchEntitySource {
	addr, port := hostPortFromEndpoint(endpoint)
	return &elasticsearchEntitySource{
		id: map[string]any{
			"server.address":     addr,
			"server.port":        port,
			"search.engine.type": "elasticsearch",
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

// updateSnapshot stores the mutable state gathered during a successful collection
// cycle: cluster name (for the topology relation) and node version.
func (s *elasticsearchEntitySource) updateSnapshot(clusterName, version string) {
	s.mu.Lock()
	s.clusterName = clusterName
	if version != "" {
		s.attrs = map[string]any{"version": version}
	}
	s.mu.Unlock()
}

// Observe implements entity.Source. Returns ok=false when the Elasticsearch
// instance is currently unreachable so the detector preserves the last good
// snapshot rather than emitting a delete (transient outage != gone, audit D3).
func (s *elasticsearchEntitySource) Observe() (entity.Observation, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if !s.up {
		return entity.Observation{}, false
	}
	obs := entity.Observation{
		Entities: []entity.Entity{{
			Type:       "search.engine",
			ID:         s.id,
			Attributes: s.attrs,
		}},
	}
	if s.clusterName != "" {
		clusterID := map[string]any{
			"search.cluster.name": s.clusterName,
			"search.engine.type":  "elasticsearch",
		}
		obs.Entities = append(obs.Entities, entity.Entity{
			Type: "search.cluster",
			ID:   clusterID,
		})
		obs.Relations = append(obs.Relations, entity.Relation{
			Type:     "has_node",
			FromType: "search.cluster",
			FromID:   clusterID,
			ToType:   "search.engine",
			ToID:     s.id,
		})
	}
	return obs, true
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
