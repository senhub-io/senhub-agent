package zookeeper

import (
	"fmt"
	"sync"

	"senhub-agent.go/internal/agent/services/agentstate"
	"senhub-agent.go/internal/agent/services/entity"
)

// entityObserver implements entity.Source for the zookeeper probe.
//
// ID resolution order (D1 decision, precedence 1→3):
//  1. operator config key "instance_name" — pinned at construction.
//  2. tech-reported server id from the ZooKeeper "conf" 4lw command
//     (serverId field = myid, a stable per-node integer); formatted as
//     "zookeeper:<id>". Pinned on the first successful Collect cycle.
//  3. host-derived fallback "zookeeper@<host.id>". Pinned once the id
//     has been unreachable long enough for the caller to decide to degrade.
//
// Immutability: the id is pinned exactly once. Observe() returns ok=false
// until pinning happens so the detector never emits an entity before the
// stable id is known (a changing id would re-key the entity in the consumer).
type entityObserver struct {
	mu sync.Mutex

	// pinnedID is set once and never changed. Empty = not yet pinned.
	pinnedID string

	// addr / port are immutable descriptive attributes.
	addr string
	port int

	// obs is the last good Observation (rebuilt when the pinned id or version
	// changes). up tracks the liveness signal from the last Collect.
	obs entity.Observation
	up  bool

	// hostIDFunc returns the host's stable machine id for the precedence-3
	// fallback. Injected so tests never touch gopsutil.
	hostIDFunc func() string
}

// pin records the stable id, builds the first observation, and marks the source
// ready. It is idempotent — subsequent calls with the same id are no-ops; the
// guard on pinnedID prevents re-pinning.
func (e *entityObserver) pin(id string) {
	e.pinnedID = id
}

// needsPin reports whether the entity id has not yet been resolved. Called by
// the probe before a collect to decide whether to attempt the "conf" lookup.
func (e *entityObserver) needsPin() bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.pinnedID == ""
}

// Observe implements entity.Source. Returns ok=false when the id has not been
// pinned yet (we have not yet learned a stable id to emit) or when the last
// Collect cycle reported the node as down (the detector reuses its previous
// good observation rather than emitting a delete — audit D3 contract).
func (e *entityObserver) Observe() (entity.Observation, bool) {
	e.mu.Lock()
	defer e.mu.Unlock()

	if e.pinnedID == "" {
		// id not yet pinned — withhold until we know the stable identity.
		return entity.Observation{}, false
	}
	if !e.up {
		// transient outage — detector should keep the previous snapshot.
		return e.obs, false
	}
	return e.obs, true
}

// setUp is called after each Collect cycle. serverID is the resolved
// "zookeeper:<n>" string (empty when the conf command is unavailable).
// version is the ZooKeeper version string; pass "" when unknown.
func (e *entityObserver) setUp(serverID string, up bool, version string) {
	e.mu.Lock()
	defer e.mu.Unlock()

	e.up = up

	if e.pinnedID == "" {
		// Attempt to pin the id on the first opportunity.
		if serverID != "" {
			e.pin(serverID)
		} else if !up {
			// Target unreachable and no id yet; keep waiting.
			return
		} else {
			// Target reachable but conf didn't yield a server id — fall back
			// to host identity now, committing to precedence-3.
			hostID := ""
			if e.hostIDFunc != nil {
				hostID = e.hostIDFunc()
			}
			if hostID != "" {
				e.pin(fmt.Sprintf("zookeeper@%s", hostID))
			} else {
				e.pin("zookeeper")
			}
		}
	}

	if !up {
		return
	}

	// Rebuild the observation with the (now stable) pinned id.
	e.obs = e.buildObservation(version)
}

// buildObservation assembles the entity.Observation for the pinned id.
// Must be called with e.mu held.
func (e *entityObserver) buildObservation(version string) entity.Observation {
	instanceID := e.pinnedID

	attrs := map[string]any{
		"service.name":   "zookeeper",
		"server.address": e.addr,
		"server.port":    e.port,
	}
	if version != "" {
		attrs["service.version"] = version
	}

	ent := entity.Entity{
		Type:       "service.instance",
		ID:         map[string]any{"service.instance.id": instanceID},
		Attributes: attrs,
	}

	obs := entity.Observation{
		Entities: []entity.Entity{ent},
	}

	agentID := agentstate.GetAgentInstanceID()
	if agentID != "" {
		obs.Relations = append(obs.Relations, entity.Relation{
			Type:     "monitors",
			FromType: "service.instance",
			FromID:   map[string]any{"service.instance.id": agentID},
			ToType:   "service.instance",
			ToID:     map[string]any{"service.instance.id": instanceID},
		})
	}

	return obs
}
