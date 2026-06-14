package couchdb

import (
	"net/url"
	"strconv"
	"sync"

	"senhub-agent.go/internal/agent/services/entity"
)

// couchdbEntitySource feeds the entity rail with the "db" entity
// for the CouchDB instance this probe monitors (Toise v0.5.0 strict contract).
// Observe is non-blocking; setReachable and updateVersion are called from Collect.
type couchdbEntitySource struct {
	instanceID string
	mu         sync.RWMutex
	up         bool
	// attrs holds descriptive attributes; version added on first successful collect.
	attrs map[string]any
}

// newCouchDBEntitySource builds the entity source from the probe endpoint URL.
// The instance identity is built once at construction and never changes.
func newCouchDBEntitySource(endpoint string) *couchdbEntitySource {
	addr, port := couchdbHostPortFromEndpoint(endpoint)
	instanceID := "couchdb://" + addr + ":" + strconv.FormatInt(port, 10)
	return &couchdbEntitySource{
		instanceID: instanceID,
		attrs: map[string]any{
			"db.system.name": "couchdb",
			"server.address": addr,
			"server.port":    port,
		},
	}
}

// setReachable is called by Collect to report the current connectivity state.
// When up is false the entity is suppressed from Observe until the next
// successful collection (transient outage != gone).
func (s *couchdbEntitySource) setReachable(up bool) {
	s.mu.Lock()
	s.up = up
	s.mu.Unlock()
}

// updateVersion stores the CouchDB server version gathered during a successful
// collection cycle.
func (s *couchdbEntitySource) updateVersion(version string) {
	if version == "" {
		return
	}
	s.mu.Lock()
	attrs := make(map[string]any, len(s.attrs)+1)
	for k, v := range s.attrs {
		attrs[k] = v
	}
	attrs["db.system.version"] = version
	s.attrs = attrs
	s.mu.Unlock()
}

// Observe implements entity.Source. Returns ok=false when the CouchDB instance
// is currently unreachable so the detector preserves the last good snapshot
// rather than emitting a delete (transient outage != gone).
func (s *couchdbEntitySource) Observe() (entity.Observation, bool) {
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

// couchdbHostPortFromEndpoint extracts the host and port from an HTTP endpoint
// URL such as "http://localhost:5984". Falls back to "localhost" / 5984 when
// the URL cannot be parsed or has no explicit port.
func couchdbHostPortFromEndpoint(endpoint string) (host string, port int64) {
	host = "localhost"
	port = 5984

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
	} else {
		switch u.Scheme {
		case "https":
			port = 443
		default:
			port = 5984
		}
	}
	return
}
