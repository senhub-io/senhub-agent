package couchdb

import (
	"sync"

	"senhub-agent.go/internal/agent/services/entity"
)

// couchdbEntitySource feeds the entity rail with the CouchDB server as a
// db.couchdb entity. Identity is immutable (server.address + server.port +
// db.system.name). Reachability (up/down) is updated by Collect; the entity
// is withheld until the first successful scrape so the topology consumer never
// sees a transient offline node on agent startup.
type couchdbEntitySource struct {
	id map[string]any // immutable after construction

	mu      sync.RWMutex
	up      bool
	version string // descriptive; empty when unknown
}

func newCouchdbEntitySource(addr string, port string) *couchdbEntitySource {
	return &couchdbEntitySource{
		id: map[string]any{
			"server.address":  addr,
			"server.port":     port,
			"db.system.name":  "couchdb",
		},
	}
}

// setReachable is called by Collect after every scrape attempt.
// version is empty on failure.
func (s *couchdbEntitySource) setReachable(up bool, version string) {
	s.mu.Lock()
	s.up = up
	if version != "" {
		s.version = version
	}
	s.mu.Unlock()
}

// Observe implements entity.Source. Returns ok=false until the first
// successful scrape (nothing to report yet is not "server deleted").
func (s *couchdbEntitySource) Observe() (entity.Observation, bool) {
	s.mu.RLock()
	up := s.up
	version := s.version
	s.mu.RUnlock()

	if !up {
		return entity.Observation{}, false
	}

	attrs := map[string]any{}
	if version != "" {
		attrs["version"] = version
	}

	obs := entity.Observation{
		Entities: []entity.Entity{{
			Type:       "db.couchdb",
			ID:         s.id,
			Attributes: attrs,
		}},
	}
	return obs, true
}
