package unifi

import (
	"net/url"
	"os"
	"strings"
	"sync"

	"senhub-agent.go/internal/agent/services/agentstate"
	"senhub-agent.go/internal/agent/services/common"
	"senhub-agent.go/internal/agent/services/entity"
)

// Entity rail (#185): the monitored UniFi Controller as a service.instance
// entity. The probe is the observer; the controller is the observed service.
// The identity is never the endpoint the operator configured (an address is
// a path to the controller, not the controller):
//
//   - the UUID the controller reports about itself, when the account can
//     read it;
//   - else, for a controller running on this host, "unifi@<host.id>";
//   - else (remote controller, nothing readable) there is no identifiable
//     key and therefore no entity.
const (
	entityTypeServiceInstance = "service.instance"
	idKeyServiceInstanceID    = "service.instance.id"
)

// unifiEntitySource reports the monitored controller as a single
// service.instance entity. Observe() is non-blocking and serves the last
// reachability state set by the probe's Collect cycle; ok=false before the
// first cycle so an unprobed controller is not reported as deleted.
type unifiEntitySource struct {
	id string

	// serverAddr is the host parsed from the endpoint; runs_on→host is emitted
	// only when it is loopback.
	serverAddr string
	hostID     func() string // agent host id resolver, overridable in tests

	mu        sync.Mutex
	observed  bool
	reachable bool
	// controllerID is the UUID the controller reports about itself, empty
	// until read (or for good when the account cannot read it).
	controllerID string
}

func newEntitySource(endpoint string) *unifiEntitySource {
	return &unifiEntitySource{
		serverAddr: hostFromEndpoint(endpoint),
		hostID:     unifiHostID,
	}
}

// setControllerID records the controller's self-reported UUID.
func (s *unifiEntitySource) setControllerID(id string) {
	s.mu.Lock()
	s.controllerID = id
	s.mu.Unlock()
}

// isLocalController reports whether the endpoint designates the host the agent
// runs on: a loopback name or address, or this machine's own hostname.
func (s *unifiEntitySource) isLocalController() bool {
	if entity.IsLoopbackHost(s.serverAddr) {
		return true
	}
	hn, err := os.Hostname()
	return err == nil && hn != "" && strings.EqualFold(hn, s.serverAddr)
}

// identity returns the service.instance.id, or "" when the controller has no
// identifiable key.
func (s *unifiEntitySource) identity(controllerID string) string {
	if controllerID != "" {
		return controllerID
	}
	if s.isLocalController() {
		if hid := s.hostID(); hid != "" {
			return "unifi@" + hid
		}
	}
	return ""
}

// hostFromEndpoint extracts the host from an endpoint URL (e.g.
// "https://localhost:8443" → "localhost"), returning the raw endpoint when it
// is not a parseable URL.
func hostFromEndpoint(endpoint string) string {
	if u, err := url.Parse(endpoint); err == nil && u.Hostname() != "" {
		return u.Hostname()
	}
	return endpoint
}

// unifiHostID resolves the agent host's stable machine-id, or "" when unreadable.
func unifiHostID() string {
	hi, err := common.GetHostIdentity()
	if err != nil {
		return ""
	}
	return hi.ID
}

// markReachable records the outcome of a Collect cycle. The entity is
// emitted with a reachable attribute so a consumer sees the controller's
// liveness without parsing metrics.
func (s *unifiEntitySource) markReachable(reachable bool) {
	s.mu.Lock()
	s.observed = true
	s.reachable = reachable
	s.mu.Unlock()
}

// Observe returns the controller entity. ok=false until the first cycle
// has run (nothing observed yet is not "everything deleted").
func (s *unifiEntitySource) Observe() (entity.Observation, bool) {
	s.mu.Lock()
	observed := s.observed
	reachable := s.reachable
	controllerID := s.controllerID
	s.mu.Unlock()

	if !observed {
		return entity.Observation{}, false
	}

	id := s.identity(controllerID)
	if id == "" {
		return entity.Observation{}, true
	}
	svcID := map[string]any{idKeyServiceInstanceID: id}
	obs := entity.Observation{
		Entities: []entity.Entity{
			{
				Type:       entityTypeServiceInstance,
				ID:         svcID,
				Attributes: map[string]any{"unifi.reachable": reachable},
			},
		},
	}
	// monitors edge: agent → controller, anchoring the entity to the agent's
	// monitoring subgraph (else it floats — #506). Emitted only when the agent
	// id is available; a non-materialised From would be buffered then dropped.
	if agentID := agentstate.GetAgentInstanceID(); agentID != "" {
		obs.Relations = append(obs.Relations, entity.Relation{
			Type:     "monitors",
			FromType: entityTypeServiceInstance,
			FromID:   map[string]any{idKeyServiceInstanceID: agentID},
			ToType:   entityTypeServiceInstance,
			ToID:     svcID,
		})
	}

	// runs_on edge: controller → host when the endpoint is loopback.
	if rel, ok := entity.LocalRunsOn(entityTypeServiceInstance, svcID, s.serverAddr, s.hostID()); ok {
		obs.Relations = append(obs.Relations, rel)
	}
	return obs, true
}
