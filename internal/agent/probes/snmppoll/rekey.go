package snmppoll

import (
	"encoding/hex"
	"strings"
	"time"

	"senhub-agent.go/internal/agent/services/entity"
)

// When the id of a polled device changes while the device stays the same
// (a stack whose chassis MAC follows a new master, an id precedence change
// across an upgrade), the entities the previous id owned are retired
// explicitly with the reason rekeyed: the device, its interfaces, routes,
// neighbours and every other entity keyed on that id. Left alone they would
// die of liveness expiry with no reason, and a consumer would read hundreds
// of things gone at once. The previous sweep is held in memory only: an id
// that changes while the agent is stopped retires by expiry.

// legacyDeviceID is the id the precedence before the chassis MAC moved
// above the engine id gave: serial > engine > mac > name > mgmt.
func legacyDeviceID(id deviceIdentity) string {
	switch {
	case strings.TrimSpace(id.Serial) != "" && strings.TrimSpace(id.VendorPEN) != "":
		return "serial:" + strings.TrimSpace(id.VendorPEN) + ":" + strings.TrimSpace(id.Serial)
	case len(id.EngineID) > 0:
		return "engine:" + hex.EncodeToString(id.EngineID)
	case len(id.ChassisMAC) > 0:
		return "mac:" + macHex(id.ChassisMAC)
	case strings.TrimSpace(id.SysName) != "":
		return "name:" + strings.TrimSpace(id.SysName)
	case strings.TrimSpace(id.MgmtIP) != "":
		return "mgmt:" + canonIP(id.MgmtIP)
	default:
		return ""
	}
}

// sameLogicalDevice reports whether two sweeps of one target read the same
// device: same sysName or a chassis serial in common. A hardware
// replacement under the same name counts as the same device. The engine id
// is no evidence: a configuration copied to another switch carries it.
func sameLogicalDevice(a, b deviceIdentity) bool {
	if n := strings.TrimSpace(a.SysName); n != "" && n == strings.TrimSpace(b.SysName) {
		return true
	}
	for _, x := range a.ChassisSerials {
		for _, y := range b.ChassisSerials {
			if x == y {
				return true
			}
		}
	}
	return false
}

type rekeyBatch struct {
	entities []entity.Entity
	left     int
	reason   string
}

// armRekey compares a published sweep with the previously published one
// and with the legacy precedence, and queues the entities a previous id
// owned. A degraded sweep (a walk failed, the budget cut it, an identity
// walk did not answer) is recorded but never compared: a lower rung read
// through a timeout would retire a device that did not change. A complete
// sweep after a degraded first one does compare, so the id that first
// sweep published is retired once the device reads in full.
func (s *snmpEntitySource) armRekey(res sweepResult, degraded bool, now time.Time) {
	if res.deviceID == "" {
		return
	}
	s.rekeyMu.Lock()
	prev, hadPrev := s.rekeyBase, s.rekeyHaveBase
	s.rekeyBase, s.rekeyHaveBase = res, true
	if degraded {
		s.rekeyMu.Unlock()
		return
	}
	announced := s.legacyRetired[legacyDeviceID(res.self)]
	s.rekeyMu.Unlock()

	current := s.buildQuiet(res, now)
	var retired []entity.Entity
	if hadPrev && prev.deviceID != res.deviceID && sameLogicalDevice(prev.self, res.self) {
		retired = ownedBy(droppedEntities(s.buildQuiet(prev, now), current), prev.deviceID)
		s.moduleLogger.Info().Str("target", s.cfg.Target).
			Str("previous_device_id", prev.deviceID).Str("device_id", res.deviceID).
			Int("retired", len(retired)).
			Msg("device id changed for the same device; retiring the previous id as rekeyed")
	}
	legacy := legacyDeviceID(res.self)
	if legacy != "" && legacy != res.deviceID && !announced && !(hadPrev && legacy == prev.deviceID) {
		old := res
		old.deviceID = legacy
		retired = append(retired, ownedBy(droppedEntities(s.buildQuiet(old, now), current), legacy)...)
		s.rekeyMu.Lock()
		if s.legacyRetired == nil {
			s.legacyRetired = map[string]bool{}
		}
		s.legacyRetired[legacy] = true
		s.rekeyMu.Unlock()
	}
	if len(retired) == 0 {
		return
	}
	s.rekeyMu.Lock()
	s.rekeyBatches = append(s.rekeyBatches, rekeyBatch{entities: dedupeEntities(retired), left: entity.RekeyAnnounceCycles, reason: entity.ReasonRekeyed})
	s.rekeyMu.Unlock()
}

