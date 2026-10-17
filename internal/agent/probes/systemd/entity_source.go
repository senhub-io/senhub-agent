//go:build linux

package systemd

import (
	"senhub-agent.go/internal/agent/probes/hostservice"
	"sync"

	"senhub-agent.go/internal/agent/services/entity"
)

// systemdEntitySource emits one host.service entity per supervised unit.
// Its identity is {host.id, host.service.name}; the runs_on relation attaches
// each entity to the host.
//
// The entity cache is rebuilt on every Observe() call from the probe's last
// collected unit list. The probe poll cycle drives the refresh; there is no
// separate sweep goroutine.
type systemdEntitySource struct {
	hostname string

	mu     sync.Mutex
	units  []hostServiceUnit // unit snapshots from the last Collect cycle
	hostID string            // stable host.id used in runs_on relations
	rekey  map[string]*entity.RekeyAnnouncer
	ready  bool
}

func newEntitySource(hostname string) *systemdEntitySource {
	return &systemdEntitySource{hostname: hostname}
}

// setUnits is called by Collect() after each successful D-Bus query.
// hostID is the stable host identity (machine-id / UUID). hostName is the
// agent's canonical host name (operator override included), so a unit's
// host.name matches the host entity's.
func (s *systemdEntitySource) setUnits(units []hostServiceUnit, hostID, hostName string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.units = append(s.units[:0], units...)
	s.hostID = hostID
	if hostName != "" {
		s.hostname = hostName
	}
	if s.rekey == nil {
		s.rekey = make(map[string]*entity.RekeyAnnouncer)
	}
	if hostID != "" {
		for _, u := range units {
			if s.rekey[u.Name] != nil {
				continue
			}
			legacyID := map[string]any{
				"service.instance.id": legacyServiceInstanceID(hostID, u.Name),
			}
			newID := map[string]any{
				"host.id":           hostID,
				hostservice.KeyName: u.Name,
			}
			s.rekey[u.Name] = entity.NewRekeyAnnouncerWithIDs(
				"service.instance",
				legacyID,
				entity.TypeHostService,
				newID,
			)
		}
	}
	s.ready = true
}

// Observe returns the last cached entity snapshot. Non-blocking.
func (s *systemdEntitySource) Observe() (entity.Observation, bool) {
	s.mu.Lock()
	if !s.ready {
		s.mu.Unlock()
		return entity.Observation{}, false
	}
	// The identity embeds host.id, so without it there is no identifiable key
	// and no entity.
	if s.hostID == "" {
		s.mu.Unlock()
		return entity.Observation{}, true
	}

	units := append([]hostServiceUnit(nil), s.units...)
	hostID := s.hostID
	hostname := s.hostname
	rekeys := make(map[string]*entity.RekeyAnnouncer, len(s.rekey))
	for k, v := range s.rekey {
		rekeys[k] = v
	}
	s.mu.Unlock()

	obs := entity.Observation{}
	for _, u := range units {
		ent, rel := buildHostServiceEntity(hostID, hostname, u)
		obs.Entities = append(obs.Entities, ent)
		obs.Relations = append(obs.Relations, rel)

		ann := rekeys[u.Name]
		if ann == nil {
			continue
		}
		ann.Announce()
		if alias, ok := ann.SameAs(); ok {
			obs.Relations = append(obs.Relations, alias)
		}
	}
	return obs, true
}
