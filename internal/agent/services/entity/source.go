package entity

import (
	"sync"
	"time"
)

// Observation is the set of entities and relations a Source currently sees.
// It carries identity + descriptive attributes only — the detector stamps
// event_time and the liveness interval uniformly, so sources never deal with
// timestamps. Returning a smaller set than last cycle is how a source signals
// that something it used to observe is gone (the tracker emits the delete).
type Observation struct {
	Entities  []Entity
	Relations []Relation
}

// Source observes a slice of the infrastructure graph — typically a probe
// reporting the systems it monitors (a db instance + the monitors edge, an
// SNMP-discovered device + its links). The detector calls Observe once per
// cycle and folds the result into the reconcile snapshot.
//
// Observe must return the COMPLETE current set the source sees (not a delta):
// the tracker diffs full snapshots. It is called from the detector goroutine
// and should not block.
type Source interface {
	Observe() Observation
}

var (
	sourcesMu sync.RWMutex
	sources   []Source
)

// RegisterSource adds a Source the detector will poll every cycle. Probes
// register their entity source at startup. Process-global, mirroring the
// entity/log channels — there is one detector per agent.
func RegisterSource(s Source) {
	if s == nil {
		return
	}
	sourcesMu.Lock()
	sources = append(sources, s)
	sourcesMu.Unlock()
}

// registeredSources returns a snapshot copy of the registered sources, safe
// to range over without holding the lock.
func registeredSources() []Source {
	sourcesMu.RLock()
	defer sourcesMu.RUnlock()
	cp := make([]Source, len(sources))
	copy(cp, sources)
	return cp
}

// toEvents stamps an Observation into state events at instant ts with the
// given liveness interval. Entities first, then relations, so a single
// snapshot carries endpoints before the edges that reference them.
func (o Observation) toEvents(ts time.Time, interval time.Duration) []Event {
	out := make([]Event, 0, len(o.Entities)+len(o.Relations))
	for i := range o.Entities {
		e := o.Entities[i]
		out = append(out, Event{Kind: EntityState, Entity: &e, Time: ts, Interval: interval})
	}
	for i := range o.Relations {
		r := o.Relations[i]
		out = append(out, Event{Kind: RelationState, Relation: &r, Time: ts, Interval: interval})
	}
	return out
}

// resetSourcesForTest clears registered sources. Test-only.
func resetSourcesForTest() {
	sourcesMu.Lock()
	sources = nil
	sourcesMu.Unlock()
}
