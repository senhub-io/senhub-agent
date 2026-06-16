package process

import (
	"sync"

	"senhub-agent.go/internal/agent/services/entity"
)

// processEntitySource feeds the Toise entity rail with one "process" node per
// monitored process plus a runs_on edge to the host. Identity is the Toise
// contract pair {process.pid, process.creation.time}: a PID reused after the
// original process exits is a *different* entity (the tracker emits a delete
// for the old one and a state for the new), so the creation time is part of
// the key, never an attribute.
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

	obs := entity.Observation{}
	for _, p := range s.procs {
		id := map[string]any{
			"process.pid":           int64(p.pid),
			"process.creation.time": p.createTime,
		}
		attrs := map[string]any{"process.name": p.name}
		if p.owner != "" {
			attrs["process.owner"] = p.owner
		}
		obs.Entities = append(obs.Entities, entity.Entity{
			Type:       "process",
			ID:         id,
			Attributes: attrs,
		})
		if s.hostID != nil {
			obs.Relations = append(obs.Relations, entity.Relation{
				Type:     "runs_on",
				FromType: "process",
				FromID:   id,
				ToType:   "host",
				ToID:     s.hostID,
			})
		}
	}
	return obs, true
}
