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
//
// The boolean reports whether the observation is trustworthy this cycle.
// Returning ok=false means "my view failed transiently — keep my last good
// one": the detector then reuses the source's previous observation (bounded
// by a staleness TTL) instead of treating everything the source used to see
// as deleted. A transient SNMP timeout must not delete a whole device tree
// in the consumer (audit D3). An EMPTY observation with ok=true is the
// legitimate way to say "everything I watched is gone".
type Source interface {
	Observe() (Observation, bool)
}

// registered pairs a Source with the registry id its detector-side caches
// key on. Ids are never reused, so a re-registered source starts clean.
type registered struct {
	id  uint64
	src Source
}

var (
	sourcesMu sync.RWMutex
	sources   []registered
	nextID    uint64
)

// RegisterSource adds a Source the detector will poll every cycle and
// returns the function that unregisters it. Probes register their entity
// source at startup and MUST call the returned function on shutdown —
// otherwise the source keeps heartbeating its cached topology forever
// (dead devices never expire in the consumer, reloads duplicate sources;
// audit D4). Process-global, mirroring the entity/log channels — there is
// one detector per agent. Nil-safe: a nil source returns a no-op.
func RegisterSource(s Source) (unregister func()) {
	if s == nil {
		return func() {}
	}
	sourcesMu.Lock()
	nextID++
	id := nextID
	sources = append(sources, registered{id: id, src: s})
	sourcesMu.Unlock()

	var once sync.Once
	return func() {
		once.Do(func() {
			sourcesMu.Lock()
			for i := range sources {
				if sources[i].id == id {
					sources = append(sources[:i], sources[i+1:]...)
					break
				}
			}
			sourcesMu.Unlock()
		})
	}
}

// RegisteredSourceCount reports how many entity sources are currently
// registered with the process-global detector registry. It exists so a
// cross-package reload test can assert there is exactly one strategy's worth
// of sources after a config-reload recreate — no overlap (the old instance
// left its sources behind) and no leak (#495).
func RegisteredSourceCount() int {
	sourcesMu.RLock()
	defer sourcesMu.RUnlock()
	return len(sources)
}

// registeredSources returns a snapshot copy of the registered sources, safe
// to range over without holding the lock.
func registeredSources() []registered {
	sourcesMu.RLock()
	defer sourcesMu.RUnlock()
	cp := make([]registered, len(sources))
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

// dropOrphanEntities enforces the graph invariant that every entity carries at
// least one relation: a node with neither an outgoing relationship of its own
// nor an incoming one (it is some other entity's relationship target) is
// unanchored and useless to a consumer, so it is removed before emission rather
// than published as a floating node.
//
// The host entity is the single exception: it is the infrastructure root, so it
// is legitimately standalone on a host-only agent and is otherwise referenced by
// its children (process/db/... runs_on host). Dropped entities are reported to
// onOrphan (when set) so the drop is observable, never silent.
func dropOrphanEntities(entities []Entity, onOrphan func([]Entity)) []Entity {
	referenced := make(map[string]bool, len(entities))
	for i := range entities {
		for _, rel := range entities[i].Relationships {
			referenced[entityKey(rel.TargetType, rel.TargetID)] = true
		}
	}
	kept := make([]Entity, 0, len(entities))
	var dropped []Entity
	for i := range entities {
		e := entities[i]
		if e.Type == "host" || len(e.Relationships) > 0 || referenced[entityKey(e.Type, e.ID)] {
			kept = append(kept, e)
			continue
		}
		dropped = append(dropped, e)
	}
	if len(dropped) > 0 && onOrphan != nil {
		onOrphan(dropped)
	}
	return kept
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

// ResetForTest clears the process-global entity state (registered sources and
// event-channel subscribers). Exported so cross-package reload tests can start
// from a clean registry; not for production use.
func ResetForTest() {
	resetSourcesForTest()
	resetEventChannelForTest()
}
