package winservices

import (
	"sync"

	"senhub-agent.go/internal/agent/services/entity"
)

// Entity rail: the host's Windows service-control surface is reported as a
// service.instance entity, and each selected Windows service is additionally
// reported as a host.service entity. The service.instance entity anchors the
// probe-level up/check-result series to the host; the host.service entities
// describe the individual services supervised by SCM.
//
// The probe-level id is winservices@<host.id>. It used to be the constant
// "winservices://localhost", which contains no host component at all and was
// therefore byte-identical on every machine.
const (
	entityTypeServiceInstance = "service.instance"
	entityTypeHost            = "host"
	idKeyServiceInstanceID    = "service.instance.id"
	idKeyHost                 = "host.id"
	serviceName               = "winservices"
	legacyServiceInstanceID   = "winservices://localhost"
	relRunsOn                 = "runs_on"

	hostServiceManagerWindows = "windows_scm"
)

func buildServiceInstanceID(hostID string) string {
	return serviceName + "@" + hostID
}

func windowsServiceStateString(state int) string {
	switch state {
	case stateStopped:
		return "stopped"
	case stateStartPending:
		return "start_pending"
	case stateStopPending:
		return "stop_pending"
	case stateRunning:
		return "running"
	case stateContinuePending:
		return "continue_pending"
	case statePausePending:
		return "pause_pending"
	case statePaused:
		return "paused"
	default:
		return "unknown"
	}
}

func startModeForStartType(startType int) (string, bool) {
	switch startType {
	case startBoot:
		return "boot_start", true
	case startSystem:
		return "system_start", true
	case startAutomatic, startAutomaticDelayed:
		return "auto_start", true
	case startManual:
		return "demand_start", true
	case startDisabled:
		return "disabled", true
	default:
		return "", false
	}
}

func buildHostServiceEntity(hostID, hostName string, s serviceState) (entity.Entity, entity.Relation) {
	id := map[string]any{
		"host.id":           hostID,
		"host.service.name": s.name,
	}

	attrs := map[string]any{
		"host.service.manager": hostServiceManagerWindows,
	}
	if hostName != "" {
		attrs["host.name"] = hostName
	}
	if startMode, ok := startModeForStartType(s.startType); ok {
		attrs["host.service.start_mode"] = startMode
		attrs["host.service.start_delayed"] = s.startDelayed
	}
	if s.triggerStart != nil {
		attrs["host.service.trigger_start"] = *s.triggerStart
	}

	return entity.Entity{
			Type:       entity.TypeHostService,
			ID:         id,
			Attributes: attrs,
		}, entity.Relation{
			Type:     relRunsOn,
			FromType: entity.TypeHostService,
			FromID:   id,
			ToType:   entityTypeHost,
			ToID:     map[string]any{idKeyHost: hostID},
		}
}

// winServicesEntitySource feeds the entity rail with the probe-level
// service.instance entity, the per-service host.service entities, and their
// runs_on → host relations.
type winServicesEntitySource struct {
	// rekey retires the pre-#742 constant service.instance identity; built on
	// first use, once the host id is known.
	rekey *entity.RekeyAnnouncer

	mu       sync.RWMutex
	hostID   string
	hostName string
	services []serviceState
}

func newEntitySource() *winServicesEntitySource {
	return &winServicesEntitySource{}
}

// setHostID stores the host's stable identity so Observe can attach the
// runs_on → host relation. Called once per Collect cycle; safe for concurrent
// use with Observe.
func (s *winServicesEntitySource) setHostID(id string) {
	s.mu.Lock()
	s.hostID = id
	s.mu.Unlock()
}

// setServices stores the selected service snapshots after a successful
// collection. It also updates the host identity and host name used to build
// host.service entities and their runs_on relations.
func (s *winServicesEntitySource) setServices(hostID, hostName string, services []serviceState) {
	s.mu.Lock()
	s.hostID = hostID
	if hostName != "" {
		s.hostName = hostName
	}
	s.services = append(s.services[:0], services...)
	s.mu.Unlock()
}

// Observe returns the service.instance entity, one host.service entity per
// selected service, and the runs_on → host relations. ok is false while the
// host id is unknown.
func (s *winServicesEntitySource) Observe() (entity.Observation, bool) {
	s.mu.RLock()
	hostID := s.hostID
	hostName := s.hostName
	services := append([]serviceState(nil), s.services...)
	s.mu.RUnlock()

	if hostID == "" {
		return entity.Observation{}, false
	}

	obs := entity.Observation{}

	instanceID := buildServiceInstanceID(hostID)
	serviceID := map[string]any{idKeyServiceInstanceID: instanceID}
	obs.Entities = append(obs.Entities, entity.Entity{
		Type:       entityTypeServiceInstance,
		ID:         serviceID,
		Attributes: map[string]any{"service.name": "winservices"},
	})
	obs.Relations = append(obs.Relations, entity.Relation{
		Type:     relRunsOn,
		FromType: entityTypeServiceInstance,
		FromID:   serviceID,
		ToType:   entityTypeHost,
		ToID:     map[string]any{idKeyHost: hostID},
	})

	for _, svc := range services {
		ent, rel := buildHostServiceEntity(hostID, hostName, svc)
		obs.Entities = append(obs.Entities, ent)
		obs.Relations = append(obs.Relations, rel)
	}

	// Retire the collapsed probe-level node explicitly rather than letting it
	// expire, so the consumer reads "someone decided" instead of "the producer
	// went quiet".
	s.mu.Lock()
	if s.rekey == nil {
		s.rekey = entity.NewRekeyAnnouncer(entityTypeServiceInstance,
			idKeyServiceInstanceID, legacyServiceInstanceID, instanceID)
	}
	rekey := s.rekey
	s.mu.Unlock()

	rekey.Announce()
	if rel, ok := rekey.SameAs(); ok {
		obs.Relations = append(obs.Relations, rel)
	}

	return obs, true
}

// serviceInstanceID returns the probe-level service.instance id while the
// host id is known and no entity is emitted otherwise.
func (s *winServicesEntitySource) serviceInstanceID() string {
	s.mu.RLock()
	hostID := s.hostID
	s.mu.RUnlock()
	if hostID == "" {
		return ""
	}
	return buildServiceInstanceID(hostID)
}
