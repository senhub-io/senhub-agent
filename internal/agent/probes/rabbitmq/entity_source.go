package rabbitmq

import (
	"sync"

	"senhub-agent.go/internal/agent/services/agentstate"
	"senhub-agent.go/internal/agent/services/entity"
)

// rabbitmqEntitySource feeds the entity rail for the rabbitmq probe.
// It reports the broker as a service.instance entity (Toise D1 option A)
// with a stable, non-network-derived id.
//
// Identity resolution precedence (pinned once, never changed):
//  1. operator config key "instance_name" — verbatim, pinned at construction.
//  2. tech-reported node name from GET /api/overview ("node" field, e.g.
//     "rabbit@myhost") — pinned on first successful collect, formatted as
//     "rabbitmq:<node-name>".
//  3. host-id fallback "rabbitmq@<host.id>" when tech id is permanently
//     unavailable — pinned when the caller explicitly calls pinFallback.
//  4. last-resort "rabbitmq" — if even the host id is empty.
//
// The entity is NOT emitted until the id is pinned (ok=false), so the
// consumer never sees a transient fallback id that a later tech-id fetch
// would re-key.
//
// The source is declared via SetEntitySource in the constructor; the
// ProbePoller registers it on Start and unregisters it on Shutdown.
// Observe is non-blocking and returns the last cached snapshot.
type rabbitmqEntitySource struct {
	// serverAddr and serverPort are the descriptive attributes from the config.
	// They are stable (set at construction) and go into entity Attributes, not the ID.
	serverAddr string
	serverPort int64

	// hostIDFn returns the fallback host identity (injected for testability).
	hostIDFn func() string

	mu sync.RWMutex
	// pinnedID is non-empty once the identity has been resolved. Once set it
	// never changes for the lifetime of the source (immutability contract).
	pinnedID string
	// up tracks whether the broker was reachable on the last collect.
	up bool
	// attrs holds the descriptive attribute set including any service.version
	// added on first successful contact.
	attrs map[string]any
}

// newRabbitmqEntitySource constructs the entity source.
//
//   - instanceName: optional operator override (config key "instance_name").
//     When non-empty the id is pinned immediately at construction time.
//   - addr, port: management API host/port, kept as descriptive attributes.
//   - hostIDFn: returns the fallback host-id string ("rabbitmq@<id>" or
//     "rabbitmq"); injected so tests need no real OS call.
func newRabbitmqEntitySource(instanceName, addr string, port int64, hostIDFn func() string) *rabbitmqEntitySource {
	s := &rabbitmqEntitySource{
		serverAddr: addr,
		serverPort: port,
		hostIDFn:   hostIDFn,
		attrs: map[string]any{
			"service.name":   "rabbitmq",
			"server.address": addr,
			"server.port":    port,
		},
	}
	if instanceName != "" {
		s.pinnedID = instanceName
	}
	return s
}

// tryPinTechID pins the id from the RabbitMQ node name (e.g. "rabbit@myhost")
// as "rabbitmq:<node-name>" if no id has been pinned yet.
// Called from Collect on the first successful API response.
func (s *rabbitmqEntitySource) tryPinTechID(nodeName string) {
	if nodeName == "" {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.pinnedID == "" {
		s.pinnedID = "rabbitmq:" + nodeName
	}
}

// pinFallback pins the host-derived fallback id when the tech id has proven
// permanently unavailable. Once called (by the probe when it decides to stop
// waiting), the entity starts being emitted with the fallback id.
func (s *rabbitmqEntitySource) pinFallback() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.pinnedID != "" {
		return
	}
	id := "rabbitmq"
	if hid := s.hostIDFn(); hid != "" {
		id = "rabbitmq@" + hid
	}
	s.pinnedID = id
}

// setReachable marks the broker as reachable or not for the next Observe call.
// An optional version string enriches the service.version attribute when the
// broker is reachable (only applied on the first call with a non-empty version).
func (s *rabbitmqEntitySource) setReachable(up bool, version string) {
	s.mu.Lock()
	s.up = up
	if up && version != "" {
		if _, hasVer := s.attrs["service.version"]; !hasVer {
			attrs := make(map[string]any, len(s.attrs)+1)
			for k, v := range s.attrs {
				attrs[k] = v
			}
			attrs["service.version"] = version
			s.attrs = attrs
		}
	}
	s.mu.Unlock()
}

// pinnedInstanceID returns the current pinned id (empty while not yet pinned).
func (s *rabbitmqEntitySource) pinnedInstanceID() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.pinnedID
}

// Observe implements entity.Source. It returns the rabbitmq service.instance
// entity plus the monitors edge when both the id is pinned and the broker is
// reachable. Returns (_, false) when the id is not yet pinned (so no transient
// id leaks to the consumer) or on a transient unreachability (the detector then
// keeps the last good snapshot alive).
func (s *rabbitmqEntitySource) Observe() (entity.Observation, bool) {
	s.mu.RLock()
	pinnedID := s.pinnedID
	up := s.up
	attrs := s.attrs
	s.mu.RUnlock()

	if pinnedID == "" || !up {
		return entity.Observation{}, false
	}

	targetID := map[string]any{"service.instance.id": pinnedID}
	obs := entity.Observation{
		Entities: []entity.Entity{{
			Type:       "service.instance",
			ID:         targetID,
			Attributes: attrs,
		}},
	}

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

	// runs_on edge: anchor a locally-monitored broker to the agent host so it
	// does not float with only its monitors edge. The helper's collapse guard
	// suppresses the edge for a remote target or a loopback-derived identity.
	if rel, ok := entity.LocalRunsOn("service.instance", targetID, s.serverAddr, s.hostIDFn()); ok {
		obs.Relations = append(obs.Relations, rel)
	}

	return obs, true
}
