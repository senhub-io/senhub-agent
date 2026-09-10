package entity

import (
	"context"
	"time"
)

// reEmitTicks is how many ticks the Tracker suppresses an UNCHANGED state
// before re-publishing it (see NewTracker below, called with reEmitTicks×tick).
// The steady-state re-emission cadence is therefore reEmitTicks×tick — NOT one
// tick — and the liveness window must be sized against that effective cadence.
const reEmitTicks = 2

// livenessSlackOverReEmit sizes the Interval carried on each event as a multiple
// of the effective RE-EMISSION cadence (reEmitTicks×tick), so the consumer's
// expiry (last_seen + Interval) survives missed re-emissions.
//
// The earlier code multiplied the TICK by the slack factor, not the re-emission
// cadence. At 60s ticks the effective cadence is 120s, so the previous 5×tick
// gave a 300s window = only 2.5× the re-emit cadence: it survived ONE missed
// re-emission (next at ~240s) but not two (~360s > 300s), leaving zero
// tolerance for a second consecutive miss — and the whole entity stack of an
// intermittently-slow agent flapped (#454). Sizing against the effective
// cadence with a 3× slack absorbs two consecutive missed re-emissions before
// expiry. The topology consumer derives its expiry from this Interval with no
// hidden margin, so the value must carry the whole slack itself.
const livenessSlackOverReEmit = 3

// livenessSlackFactor is the resulting multiple of the TICK
// (reEmitTicks × livenessSlackOverReEmit): 6× at the 60s default → a 360s
// window, i.e. 3× the 120s re-emission cadence.
const livenessSlackFactor = reEmitTicks * livenessSlackOverReEmit

// lastGoodTTL bounds how long the detector keeps serving a source's last
// good observation once Observe starts reporting failures (ok=false). A
// transient sweep failure must not delete a device tree in the consumer
// (audit D3); a source failing for longer than this is treated as truly
// gone and its entities expire through the normal delete path.
const lastGoodTTL = 15 * time.Minute

// HostIdentityFunc resolves the current host identity. It may fail (e.g. the
// OS host info is briefly unavailable); the detector skips that cycle.
type HostIdentityFunc func() (HostIdentity, error)

// AgentIdentityFunc resolves the agent's own identity. It does not fail —
// the agent key and build version are known once configured.
type AgentIdentityFunc func() AgentIdentity

// Detector drives Lot 1 emission: on a fixed cadence it observes the host
// and the agent and publishes the foundation events (host + service.instance
// + runs_on). Re-emitting the same state each tick is the heartbeat — the
// consumer coalesces identical states (at-least-once is idempotent), and the
// carried Interval lets it expire the entity if heartbeats stop.
//
// Lot 1 has no disappearing entities (the host and the agent are always
// present while the agent runs), so no delete logic is needed yet; the full
// state/delete lifecycle tracker arrives with Lot 2 (probe targets that come
// and go).
type Detector struct {
	host           HostIdentityFunc
	agent          AgentIdentityFunc
	interval       time.Duration
	publish        func(Event)
	now            func() time.Time
	onOrphan       func([]Relation)
	onOrphanEntity func([]Entity)
	// lastGood caches, per registered-source id, the most recent
	// observation reported with ok=true, so a transient failure serves
	// stale-but-real topology instead of an empty set (audit D3).
	lastGood map[uint64]cachedObservation
}

type cachedObservation struct {
	obs Observation
	at  time.Time
}

// NewDetector builds a Detector. interval is the heartbeat cadence and is
// also carried on each event as the liveness backstop hint. publish defaults
// to PublishEvent and now to time.Now when nil (overridable in tests).
func NewDetector(host HostIdentityFunc, agent AgentIdentityFunc, interval time.Duration) *Detector {
	return &Detector{host: host, agent: agent, interval: interval}
}

// OnOrphanRelations registers a hook called with any relations that could not
// be folded onto a source entity this cycle (the source endpoint was absent
// from the observation). Nil-safe; used by the wiring layer to surface the
// producer bug via its logger.
func (d *Detector) OnOrphanRelations(fn func([]Relation)) {
	d.onOrphan = fn
}

// OnOrphanEntities registers a hook called with any entity dropped before
// emission for carrying no relation at all (the anti-orphan guard; host is never
// dropped). Nil-safe; used by the wiring layer to surface the producer bug via
// its logger.
func (d *Detector) OnOrphanEntities(fn func([]Entity)) {
	d.onOrphanEntity = fn
}

