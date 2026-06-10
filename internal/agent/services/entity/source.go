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

// merge folds another observation into this one. The detector merges the
// foundation and every source into a single per-cycle observation so a
// relation can resolve its source endpoint against any entity seen this cycle,
// not only the ones from the same source.
func (o Observation) merge(other Observation) Observation {
	o.Entities = append(o.Entities, other.Entities...)
	o.Relations = append(o.Relations, other.Relations...)
	return o
}

// foldRelationships attaches each relation to its source entity (matched by
// the relation's From endpoint = an entity's type+identity) as an embedded
// Relationship, and returns the entities with their relationship sets filled.
// The embedded descriptor is bare: only the target type+id survive — an edge
// attribute that must persist belongs on an entity, not the wire (re-homing
// the currently-dropped host/SNMP edge attributes is tracked in #239).
//
// A relation whose source entity is absent from the set is returned as an
// orphan rather than silently dropped (every producer is expected to emit the
// source endpoint in the same cycle; an orphan is a producer bug worth
// surfacing).
func (o Observation) foldRelationships() (entities []Entity, orphans []Relation) {
	entities = make([]Entity, len(o.Entities))
	copy(entities, o.Entities)

	idx := make(map[string]int, len(entities))
	for i := range entities {
		entities[i].Relationships = nil
		idx[entityKey(entities[i].Type, entities[i].ID)] = i
	}
	for _, r := range o.Relations {
		i, ok := idx[entityKey(r.FromType, r.FromID)]
		if !ok {
			orphans = append(orphans, r)
			continue
		}
		entities[i].Relationships = append(entities[i].Relationships, Relationship{
			Type:       r.Type,
			TargetType: r.ToType,
			TargetID:   r.ToID,
		})
	}
	return entities, orphans
}

// stateEvents stamps entities into state events at instant ts with the given
// liveness interval.
func stateEvents(entities []Entity, ts time.Time, interval time.Duration) []Event {
	out := make([]Event, len(entities))
	for i := range entities {
		e := entities[i]
		out[i] = Event{Kind: EntityState, Entity: &e, Time: ts, Interval: interval}
	}
	return out
}

// resetSourcesForTest clears registered sources. Test-only.
func resetSourcesForTest() {
	sourcesMu.Lock()
	sources = nil
	sourcesMu.Unlock()
}
