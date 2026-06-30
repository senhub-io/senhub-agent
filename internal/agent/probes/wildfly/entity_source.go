package wildfly

import (
	"net/url"
	"strconv"
	"sync"

	"senhub-agent.go/internal/agent/services/agentstate"
	"senhub-agent.go/internal/agent/services/entity"
)

// wildflyEntitySource feeds the entity rail with the WildFly instance this
// probe monitors. It reports a single service.instance entity with a stable,
// non-network-derived id. The entity is emitted only while the management API
// is reachable (up=true); a transient failure returns ok=false so the tracker
// reuses the last good snapshot rather than emitting a delete.
type wildflyEntitySource struct {
	instanceID   string
	baseAttrs    map[string]any
	serverAddr   string // host from the endpoint; a runs_on→host is emitted only when it is loopback
	runsOnHostID string // agent host id; target of the local-target runs_on edge
	mu           sync.RWMutex
	up           bool
	version      string
}

// newWildflyEntitySource builds the entity source. The instance id follows the
// D1 precedence rule: operator-supplied instance_name if set, else
// "wildfly@<hostID>" where hostID comes from the injected resolveHostID func,
// else "wildfly" as a last resort. Descriptive server.address / server.port
// attributes are kept but never used for identity.
func newWildflyEntitySource(endpoint, instanceName string, resolveHostID func() string) *wildflyEntitySource {
	var hostID string
	if resolveHostID != nil {
		hostID = resolveHostID()
	}
	instanceID := resolveInstanceID(instanceName, func() string { return hostID })
	host, port := hostPortFromEndpoint(endpoint)
	return &wildflyEntitySource{
		instanceID:   instanceID,
		serverAddr:   host,
		runsOnHostID: hostID,
		baseAttrs: map[string]any{
			"service.name":   "wildfly",
			"server.address": host,
			"server.port":    port,
		},
	}
}

// resolveInstanceID applies the D1 precedence rule for service.instance.id:
//  1. operator-supplied instance_name → verbatim
//  2. "wildfly@<hostID>" from resolveHostID
//  3. "wildfly" as last resort
func resolveInstanceID(instanceName string, resolveHostID func() string) string {
	if instanceName != "" {
		return instanceName
	}
	if resolveHostID != nil {
		if id := resolveHostID(); id != "" {
			return "wildfly@" + id
		}
	}
	return "wildfly"
}

// hostPortFromEndpoint extracts host and port (as int64) from an HTTP(S) URL.
// Missing port defaults to 9990 (WildFly management API conventional default).
func hostPortFromEndpoint(rawURL string) (host string, port int64) {
	u, err := url.Parse(rawURL)
	if err != nil || u.Hostname() == "" {
		return "localhost", 9990
	}
	host = u.Hostname()
	p := u.Port()
	if p == "" {
		return host, 9990
	}
	n, err := strconv.ParseInt(p, 10, 64)
	if err != nil {
		return host, 9990
	}
	return host, n
}

// setReachable is called from Collect to report whether the management API
// responded successfully this cycle. When version is non-empty it is stored as
// the service.version attribute (updated on every successful cycle).
func (s *wildflyEntitySource) setReachable(up bool, version string) {
	s.mu.Lock()
	s.up = up
	if up && version != "" {
		s.version = version
	}
	s.mu.Unlock()
}

// Observe implements entity.Source. It returns the wildfly service.instance
// entity when the endpoint is reachable, along with a monitors relation from
// this agent to the target. On a transient failure it returns (_, false) so
// the detector keeps the last good snapshot alive.
func (s *wildflyEntitySource) Observe() (entity.Observation, bool) {
	s.mu.RLock()
	up := s.up
	version := s.version
	s.mu.RUnlock()

	if !up {
		return entity.Observation{}, false
	}

	attrs := make(map[string]any, len(s.baseAttrs)+1)
	for k, v := range s.baseAttrs {
		attrs[k] = v
	}
	if version != "" {
		attrs["service.version"] = version
	}

	targetID := map[string]any{"service.instance.id": s.instanceID}
	obs := entity.Observation{
		Entities: []entity.Entity{{
			Type:       "service.instance",
			ID:         targetID,
			Attributes: attrs,
		}},
	}

	if agentID := agentstate.GetAgentInstanceID(); agentID != "" {
		obs.Relations = append(obs.Relations, entity.Relation{
			Type:     "monitors",
			FromType: "service.instance",
			FromID:   map[string]any{"service.instance.id": agentID},
			ToType:   "service.instance",
			ToID:     targetID,
		})
	}

	// runs_on edge: anchor a locally-monitored WildFly to the agent host so it
	// does not float with only its monitors edge. The helper's collapse guard
	// suppresses the edge for a remote target or a loopback-derived identity.
	if rel, ok := entity.LocalRunsOn("service.instance", targetID, s.serverAddr, s.runsOnHostID); ok {
		obs.Relations = append(obs.Relations, rel)
	}

	return obs, true
}