// ownedBy keeps the entities whose identity carries deviceID: an entity
// keyed on something else (a neighbour device, a network.address) is
// emitted by the new id too and must not be retired.
func ownedBy(in []entity.Entity, deviceID string) []entity.Entity {
	var out []entity.Entity
	for _, e := range in {
		if e.ID[idKeyNetworkDevice] == deviceID {
			out = append(out, e)
		}
	}
	return out
}

// announceRekey publishes each queued retirement once per observation
// cycle, for entity.RekeyAnnounceCycles cycles: a delete can be dropped
// under backpressure, and the consumer's delete is idempotent. Each batch
// counts its own cycles, and a batch is never changed once queued, so the
// publisher reads it without the lock.
func (s *snmpEntitySource) announceRekey() {
	s.rekeyMu.Lock()
	batches := make([]rekeyBatch, 0, len(s.rekeyBatches))
	kept := s.rekeyBatches[:0:0]
	for _, b := range s.rekeyBatches {
		batches = append(batches, b)
		if b.left--; b.left > 0 {
			kept = append(kept, b)
		}
	}
	s.rekeyBatches = kept
	s.rekeyMu.Unlock()
	for _, b := range batches {
		for i := range b.entities {
			e := b.entities[i]
			s.publish(entity.Event{Kind: entity.EntityDelete, Entity: &e, DeleteReason: b.reason})
		}
	}
}

func dedupeEntities(in []entity.Entity) []entity.Entity {
	seen := make(map[string]bool, len(in))
	out := make([]entity.Entity, 0, len(in))
	for _, e := range in {
		k := entityIdentity(e)
		if !seen[k] {
			seen[k] = true
			out = append(out, e)
		}
	}
	return out
}

// retirePreviousVersionRoutes retires, once per device and process, the
// routes an agent without the system-route filter would have published from
// the same complete read and this one does not: the routes an earlier version
// left in the consumer's graph. Only routes keyed on deviceID and absent from
// the current observation are retired, so a route still published never is.
// A delete for a route the consumer never held is a no-op there, so a fresh
// install costs nothing.
func (s *snmpEntitySource) retirePreviousVersionRoutes(deviceID string, previous, current entity.Observation) {
	if deviceID == "" {
		return
	}
	s.rekeyMu.Lock()
	if s.previousRoutesRetired == nil {
		s.previousRoutesRetired = map[string]bool{}
	}
	done := s.previousRoutesRetired[deviceID]
	s.previousRoutesRetired[deviceID] = true
	s.rekeyMu.Unlock()
	if done {
		return
	}
	var routes []entity.Entity
	for _, e := range ownedBy(droppedEntities(previous, current), deviceID) {
		if e.Type == entityTypeNetworkRoute {
			routes = append(routes, e)
		}
	}
	if len(routes) == 0 {
		return
	}
	s.moduleLogger.Info().Str("target", s.cfg.Target).Str("device_id", deviceID).Int("routes", len(routes)).
		Msg("retiring the system routes an earlier version published as excluded_by_config")
	s.rekeyMu.Lock()
	s.rekeyBatches = append(s.rekeyBatches, rekeyBatch{entities: dedupeEntities(routes), left: entity.RekeyAnnounceCycles, reason: entity.ReasonExcludedByConfig})
	s.rekeyMu.Unlock()
}
