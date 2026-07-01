package apache

import (
	"sync"

	"senhub-agent.go/internal/agent/services/agentstate"
	"senhub-agent.go/internal/agent/services/entity"
)

// apacheEntitySource feeds the entity rail. Observe() never blocks: it returns
// the last cached snapshot. The cache is refreshed from each successful
// mod_status fetch in Collect(). ok=false before the first successful fetch so
// the detector does not treat an empty initial cache as "server deleted".
type apacheEntitySource struct {
	instanceID string // stable service.instance.id, computed once at construction
	host       string
	hostID     string // agent host id, target of the local-target runs_on edge
	port       int64
	mu         sync.Mutex
	cache      entity.Observation
	ready      bool
}

// newApacheEntitySource constructs the source. instanceName comes from the
// optional "instance_name" config key; when empty the stable host id is used
// instead. hostID is the resolved agent host identity string (GetHostIdentity().ID
// called once by the constructor); the empty string is the last-resort fallback.
func newApacheEntitySource(instanceName, hostID string, host string, port int) *apacheEntitySource {
	id := resolveInstanceID(instanceName, hostID)
	return &apacheEntitySource{
		instanceID: id,
		host:       host,
		hostID:     hostID,
		port:       int64(port),
	}
}

// resolveInstanceID builds the stable service.instance.id. Precedence:
//  1. operator-supplied instance_name (verbatim, unique across instances)
//  2. "apache@" + stable host id
//  3. "apache" (last resort when host id is unavailable)
func resolveInstanceID(instanceName, hostID string) string {
	if instanceName != "" {
		return instanceName
	}
	if hostID != "" {
		return "apache@" + hostID
	}
	return "apache"
}

// setReachable updates the cached entity observation. up=true replaces the
// cache with a live entity plus the monitors relation from the agent; up=false
// clears it (empty observation with ok=true signals "server gone" — detector
// emits a delete).
func (s *apacheEntitySource) setReachable(up bool, version string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !up {
		s.cache = entity.Observation{}
		s.ready = true
		return
	}
	attrs := map[string]any{
		"service.name":   "apache",
		"server.address": s.host,
		"server.port":    s.port,
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

	// runs_on edge: apache → host when the monitored endpoint is local (loopback),
	// so a locally-monitored apache hangs off the host it runs on instead of
	// floating with only its monitors anchor. A remote endpoint yields no edge.
	if rel, ok := entity.LocalRunsOn("service.instance", targetID, s.host, s.hostID); ok {
		obs.Relations = append(obs.Relations, rel)
	}

	s.cache = obs
	s.ready = true
}

// Observe returns the latest cached entity snapshot. Non-blocking; safe to
// call from the detector goroutine. Returns ok=false until the first Collect()
// cycle completes (success or failure).
func (s *apacheEntitySource) Observe() (entity.Observation, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.cache, s.ready
}
