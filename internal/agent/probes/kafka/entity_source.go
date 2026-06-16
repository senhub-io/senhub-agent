package kafka

import (
	"net"
	"strconv"
	"sync"

	"senhub-agent.go/internal/agent/services/agentstate"
	"senhub-agent.go/internal/agent/services/common"
	"senhub-agent.go/internal/agent/services/entity"
)

// techIDFetcher is a function the probe injects to retrieve the Kafka
// cluster id from the live cluster (wraps a sarama client call). It is
// called once on the first successful collect and its result is pinned
// as the entity identity. Tests inject a stub so no live broker is needed.
type techIDFetcher func() (string, error)

// hostIDProvider returns the host identity ID used as the precedence-2
// fallback when the tech id is genuinely unavailable. Injectable for
// testing — production code leaves it nil and the real gopsutil path runs.
type hostIDProvider func() string

// kafkaEntitySource feeds the entity rail for a Kafka probe. Observe()
// never blocks: it returns the last cached snapshot.
//
// Entity model (Toise D1, precedence-1 path):
//
//	service.instance — one Kafka cluster per probe instance.
//	Identity is a STABLE tech-reported id, resolved on the first
//	successful collect and pinned for the process lifetime. The entity
//	is NOT emitted before the id is pinned (ok=false).
//
// ID resolution order:
//  1. operator "instance_name" config key (verbatim, pinned at construction)
//  2. Kafka cluster id from the broker "kafka:<cluster-id>" (pinned on
//     first successful fetch, lazy)
//  3. "kafka@<host.id>" (fallback when cluster id permanently unreachable)
//  4. "kafka" (last resort when even the host id is unavailable)
type kafkaEntitySource struct {
	// static (immutable after construction)
	host string
	port int64

	// techFetch fetches the cluster id from the live Kafka cluster. Set to
	// nil once the id is pinned.
	techFetch  techIDFetcher
	hostIDFunc hostIDProvider

	mu sync.RWMutex
	// pinnedID is the resolved service.instance.id once established; ""
	// means "not yet pinned — withhold entity emission".
	pinnedID string
	// degraded is true once we have given up on the tech id and pinned a
	// fallback — no further fetch attempts are made.
	degraded bool

	up      bool
	version string
}

// newKafkaEntitySource builds the entity source. addr is the primary bootstrap
// host (host or host:port). instanceName is the optional operator override from
// config — when non-empty it is pinned immediately and no tech-id fetch is needed.
func newKafkaEntitySource(addr, instanceName string, fetch techIDFetcher, hostID hostIDProvider) *kafkaEntitySource {
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

	s := &kafkaEntitySource{
		host:       host,
		port:       port,
		techFetch:  fetch,
		hostIDFunc: hostID,
	}
	// Precedence 1: operator config overrides everything.
	if instanceName != "" {
		s.pinnedID = instanceName
		s.techFetch = nil // no need to fetch
	}
	return s
}

// notifySuccess is called from Collect when the broker is reachable. It
// attempts to resolve the cluster id on the first call after the entity
// source is created (tech id path). version may be "" when unknown.
func (s *kafkaEntitySource) notifySuccess(version string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.up = true
	if version != "" {
		s.version = version
	}

	if s.pinnedID != "" {
		// Already pinned (operator config or previous successful fetch).
		return
	}

	// Attempt to pin the tech id on the first successful collect.
	if s.techFetch != nil && !s.degraded {
		if clusterID, err := s.techFetch(); err == nil && clusterID != "" {
			s.pinnedID = "kafka:" + clusterID
			s.techFetch = nil // fetch done; release the closure
			return
		}
		// Tech id unavailable this cycle — defer, do not degrade yet.
		// The entity is withheld (pinnedID=="") until we succeed or
		// explicitly degrade (notifyFailure after repeated unreachability).
	}
}

// notifyFailure is called from Collect when the broker is unreachable.
// After the first unreachable cycle with no pinned id we degrade to the
// host-id fallback so the entity can be emitted rather than withheld
// indefinitely.
func (s *kafkaEntitySource) notifyFailure() {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.up = false

	if s.pinnedID != "" || s.degraded {
		return
	}

	// First collect already failed — tech id is unreachable.
	// Degrade to precedence-2 (host.id) or precedence-3 ("kafka").
	s.degraded = true
	s.techFetch = nil

	hostID := ""
	if s.hostIDFunc != nil {
		hostID = s.hostIDFunc()
	} else {
		if hi, err := common.GetHostIdentity(); err == nil {
			hostID = hi.ID
		}
	}
	if hostID != "" {
		s.pinnedID = "kafka@" + hostID
	} else {
		s.pinnedID = "kafka"
	}
}

// Observe returns the current observation. Returns ok=false until the entity
// id is pinned (so no entity with a transient or network-derived id is ever
// emitted). Once the broker is known-down, the entity is still emitted using
// the pinned id but without the monitors edge (up stays the value from the
// last collect).
func (s *kafkaEntitySource) Observe() (entity.Observation, bool) {
	s.mu.RLock()
	pinnedID := s.pinnedID
	up := s.up
	version := s.version
	host := s.host
	port := s.port
	s.mu.RUnlock()

	if pinnedID == "" {
		// Id not yet resolved; withhold emission.
		return entity.Observation{}, false
	}

	if !up {
		// Broker unreachable: keep the last known entity alive in the
		// consumer but do not emit a monitors edge (down probe is not
		// monitoring anything actionable).
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

	targetID := map[string]any{"service.instance.id": pinnedID}
	obs := entity.Observation{
		Entities: []entity.Entity{{
			Type:       "service.instance",
			ID:         targetID,
			Attributes: attrs,
		}},
	}

	// monitors edge: agent → kafka service.instance
	agentID := agentstate.GetAgentInstanceID()
	if agentID != "" {
		obs.Relations = append(obs.Relations, entity.Relation{
			Type:     "monitors",
			FromType: "service.instance",
			FromID:   map[string]any{"service.instance.id": agentID},
			ToType:   "service.instance",
			ToID:     targetID,
		})
	}

	return obs, true
}