// Run emits the foundation once immediately, then on every interval tick,
// until the context is cancelled. Blocks; run it in its own goroutine.
func (d *Detector) Run(ctx context.Context) {
	publish := d.publish
	if publish == nil {
		publish = PublishEvent
	}
	now := d.now
	if now == nil {
		now = time.Now
	}

	// One Tracker for the detector's lifetime: it remembers the last-seen
	// set so it can emit deletes when an item disappears between cycles.
	// Suppress unchanged heartbeats for reEmitTicks ticks — this defines the
	// effective re-emission cadence the liveness window is sized against
	// (see livenessSlackOverReEmit).
	tracker := NewTracker(publish, reEmitTicks*d.interval)
	d.reconcile(tracker, now())

	ticker := time.NewTicker(d.interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			d.reconcile(tracker, now())
		}
	}
}

// reconcile observes the host + agent at instant ts and feeds the resulting
// snapshot to the tracker, which emits state (heartbeat) + any deletes. A
// failure to read host identity skips this cycle rather than emitting a host
// entity with an empty id.
func (d *Detector) reconcile(t *Tracker, ts time.Time) {
	h, err := d.host()
	if err != nil || h.ID == "" {
		return
	}
	a := d.agent()
	if a.InstanceID == "" {
		return
	}
	// Tick at d.interval; emit a slacked Interval (livenessSlackOverReEmit× the
	// effective re-emission cadence, = livenessSlackFactor× the tick) so a late
	// heartbeat does not expire a live entity on the consumer (#454).
	interval := d.interval * livenessSlackFactor

	// Foundation (host + service.instance + runs_on) plus everything the
	// registered sources observe this cycle (probe-monitored systems), merged
	// so a relation can resolve its source endpoint against any entity seen
	// this cycle.
	obs := DetectFoundation(h, a)
	if d.lastGood == nil {
		d.lastGood = map[uint64]cachedObservation{}
	}
	// reasons collects why an entity is about to disappear, for the cases the
	// detector can explain. The tracker sees only absence and would otherwise
	// call every one of them a termination — including the two below, where
	// nothing is known about the resource at all (#806).
	reasons := map[string]string{}
	regs := registeredSources()
	live := make(map[uint64]bool, len(regs))
	for _, r := range regs {
		live[r.id] = true
		o, ok := r.src.Observe()
		switch {
		case ok:
			d.lastGood[r.id] = cachedObservation{obs: o, at: ts}
		default:
			if cached, has := d.lastGood[r.id]; has && ts.Sub(cached.at) < lastGoodTTL {
				// Transient failure: keep the consumer's view of this
				// slice of the graph until the TTL says it is real.
				o = cached.obs
			} else {
				// Failing beyond the TTL (or never succeeded): the
				// empty set flows through and absence-deletes fire. We
				// stopped seeing this slice of the graph; we did not
				// learn that it went away.
				markUnmonitored(reasons, cached.obs.Entities)
				o = Observation{}
			}
		}
		obs = obs.merge(o)
	}
	// Drop caches of unregistered sources so a stopped probe's topology
	// expires instead of being served forever (audit D4). The probe stopped or
	// was removed from the configuration: its targets are unobserved, not gone.
	for id, cached := range d.lastGood {
		if !live[id] {
			markUnmonitored(reasons, cached.obs.Entities)
			delete(d.lastGood, id)
		}
	}
	// The host's governance descends to what the sources placed here:
	// its location to everything local, its ownership to the types an
	// operator reasons about. Nothing remote inherits either.
	obs = inheritHostGovernance(obs, h.ID, h.Governance)
	// Fold each relation onto its source entity (embedded entity.relationships)
	// before the tracker, so the tracker reconciles entities only.
	entities, orphans := obs.foldRelationships()
	if len(orphans) > 0 && d.onOrphan != nil {
		d.onOrphan(orphans)
	}
	// Invariant: never publish an unanchored node. Drop any entity with no
	// relation (host excepted) before it reaches the wire. One that was
	// published before and is dropped now lost its anchor, not its existence.
	kept, unanchored := dropOrphanEntities(entities, d.onOrphanEntity)
	for i := range unanchored {
		reasons[entityKey(unanchored[i].Type, unanchored[i].ID)] = ReasonParentRemoved
	}
	t.Reconcile(stateEvents(kept, ts, interval), ts, reasons)
}

// markUnmonitored records that every entity a source used to report is about
// to vanish because the agent stopped observing it, not because it went away.
// An entity another source still reports this cycle is not deleted at all, so
// a stale mark for it is inert.
func markUnmonitored(reasons map[string]string, entities []Entity) {
	for i := range entities {
		reasons[entityKey(entities[i].Type, entities[i].ID)] = ReasonUnmonitored
	}
}
