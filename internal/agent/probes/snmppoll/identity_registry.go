package snmppoll

import (
	"sync"
	"time"

	"senhub-agent.go/internal/agent/services/entity"
)

// polledRegistry reconciles the SAME physical device across SNMP sources so the
// producer emits ONE canonical id per device, never several.
//
// The problem it solves: a device polled directly resolves to its strong id
// (serial:/engine:), but the SAME device seen only through another device's LLDP
// view carries a weak id (mac: from the chassis-id). Those are two nodes for one
// device — and no consumer feature can repair a producer that emits two ids: the
// fix must be producer-side (the exact-identity contract holds in every Toise
// version, so emitting one canonical id is the version-independent answer).
//
// Each polled device registers its chassis MAC under its canonical id; a
// neighbour known by that same MAC then resolves to the canonical id instead of
// minting a mac: shadow. Reconciliation is keyed on the chassis MAC ONLY:
// globally unique and identical in both views. sysName is deliberately NOT a key
// — it is non-unique (default "Switch") and would falsely merge distinct
// devices.
//
// Process-global because each target is an independent probe instance; entries
// carry a freshness stamp so a stale mapping (a device renumbered or replaced)
// is not trusted past polledRegistryTTL.
type polledRegistry struct {
	mu    sync.Mutex
	byMAC map[string]registryEntry
	// started is when the registry began filling: the agent's start for
	// the shared one. It is empty then, and fills in the order the probe
	// instances happen to sweep.
	started time.Time
	// warmupOverride replaces the interval-derived window in tests.
	warmupOverride time.Duration
}

type registryEntry struct {
	id   string
	seen time.Time
}

// polledRegistryTTL bounds how long a polled device's facets stay trusted
// without a refresh — 3× the default topology cadence, the same liveness ratio
// the entity report interval uses.
const polledRegistryTTL = 3 * defaultTopologyInterval

// sharedPolledRegistry is the process-wide registry every snmp_poll entity
// source records into and consults, so a neighbour polled by one probe instance
// reconciles to the canonical id another instance assigned it.
var sharedPolledRegistry = newPolledRegistry()

func newPolledRegistry() *polledRegistry {
	return &polledRegistry{byMAC: map[string]registryEntry{}, started: time.Now()}
}

// warming reports whether the registry may still be missing devices that
// will be polled shortly after the start. The window is a third of the
// announced entity liveness interval: the consumer expires what is not
// re-asserted within that interval, the last heartbeat before a restart can
// be a third of it old (the re-emission cadence), so holding back for
// another third keeps a missed heartbeat absorbable. Tied to the interval,
// it cannot drift from it; with no detector running there is nothing to
// hold back for.
func (r *polledRegistry) warming(now time.Time) bool {
	return now.Sub(r.started) < r.warmup()
}

func (r *polledRegistry) warmup() time.Duration {
	if r.warmupOverride > 0 {
		return r.warmupOverride
	}
	return entity.ReportInterval() / 3
}

// recordPolled registers a directly-polled device's chassis MAC under its
// canonical id. No-op when the device exposes no chassis MAC (nothing to
// reconcile a neighbour against) or has no resolved id.
func (r *polledRegistry) recordPolled(self deviceIdentity, canonicalID string, now time.Time) {
	if canonicalID == "" || len(self.ChassisMAC) == 0 {
		return
	}
	key := macHex(self.ChassisMAC)
	if key == "" {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.byMAC[key] = registryEntry{id: canonicalID, seen: now}
	r.evictLocked(now)
}

// canonicalFor returns the canonical id of a directly-polled device whose chassis
// MAC matches the neighbour's, or "" when there is no fresh match. A self-match
// (the neighbour's MAC is the polled device itself) is the caller's concern; this
// only maps MAC → canonical id.
func (r *polledRegistry) canonicalFor(n deviceIdentity, now time.Time) (string, bool) {
	if len(n.ChassisMAC) == 0 {
		return "", false
	}
	key := macHex(n.ChassisMAC)
	if key == "" {
		return "", false
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	e, ok := r.byMAC[key]
	if !ok || now.Sub(e.seen) > polledRegistryTTL {
		return "", false
	}
	return e.id, true
}

// evictLocked drops entries older than the TTL. Called under the lock on each
// write so the map cannot grow without bound across device churn.
func (r *polledRegistry) evictLocked(now time.Time) {
	for k, e := range r.byMAC {
		if now.Sub(e.seen) > polledRegistryTTL {
			delete(r.byMAC, k)
		}
	}
}
