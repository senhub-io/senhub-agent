package nats

import (
	"net/url"
	"sync"

	"senhub-agent.go/internal/agent/services/entity"
)

// natsEntitySource exposes the monitored NATS server as a service.instance
// entity so it appears in the topology graph (Toise / entity rail, #185).
//
// Type: "service.instance"
// ID:   {"service.instance.id": "nats://<host>:<port>"}
//
// The entity is emitted once the first successful /varz response confirms the
// server is reachable (alive=true). Observe returns ok=false before the first
// success so a transient startup gap doesn't delete the entity in consumers.
type natsEntitySource struct {
	id string // "nats://<host>:<port>"

	mu    sync.Mutex
	alive bool
}

func newNATSEntitySource(endpoint string) *natsEntitySource {
	id := buildServiceInstanceID(endpoint)
	return &natsEntitySource{id: id}
}

// markAlive is called by the probe after a successful /varz fetch.
// After the first call, Observe will return ok=true.
func (s *natsEntitySource) markAlive() {
	s.mu.Lock()
	s.alive = true
	s.mu.Unlock()
}

// Observe implements entity.Source. It returns the service.instance entity
// representing the NATS server, or ok=false before the first successful scrape.
func (s *natsEntitySource) Observe() (entity.Observation, bool) {
	s.mu.Lock()
	alive := s.alive
	s.mu.Unlock()

	if !alive {
		return entity.Observation{}, false
	}

	e := entity.Entity{
		Type: "service.instance",
		ID:   map[string]any{"service.instance.id": s.id},
	}
	return entity.Observation{Entities: []entity.Entity{e}}, true
}

// buildServiceInstanceID derives "nats://<host>:<port>" from the configured
// HTTP management endpoint. If parsing fails, the raw endpoint string is used
// as a fallback so the entity ID is never empty.
func buildServiceInstanceID(endpoint string) string {
	u, err := url.Parse(endpoint)
	if err != nil || u.Host == "" {
		return "nats://" + endpoint
	}
	return "nats://" + u.Host
}
