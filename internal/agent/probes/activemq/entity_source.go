package activemq

import (
	"sync"

	"senhub-agent.go/internal/agent/services/agentstate"
	"senhub-agent.go/internal/agent/services/entity"
)

// activemqEntitySource implements entity.Source for the ActiveMQ probe.
//
// Model: Toise D1 option A — the broker is a service.instance with a stable,
// non-network-derived id. The agent emits a "monitors" edge from itself.
//
// ID precedence (pinned once, never changed for the process lifetime):
//
//  1. operator config key "instance_name" — verbatim, pinned at construction.
//  2. tech-reported "activemq:<BrokerId>" fetched from Jolokia on the first
//     successful collect. The entity is NOT emitted before this is pinned.
//  3. fallback "activemq@<host.id>" when the tech id is genuinely unavailable.
//  4. last resort "activemq" when even the host id is unresolvable.
//
// A changing id would re-key the entity in Toise, so the id is latched the
// first time it resolves and never updated.
type activemqEntitySource struct {
	// serverAddr / serverPort are the descriptive network coordinates — NOT
	// part of the identity.
	serverAddr string
	serverPort int

	// hostIDFn injects the host id (useful in tests; production code sets this
	// to a real lookup at construction). It is called at most once (when pinning
	// the fallback id) and its result is never stored; the pinned id holds it.
	hostIDFn func() string

	// hostID is the agent host id, resolved once at construction; it is the
	// target of the local-target runs_on edge.
	hostID string

	mu sync.Mutex

	// pinnedID is the id once resolved; "" means not yet resolved.
	pinnedID string
	// idResolved is true once pinnedID is finalized (either from tech id or
	// from the fallback path).
	idResolved bool

	// up tracks broker reachability.
	up bool
	// attrs is the last descriptive attribute snapshot.
	attrs map[string]any

	// destinations is kept for probe compatibility.
	destinations []destinationSnapshot
}

type destinationSnapshot struct {
	name     string
	destType string // "queue" or "topic"
}

// newActivemqEntitySource constructs the entity source.
//
// When instanceName is non-empty (operator config "instance_name") the id is
// pinned immediately at construction and the tech-id fetch is skipped.
// addr and port are the Jolokia target coordinates kept as descriptive attrs.
// hostIDFn must return the host's stable OS identity (or "" on failure); it is
// only called if the tech id is unavailable.
func newActivemqEntitySource(instanceName, addr string, port int, hostIDFn func() string) *activemqEntitySource {
	s := &activemqEntitySource{
		serverAddr: addr,
		serverPort: port,
		hostIDFn:   hostIDFn,
	}
	if hostIDFn != nil {
		s.hostID = hostIDFn()
	}
	if instanceName != "" {
		s.pinnedID = instanceName
		s.idResolved = true
	}
	return s
}

// pinTechID is called by the probe after the first successful Jolokia collect
// with the broker's BrokerId. It pins "activemq:<brokerID>" as the entity id.
// No-op if the id is already resolved (either from instance_name or a prior
// successful call).
func (s *activemqEntitySource) pinTechID(brokerID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.idResolved {
		return
	}
	if brokerID != "" {
		s.pinnedID = "activemq:" + brokerID
		s.idResolved = true
	}
}

// pinFallback is called when the tech id is definitively unavailable (the
// target has been unreachable and the probe decides to degrade). It pins
// "activemq@<host.id>" or "activemq" as the last-resort id.
// No-op if the id is already resolved.
func (s *activemqEntitySource) pinFallback() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.idResolved {
		return
	}
	id := "activemq"
	if s.hostIDFn != nil {
		if hid := s.hostIDFn(); hid != "" {
			id = "activemq@" + hid
		}
	}
	s.pinnedID = id
	s.idResolved = true
}

// setReachable updates liveness and the descriptive attribute snapshot. Called
// by the probe after every Collect: setReachable(true, version) on success,
// setReachable(false, "") on error.
func (s *activemqEntitySource) setReachable(up bool, version string) {
	s.mu.Lock()
	s.up = up
	if up {
		attrs := map[string]any{
			"service.name":   "activemq",
			"server.address": s.serverAddr,
			"server.port":    int64(s.serverPort),
		}
		if version != "" {
			attrs["service.version"] = version
		}
		s.attrs = attrs
	}
	s.mu.Unlock()
}

// updateSnapshot stores the destination list.
func (s *activemqEntitySource) updateSnapshot(dests []destinationSnapshot) {
	s.mu.Lock()
	s.destinations = dests
	s.mu.Unlock()
}

// Observe implements entity.Source. Non-blocking; returns the last cached
// snapshot. Returns ok=false when:
//   - the entity id is not yet resolved (no entity ever emitted until pinned), or
//   - the broker is unreachable.
func (s *activemqEntitySource) Observe() (entity.Observation, bool) {
	s.mu.Lock()
	resolved, pinnedID, up, attrs := s.idResolved, s.pinnedID, s.up, s.attrs
	s.mu.Unlock()

	if !resolved || !up {
		return entity.Observation{}, false
	}

	instanceID := map[string]any{"service.instance.id": pinnedID}

	obs := entity.Observation{
		Entities: []entity.Entity{{
			Type:       "service.instance",
			ID:         instanceID,
			Attributes: attrs,
		}},
	}

	if agentID := agentstate.GetAgentInstanceID(); agentID != "" {
		obs.Relations = append(obs.Relations, entity.Relation{
			Type:     "monitors",
			FromType: "service.instance",
			FromID:   map[string]any{"service.instance.id": agentID},
			ToType:   "service.instance",
			ToID:     instanceID,
		})
	}

	// runs_on edge: activemq → host when the monitored broker is local (loopback),
	// so a locally-monitored broker hangs off the host it runs on instead of
	// floating with only its monitors anchor. A remote target yields no edge.
	if rel, ok := entity.LocalRunsOn("service.instance", instanceID, s.serverAddr, s.hostID); ok {
		obs.Relations = append(obs.Relations, rel)
	}

	return obs, true
}
