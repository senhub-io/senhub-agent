package mongodb

import (
	"net/url"
	"strconv"
	"sync"

	"senhub-agent.go/internal/agent/services/entity"
)

// mongodbEntitySource feeds the entity rail with the MongoDB instance this
// probe monitors. Observe is non-blocking; setReachable is called from Collect.
type mongodbEntitySource struct {
	instanceID string
	mu         sync.RWMutex
	up         bool
	// attrs holds descriptive attributes; updated on each successful collection.
	attrs map[string]any
}

// newMongodbEntitySource builds the entity source from the probe URI. The
// instance identity is extracted once at construction and never changes for
// the lifetime of the source.
func newMongodbEntitySource(uri string) *mongodbEntitySource {
	addr, port := hostPortFromURI(uri)
	instanceID := "mongodb://" + addr + ":" + strconv.FormatInt(port, 10)
	return &mongodbEntitySource{
		instanceID: instanceID,
		attrs: map[string]any{
			"db.system.name": "mongodb",
			"server.address": addr,
			"server.port":    port,
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
		s.attrs["db.system.version"] = version
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
	return entity.Observation{
		Entities: []entity.Entity{{
			Type:       "db",
			ID:         map[string]any{"db.instance.id": s.instanceID},
			Attributes: s.attrs,
		}},
	}, true
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
