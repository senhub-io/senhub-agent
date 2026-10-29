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
// device: same sysName, a chassis serial in common or the same engine id. A hardware
// replacement under the same name counts as the same device.
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

// armRekey compares a successful sweep with the previous one and with the
// legacy precedence, and queues the entities a previous id owned.
func (s *snmpEntitySource) armRekey(prev sweepResult, hadPrev bool, res sweepResult, now time.Time) {
	if res.deviceID == "" {
		return
	}
	current := s.buildQuiet(res, now)
	var retired []entity.Entity
	if hadPrev && prev.deviceID != "" && prev.deviceID != res.deviceID && sameLogicalDevice(prev.self, res.self) {
		retired = append(retired, droppedEntities(s.buildQuiet(prev, now), current)...)
		s.moduleLogger.Info().Str("target", s.cfg.Target).
			Str("previous_device_id", prev.deviceID).Str("device_id", res.deviceID).
			Int("retired", len(retired)).
			Msg("device id changed for the same device; retiring the previous id as rekeyed")
	}
	legacy := legacyDeviceID(res.self)
	s.rekeyMu.Lock()
	announced := s.legacyRetired[legacy]
	s.rekeyMu.Unlock()
	if legacy != "" && legacy != res.deviceID && !announced && !(hadPrev && legacy == prev.deviceID) {
		old := res
		old.deviceID = legacy
		retired = append(retired, droppedEntities(s.buildQuiet(old, now), current)...)
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
	s.rekeyRetired = dedupeEntities(append(s.rekeyRetired, retired...))
	s.rekeyLeft = entity.RekeyAnnounceCycles
	s.rekeyMu.Unlock()
}

// announceRekey publishes the queued retirements, once per observation
// cycle for entity.RekeyAnnounceCycles cycles: a delete can be dropped
// under backpressure, and the consumer's delete is idempotent.
func (s *snmpEntitySource) announceRekey() {
	s.rekeyMu.Lock()
	if s.rekeyLeft <= 0 {
		s.rekeyMu.Unlock()
		return
	}
	s.rekeyLeft--
	retired := s.rekeyRetired
	if s.rekeyLeft == 0 {
		s.rekeyRetired = nil
	}
	s.rekeyMu.Unlock()
	for i := range retired {
		e := retired[i]
		s.publish(entity.Event{Kind: entity.EntityDelete, Entity: &e, DeleteReason: entity.ReasonRekeyed})
	}
}

func dedupeEntities(in []entity.Entity) []entity.Entity {
	seen := make(map[string]bool, len(in))
	out := in[:0]
	for _, e := range in {
		k := entityIdentity(e)
		if !seen[k] {
			seen[k] = true
			out = append(out, e)
		}
	}
	return out
}
