package kafka

import (
	"net"
	"strconv"
	"sync"

	"senhub-agent.go/internal/agent/services/entity"
)

// kafkaEntitySource feeds the entity rail for a Kafka probe. Observe()
// never blocks: it returns the last cached snapshot set during Collect.
//
// Entity model (Toise strict v0.5.0):
//   - service.instance — one per configured bootstrap address
//     ID = {service.instance.id: "kafka://<host>:<port>"}
type kafkaEntitySource struct {
	// immutable identity fields set at construction time
	instanceID string
	host       string
	port       int64

	mu      sync.RWMutex
	up      bool
	version string
}

// newKafkaEntitySource builds the entity source. addr is the primary bootstrap
// host (host or host:port); port is extracted from addr when present, otherwise
// defaultBrokerPort is used.
func newKafkaEntitySource(addr string) *kafkaEntitySource {
	host, portStr, err := net.SplitHostPort(addr)
	if err != nil {
		host = addr
		portStr = "9092"
	}
	if host == "" {
		host = "localhost"
	}
	if portStr == "" {
		portStr = "9092"
	}
	port, _ := strconv.ParseInt(portStr, 10, 64)
	if port == 0 {
		port = 9092
	}
	return &kafkaEntitySource{
		instanceID: "kafka://" + host + ":" + strconv.FormatInt(port, 10),
		host:       host,
		port:       port,
	}
}

// setReachable updates the liveness flag and optional version string.
// Called from Collect on success or failure.
func (s *kafkaEntitySource) setReachable(up bool, version string) {
	s.mu.Lock()
	s.up = up
	if version != "" {
		s.version = version
	}
	s.mu.Unlock()
}

// Observe returns the current observation. Returns ok=false when the
// broker was unreachable on the last Collect so the detector keeps the
// previous snapshot rather than emitting deletes on a transient failure.
func (s *kafkaEntitySource) Observe() (entity.Observation, bool) {
	s.mu.RLock()
	up := s.up
	version := s.version
	instanceID := s.instanceID
	host := s.host
	port := s.port
	s.mu.RUnlock()

	if !up {
		return entity.Observation{}, false
	}

	attrs := map[string]any{
		"service.name":   "kafka",
		"server.address": host,
		"server.port":    port,
	}
	if version != "" {
		attrs["service.version"] = version
	}

	return entity.Observation{
		Entities: []entity.Entity{{
			Type:       "service.instance",
			ID:         map[string]any{"service.instance.id": instanceID},
			Attributes: attrs,
		}},
	}, true
}
