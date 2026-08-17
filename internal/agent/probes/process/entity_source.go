package process

import (
	"sync"

	"senhub-agent.go/internal/agent/services/entity"
)

// processEntitySource feeds the Toise entity rail with one "process" node per
// monitored process plus a runs_on edge to the host.
//
// Identity is {host.id, process.pid, process.creation.time}. The pair without
// the host was the contract until #741: a pid plus a creation instant is unique
// on ONE machine, and collides the moment two hosts start a process with the
// same pid at the same second — which is not exotic on a fleet booted from the
// same image, where early pids are assigned in the same order at the same time.
//
// Scoping by host is the same repair applied to loopback endpoints (#713) and
// to local databases (#740): an identity narrower than the observation domain
// silently merges distinct things. It is also what lets the pid be published as
// a telemetry join key at all — pointing a consumer at a pid that might belong
// to another machine is a confident wrong answer, so the key waited for the
// scope.
//
// The creation time stays in the key: a PID reused after the original process
// exits is a *different* entity (the tracker emits a delete for the old one and
// a state for the new), so it is part of the identity, never an attribute.
//
// It is only wired in inventory mode — when the operator named the processes
// to watch (by_name / by_user). A pure top_n or unfiltered view is a resource
// sample whose membership churns every cycle; emitting entities there would
// create and delete nodes on every heartbeat and flood the graph, so those
// modes keep the BaseProbe NoOp source instead.
type processEntitySource struct {
	mu     sync.RWMutex
	ready  bool
	hostID map[string]any
	procs  []procEntity
}

// procEntity is the identity + descriptive facts of one monitored process,
// snapshotted each Collect cycle.
type procEntity struct {
	pid        int32
	createTime int64
	name       string
	owner      string
}

func newProcessEntitySource() *processEntitySource {
	return &processEntitySource{}
}

// update replaces the observed set. Called once per Collect cycle with the
// host identity and the filtered process snapshots.
func (s *processEntitySource) update(hostID string, procs []procEntity) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.ready = true
	if hostID != "" {
		s.hostID = map[string]any{"host.id": hostID}
	}
	s.procs = procs
}

// Observe implements entity.Source. It returns ok=false until the first
// successful Collect so a transient enumeration failure does not delete the
// whole process set from the consumer; an empty-but-ready set is a legitimate
// "everything I watched is gone".
func (s *processEntitySource) Observe() (entity.Observation, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if !s.ready {
		return entity.Observation{}, false
	}

	// No host id, no entities. The identity is scoped by it, and an unscoped
	// process node is the collision this fixes — better a visible gap than
	// nodes that merge across machines.
	hostID, _ := s.hostID["host.id"].(string)
	if hostID == "" {
		return entity.Observation{}, false
	}

	obs := entity.Observation{}
	for _, p := range s.procs {
		id := map[string]any{
			"host.id":               hostID,
			"process.pid":           int64(p.pid),
			"process.creation.time": p.createTime,
		}
		attrs := map[string]any{"process.name": p.name}
		if p.owner != "" {
			attrs["process.owner"] = p.owner
		}
		obs.Entities = append(obs.Entities, entity.Entity{
			Type:       entity.TypeProcess,
			ID:         id,
			Attributes: attrs,
		})
		obs.Relations = append(obs.Relations, entity.Relation{
			Type:     entity.RelRunsOn,
			FromType: entity.TypeProcess,
			FromID:   id,
			ToType:   entity.TypeHost,
			ToID:     s.hostID,
		})
	}
	return obs, true
}
