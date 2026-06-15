package tomcat

import (
	"sync"

	"senhub-agent.go/internal/agent/services/agentstate"
	"senhub-agent.go/internal/agent/services/entity"
)

// tomcatEntitySource is the entity.Source for the Tomcat probe.
//
// Identity rule (D1, option A — stable non-network-derived id):
//
//	if instance_name is set → use it verbatim as service.instance.id
//	else                    → "tomcat@" + hostID  (host machine-id)
//	fallback (hostID empty) → "tomcat"
//
// server.address and server.port are kept as DESCRIPTIVE attributes only —
// they never appear in the identity.
//
// When the target is reachable (up==true), Observe also returns a monitors
// relation from the agent's own service.instance to this target.  The edge
// is omitted when the agent instance id is unknown (entity emission off).
type tomcatEntitySource struct {
	instanceID string // computed once at construction; immutable
	addr       string // descriptive, not identity
	port       int64  // descriptive, not identity

	mu    sync.RWMutex
	up    bool
	attrs map[string]any // last descriptive attrs set by SetUp
}

func newTomcatEntitySource(instanceName, hostID, addr string, port int64) *tomcatEntitySource {
	id := resolveInstanceID(instanceName, hostID)
	src := &tomcatEntitySource{
		instanceID: id,
		addr:       addr,
		port:       port,
	}
	// Pre-populate descriptive attrs (server.address / server.port never change).
	src.attrs = map[string]any{
		"service.name":   "tomcat",
		"server.address": addr,
		"server.port":    port,
	}
	return src
}

// resolveInstanceID applies the precedence rule and is exported for tests.
func resolveInstanceID(instanceName, hostID string) string {
	if instanceName != "" {
		return instanceName
	}
	if hostID != "" {
		return "tomcat@" + hostID
	}
	return "tomcat"
}

// SetUp updates the liveness flag. Call SetUp(true, nil) on Collect success
// and SetUp(false, nil) on Collect failure.  attrs is accepted but ignored
// here — the descriptive set is fixed at construction (server.address and
// server.port don't change across cycles).
func (s *tomcatEntitySource) SetUp(up bool, _ map[string]any) {
	s.mu.Lock()
	s.up = up
	s.mu.Unlock()
}

// Observe implements entity.Source.
// When up==false it returns ok=false (D3 contract: keep last-known state in
// the consumer rather than deleting the entity on a transient outage).
// When up==true it returns the service.instance entity plus a monitors edge
// from the agent's own identity (skipped when the agent id is unknown).
func (s *tomcatEntitySource) Observe() (entity.Observation, bool) {
	s.mu.RLock()
	up, attrs, instanceID, addr, port := s.up, s.attrs, s.instanceID, s.addr, s.port
	s.mu.RUnlock()

	if !up {
		return entity.Observation{}, false
	}

	// Ensure attrs is always populated (defensive, normally set at construction).
	if attrs == nil {
		attrs = map[string]any{
			"service.name":   "tomcat",
			"server.address": addr,
			"server.port":    port,
		}
	}

	targetID := map[string]any{"service.instance.id": instanceID}

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

	return obs, true
}
