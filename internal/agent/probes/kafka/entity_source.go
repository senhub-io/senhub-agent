package kafka

import (
	"net"
	"sync"

	"senhub-agent.go/internal/agent/services/entity"
)

// kafkaEntitySource feeds the entity rail for a Kafka probe. Observe()
// never blocks: it returns the last cached snapshot set during Collect.
//
// Entity model:
//   - messaging.broker — one per configured bootstrap address; ID = {server.address, server.port, messaging.system}
//   - messaging.topic — one per visible non-internal topic; ID = {server.address, messaging.system, messaging.destination.name}
//   - messaging.consumer_group — one per consumer group; ID = {server.address, messaging.system, messaging.consumer_group.name}
//
// Relations:
//   - messaging.broker  contains  messaging.topic
//   - messaging.consumer_group  subscribes_to  messaging.topic
type kafkaEntitySource struct {
	// immutable identity fields set at construction time
	brokerID map[string]any

	mu      sync.RWMutex
	up      bool
	version string
	topics  []string
	groups  []string
}

// newKafkaEntitySource builds the entity source. addr is the primary bootstrap
// host (host or host:port); port is extracted from addr when present, otherwise
// defaultBrokerPort is used.
func newKafkaEntitySource(addr string) *kafkaEntitySource {
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		// addr has no port — use it as host with the default Kafka port.
		host = addr
		port = "9092"
	}
	if host == "" {
		host = "localhost"
	}
	if port == "" {
		port = "9092"
	}
	return &kafkaEntitySource{
		brokerID: map[string]any{
			"server.address":    host,
			"server.port":       port,
			"messaging.system":  "kafka",
		},
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

// updateSnapshot replaces the topic/group lists discovered during a Collect
// cycle. Called after a successful listing so Observe can build relations.
func (s *kafkaEntitySource) updateSnapshot(topics, groups []string) {
	s.mu.Lock()
	s.topics = topics
	s.groups = groups
	s.mu.Unlock()
}

// Observe returns the full current observation. Returns ok=false when the
// broker was unreachable on the last Collect so the detector keeps the
// previous snapshot rather than emitting deletes on a transient failure.
func (s *kafkaEntitySource) Observe() (entity.Observation, bool) {
	s.mu.RLock()
	up := s.up
	version := s.version
	topics := s.topics
	groups := s.groups
	brokerID := s.brokerID
	s.mu.RUnlock()

	if !up {
		return entity.Observation{}, false
	}

	addr, _ := brokerID["server.address"].(string)
	msgSystem, _ := brokerID["messaging.system"].(string)

	brokerAttrs := map[string]any{}
	if version != "" {
		brokerAttrs["version"] = version
	}

	obs := entity.Observation{
		Entities: []entity.Entity{
			{
				Type:       "messaging.broker",
				ID:         brokerID,
				Attributes: brokerAttrs,
			},
		},
	}

	// Emit topic entities and broker→topic relations.
	for _, topic := range topics {
		topicID := map[string]any{
			"server.address":                 addr,
			"messaging.system":               msgSystem,
			"messaging.destination.name":     topic,
		}
		obs.Entities = append(obs.Entities, entity.Entity{
			Type: "messaging.topic",
			ID:   topicID,
		})
		obs.Relations = append(obs.Relations, entity.Relation{
			Type:     "contains",
			FromType: "messaging.broker",
			FromID:   brokerID,
			ToType:   "messaging.topic",
			ToID:     topicID,
		})
	}

	// Emit consumer group entities and group→topic relations.
	for _, group := range groups {
		groupID := map[string]any{
			"server.address":                  addr,
			"messaging.system":                msgSystem,
			"messaging.consumer_group.name":   group,
		}
		obs.Entities = append(obs.Entities, entity.Entity{
			Type: "messaging.consumer_group",
			ID:   groupID,
		})
		// A consumer group subscribes to all visible topics; the
		// actual per-topic subscription is refined by lag offset data
		// (deferred — today we emit a relation for every topic/group
		// pair that both exist and let Toise filter by lag presence).
		for _, topic := range topics {
			topicID := map[string]any{
				"server.address":             addr,
				"messaging.system":           msgSystem,
				"messaging.destination.name": topic,
			}
			obs.Relations = append(obs.Relations, entity.Relation{
				Type:     "subscribes_to",
				FromType: "messaging.consumer_group",
				FromID:   groupID,
				ToType:   "messaging.topic",
				ToID:     topicID,
			})
		}
	}

	return obs, true
}
