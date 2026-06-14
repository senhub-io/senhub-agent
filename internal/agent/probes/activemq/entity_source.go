package activemq

import (
	"strconv"
	"sync"

	"senhub-agent.go/internal/agent/services/entity"
)

// activemqEntitySource implements entity.Source for the ActiveMQ probe.
// Reports the broker as a service.instance entity (Toise strict v0.5.0).
//
// Toise contract:
//
//	type:   service.instance
//	id key: service.instance.id = "activemq://<host>:<port>"
type activemqEntitySource struct {
	// Immutable identity built once in the constructor.
	instanceID string

	mu    sync.RWMutex
	up    bool
	attrs map[string]any

	// destinations is kept for probe compatibility (updateSnapshot is called
	// by the probe after every successful Collect) but is not emitted as
	// entities — non-standard relation types (contains) are not registered
	// in Toise v0.5.0.
	destinations []destinationSnapshot
}

type destinationSnapshot struct {
	name     string
	destType string // "queue" or "topic"
}

func newActivemqEntitySource(addr string, port int) *activemqEntitySource {
	return &activemqEntitySource{
		instanceID: "activemq://" + addr + ":" + strconv.FormatInt(int64(port), 10),
	}
}

// setReachable updates liveness and base attributes.
// Call after every Collect: setReachable(true, version) on success,
// setReachable(false, "") on fatal error.
func (s *activemqEntitySource) setReachable(up bool, version string) {
	s.mu.Lock()
	s.up = up
	if up {
		host, port := splitInstanceID(s.instanceID)
		attrs := map[string]any{
			"service.name":   "activemq",
			"server.address": host,
			"server.port":    port,
		}
		if version != "" {
			attrs["service.version"] = version
		}
		s.attrs = attrs
	}
	s.mu.Unlock()
}

// updateSnapshot stores the destination list (retained for probe compat;
// destinations are not exposed as entities in the current Toise contract).
func (s *activemqEntitySource) updateSnapshot(dests []destinationSnapshot) {
	s.mu.Lock()
	s.destinations = dests
	s.mu.Unlock()
}

// Observe implements entity.Source. Non-blocking; returns the last cached
// snapshot. Returns ok=false when the broker is unreachable.
func (s *activemqEntitySource) Observe() (entity.Observation, bool) {
	s.mu.RLock()
	up, attrs := s.up, s.attrs
	s.mu.RUnlock()

	if !up {
		return entity.Observation{}, false
	}

	return entity.Observation{
		Entities: []entity.Entity{{
			Type:       "service.instance",
			ID:         map[string]any{"service.instance.id": s.instanceID},
			Attributes: attrs,
		}},
	}, true
}

// splitInstanceID extracts host and port from "activemq://host:port".
// Returns the raw instanceID and int64(0) if parsing fails.
func splitInstanceID(id string) (host string, port int64) {
	// Format: "activemq://host:port"
	const prefix = "activemq://"
	if len(id) <= len(prefix) {
		return id, 0
	}
	hostPort := id[len(prefix):]
	for i := len(hostPort) - 1; i >= 0; i-- {
		if hostPort[i] == ':' {
			p, err := strconv.ParseInt(hostPort[i+1:], 10, 64)
			if err != nil {
				return hostPort, 0
			}
			return hostPort[:i], p
		}
	}
	return hostPort, 0
}
