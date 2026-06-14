package mongodb

import (
	"net/url"
	"strconv"
	"sync"

	"senhub-agent.go/internal/agent/services/entity"
)

// mongodbEntitySource feeds the entity rail with the db.mongodb instance this
// probe monitors. Observe is non-blocking; setReachable is called from Collect.
type mongodbEntitySource struct {
	id  map[string]any
	mu  sync.RWMutex
	up  bool
	// attrs holds mutable descriptive attributes (e.g. server version).
	attrs map[string]any
}

// newMongodbEntitySource builds the entity source from the probe URI. The
// identity (server.address, server.port, db.system.name) is extracted once at
// construction and never changes for the lifetime of the source.
func newMongodbEntitySource(uri string) *mongodbEntitySource {
	addr, port := hostPortFromURI(uri)
	return &mongodbEntitySource{
		id: map[string]any{
			"server.address":  addr,
			"server.port":     port,
			"db.system.name":  "mongodb",
		},
	}
}

// setReachable is called by Collect to report the current connectivity state.
// When up is true, version (if non-empty) is stored as a descriptive attribute.
// When up is false the entity is suppressed from Observe until the next
// successful collection.
func (s *mongodbEntitySource) setReachable(up bool, version string) {
	s.mu.Lock()
	s.up = up
	if up && version != "" {
		s.attrs = map[string]any{"version": version}
	}
	s.mu.Unlock()
}

// Observe implements entity.Source. Returns ok=false when the MongoDB instance
// is currently unreachable so the detector preserves the last good snapshot
// rather than emitting a delete (transient outage ≠ gone, audit D3).
func (s *mongodbEntitySource) Observe() (entity.Observation, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if !s.up {
		return entity.Observation{}, false
	}
	obs := entity.Observation{
		Entities: []entity.Entity{{
			Type:       "db.mongodb",
			ID:         s.id,
			Attributes: s.attrs,
		}},
	}
	return obs, true
}

// hostPortFromURI extracts the host and port from a MongoDB URI such as
// "mongodb://user:pass@host:27017/dbname". Falls back to "localhost" / 27017
// when the URI cannot be parsed or has no explicit port.
func hostPortFromURI(uri string) (host string, port int64) {
	host = "localhost"
	port = 27017

	u, err := url.Parse(uri)
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
