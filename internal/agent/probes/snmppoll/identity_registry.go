package snmppoll

import (
	"fmt"
	"strings"
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
	// byID holds which target last claimed each resolved device id, so two
	// targets that are visibly different devices yet resolve to one id are
	// reported instead of being merged silently; warnedPairs keeps that
	// report to one per pair.
	byID        map[string]idClaim
	warnedPairs map[string]bool
	// downPorts holds the interfaces a recent sweep read as not up, keyed
	// by device id and interface name, so a link a neighbour still lists
	// can be checked against the far end's own view of its port.
	downPorts map[string]time.Time // value: when the mark expires
	// portsRead is when each device last had its port states read, and
	// unreachable when a sweep of it last failed: together they say how
	// much evidence stands behind the far end of a link.
	portsRead   map[string]time.Time // value: when the reading goes stale
	unreachable map[string]time.Time // value: when the failure is forgotten
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

type idClaim struct {
	target     string
	sysName    string
	chassisMAC string
	seen       time.Time
}

// maxWarnedPairs bounds the warned-pair memory; past it the set is cleared
// and a still-colliding pair warns once more.
const maxWarnedPairs = 1024

// polledRegistryTTL bounds how long a polled device's facets stay trusted
// without a refresh — 3× the default topology cadence, the same liveness ratio
// the entity report interval uses.
const polledRegistryTTL = 3 * defaultTopologyInterval

// sharedPolledRegistry is the process-wide registry every snmp_poll entity
// source records into and consults, so a neighbour polled by one probe instance
// reconciles to the canonical id another instance assigned it.
var sharedPolledRegistry = newPolledRegistry()

func newPolledRegistry() *polledRegistry {
	return &polledRegistry{
		byMAC:       map[string]registryEntry{},
		byID:        map[string]idClaim{},
		warnedPairs: map[string]bool{},
		downPorts:   map[string]time.Time{},
		started:     time.Now(),
	}
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

// warmupRemaining is how long the warm-up window still has to run.
func (r *polledRegistry) warmupRemaining(now time.Time) time.Duration {
	return min(max(r.warmup()-now.Sub(r.started), 0), r.warmup())
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
	for k, c := range r.byID {
		if now.Sub(c.seen) > polledRegistryTTL {
			delete(r.byID, k)
		}
	}
}

// claimID records that target resolved to id. It reports a collision when a
// different target holds a fresh claim on the same id and the two show
// different sysNames or chassis MACs: two devices, not one device reached
// by two addresses. The first claimer keeps the id; firstWarn is true once
// per pair.
func (r *polledRegistry) claimID(id, target string, self deviceIdentity, now time.Time) (other idClaim, collided, firstWarn bool) {
	if id == "" || target == "" {
		return idClaim{}, false, false
	}
	name := strings.TrimSpace(self.SysName)
	mac := macHex(self.ChassisMAC)
	r.mu.Lock()
	defer r.mu.Unlock()
	if prev, ok := r.byID[id]; ok && prev.target != target && now.Sub(prev.seen) <= polledRegistryTTL {
		differentName := prev.sysName != "" && name != "" && prev.sysName != name
		differentMAC := prev.chassisMAC != "" && mac != "" && prev.chassisMAC != mac
		if differentName || differentMAC {
			key := pairKey(id, prev.target, target)
			if !r.warnedPairs[key] {
				if len(r.warnedPairs) >= maxWarnedPairs {
					r.warnedPairs = map[string]bool{}
				}
				r.warnedPairs[key] = true
				firstWarn = true
			}
			return prev, true, firstWarn
		}
	}
	r.byID[id] = idClaim{target: target, sysName: name, chassisMAC: mac, seen: now}
	r.evictLocked(now)
	return idClaim{}, false, false
}

func pairKey(id, a, b string) string {
	if a > b {
		a, b = b, a
	}
	return id + "\x00" + a + "\x00" + b
}

func portKey(deviceID, ifName string) string { return deviceID + "\x00" + ifName }

// recordPortStates stores which of a swept device's interfaces are not up.
// An interface read as up clears a previous down mark; an unread status
// changes nothing.
//
// Freshness is counted in the sweeping instance's own cadence (silentAfterSweeps
// intervals): one fixed window would call a slow-swept device stale between two
// good sweeps and flip the mark on every cycle.
func (r *polledRegistry) recordPortStates(deviceID string, ifaces []ifaceRow, interval time.Duration, now time.Time) {
	if deviceID == "" {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.downPorts == nil {
		r.downPorts = map[string]time.Time{}
	}
	if r.portsRead == nil {
		r.portsRead = map[string]time.Time{}
	}
	until := now.Add(freshFor(interval))
	r.portsRead[deviceID] = until
	delete(r.unreachable, deviceID)
	for _, ifc := range ifaces {
		if ifc.Name == "" || ifc.OperStatus == 0 {
			continue
		}
		k := portKey(deviceID, ifc.Name)
		if ifc.OperStatus == ifOperUp {
			delete(r.downPorts, k)
		} else {
			r.downPorts[k] = until
		}
	}
	for k, exp := range r.downPorts {
		if now.After(exp) {
			delete(r.downPorts, k)
		}
	}
}

// portDown reports whether a fresh sweep read this interface as not up.
func (r *polledRegistry) portDown(deviceID, ifName string, now time.Time) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	exp, ok := r.downPorts[portKey(deviceID, ifName)]
	return ok && !now.After(exp)
}

// dropLinksToDownPorts removes connected_to edges whose far end a sweep read
// as down. Each device only knows its own ports: a switch keeps a neighbour
// whose link the other side shut (a virtual link can stay up on one end), and
// without this the link outlives the cut for as long as that neighbour lists it.
func dropLinksToDownPorts(obs *entity.Observation, r *polledRegistry, now time.Time) {
	if r == nil {
		return
	}
	kept := obs.Relations[:0]
	for _, rel := range obs.Relations {
		if rel.Type == relConnectedTo {
			dev, _ := rel.ToID[idKeyNetworkDevice].(string)
			ifn, _ := rel.ToID[idKeyInterfaceName].(string)
			if r.portDown(dev, ifn, now) {
				continue
			}
		}
		kept = append(kept, rel)
	}
	obs.Relations = kept
}

// recordUnreachable notes that a sweep of a known device got no answer.
func (r *polledRegistry) recordUnreachable(deviceID string, interval time.Duration, now time.Time) {
	if r == nil || deviceID == "" {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.unreachable == nil {
		r.unreachable = map[string]time.Time{}
	}
	r.unreachable[deviceID] = now.Add(freshFor(interval))
}

// Values of senhub.link.peer_state, set on the local interface of a link
// whose far end does not have fresh evidence; absent when it does.
const (
	attrLinkPeerState    = "senhub.link.peer_state"
	peerStateUnread      = "unread"
	peerStateStale       = "stale"
	peerStateUnreachable = "unreachable"
)

// peerState says how much evidence stands behind a far device's ports: ""
// when they were read recently, else why not.
func (r *polledRegistry) peerState(deviceID string, now time.Time) string {
	r.mu.Lock()
	defer r.mu.Unlock()
	if exp, ok := r.unreachable[deviceID]; ok && !now.After(exp) {
		return peerStateUnreachable
	}
	exp, ok := r.portsRead[deviceID]
	switch {
	case !ok:
		return peerStateUnread
	case now.After(exp):
		return peerStateStale
	}
	return ""
}

// annotatePeerState marks the local interface of each link whose far end has
// no fresh evidence. The mark rides the interface, not the edge: Toise keeps
// attributes on an embedded relation only for same_as and resolves_to, and a
// physical interface has one neighbour, so the interface names the link.
func annotatePeerState(obs *entity.Observation, r *polledRegistry, now time.Time) {
	if r == nil {
		return
	}
	state := map[string]string{}
	for _, rel := range obs.Relations {
		if rel.Type != relConnectedTo {
			continue
		}
		dev, _ := rel.ToID[idKeyNetworkDevice].(string)
		if st := r.peerState(dev, now); st != "" {
			state[portKey(fmt.Sprint(rel.FromID[idKeyNetworkDevice]), fmt.Sprint(rel.FromID[idKeyInterfaceName]))] = st
		}
	}
	if len(state) == 0 {
		return
	}
	for i, e := range obs.Entities {
		if e.Type != entityTypeNetworkInterface {
			continue
		}
		st, ok := state[portKey(fmt.Sprint(e.ID[idKeyNetworkDevice]), fmt.Sprint(e.ID[idKeyInterfaceName]))]
		if !ok {
			continue
		}
		attrs := make(map[string]any, len(e.Attributes)+1)
		for k, v := range e.Attributes {
			attrs[k] = v
		}
		attrs[attrLinkPeerState] = st
		obs.Entities[i].Attributes = attrs
	}
}

func freshFor(interval time.Duration) time.Duration {
	if interval <= 0 {
		interval = defaultTopologyInterval
	}
	return silentAfterSweeps * interval
}
