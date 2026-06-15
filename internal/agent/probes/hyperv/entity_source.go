// Package hyperv — entity rail.
//
// ADR 0018 / Toise frozen contract: host.id is ALWAYS a machine-id, NEVER
// a vmid or hypervisor slot id. A VM observed only from the hypervisor
// therefore becomes a "compute.vm" entity (the hypervisor allocation slot)
// whose identity is a composite {host.id: <hypervisor machine-id>, vmid:
// <hypervisor vm GUID>}. Toise registers the compute.vm + runs_on edge on
// its side from the state events this source emits.
//
// Per VM (in priority order):
//   - BEST-EFFORT host: if a guest machine-id is available via Hyper-V KVP
//     data exchange, emit a real host entity (reconciles with an agent
//     running inside the VM) + a runs_on edge.
//   - ELSE (common case) emit a compute.vm entity keyed by
//     {host.id: <hypervisor machine-id>, vmid: <VM GUID>} + runs_on.
//   - monitors: agent service.instance → the VM entity.
//
// The pure topology logic (buildHypervObservation) is cross-platform and
// fully testable without WMI by injecting synthetic vmInfo rows.
package hyperv

import (
	"sync"

	"senhub-agent.go/internal/agent/services/agentstate"
	"senhub-agent.go/internal/agent/services/entity"
	"senhub-agent.go/internal/agent/services/logger"
)

// vmInfo is the normalised per-VM row the WMI collector produces, stripped of
// WMI types so the entity logic is pure and platform-independent.
type vmInfo struct {
	// GUID is the Msvm_ComputerSystem.Name (the hypervisor's immutable VM id).
	GUID string
	// VMName is the human-readable ElementName from Msvm_SummaryInformation.
	VMName string
	// State is the OTel-normalised state string ("running", "stopped", "paused", …).
	State string
}

// hypervEntitySource feeds the entity rail for the hyperv probe. It holds the
// last good snapshot of VMs and updates it after every successful WMI
// collection cycle. Observe is called from the detector goroutine (never
// blocks); the cache is refreshed by update() from the probe's Collect path.
type hypervEntitySource struct {
	hostID       string
	moduleLogger *logger.ModuleLogger

	// resolveGuestMachineID queries Hyper-V KVP data exchange for the guest's
	// machine-id. It is a field so tests can inject a stub. The production
	// implementation lives in entity_source_windows.go / _stub.go.
	resolveGuestMachineID func(vmGUID string) string

	mu      sync.Mutex
	cache   entity.Observation
	hasData bool // true after the first successful update
}

// newHypervEntitySource constructs the entity source. hostID is the
// hypervisor's own machine-id (common.GetHostIdentity().ID).
func newHypervEntitySource(hostID string, log *logger.ModuleLogger) *hypervEntitySource {
	return &hypervEntitySource{
		hostID:                hostID,
		moduleLogger:          log,
		resolveGuestMachineID: kvpGuestMachineID,
	}
}

// Observe returns the last cached observation. ok=false before the first
// successful update (nothing to report yet is not "everything deleted").
func (s *hypervEntitySource) Observe() (entity.Observation, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.cache, s.hasData
}

// update rebuilds the cached observation from the latest VM list. Called from
// the probe's Collect path after a successful WMI query.
func (s *hypervEntitySource) update(vms []vmInfo) {
	agentID := agentstate.GetAgentInstanceID()
	obs := buildHypervObservation(vms, s.hostID, s.resolveGuestMachineID, agentID)

	s.mu.Lock()
	s.cache = obs
	s.hasData = true
	s.mu.Unlock()
}

// buildHypervObservation is the pure, cross-platform topology builder. It is
// separated from the struct so it can be tested without any WMI dependency by
// injecting synthetic vmInfo slices, a stub hostID and a stub guest-id
// resolver.
//
// For each VM, the builder applies the priority rule:
//  1. If resolveGuest returns a non-empty machine-id for the VM's GUID, emit a
//     real host entity (guest-reconcilable) + runs_on(host→hypervisor host).
//  2. Otherwise emit a compute.vm entity keyed by {host.id, vmid} +
//     runs_on(compute.vm→hypervisor host).
//
// In both cases, a monitors edge is appended if agentID is non-empty.
func buildHypervObservation(
	vms []vmInfo,
	hypervHostID string,
	resolveGuest func(vmGUID string) string,
	agentID string,
) entity.Observation {
	if hypervHostID == "" {
		// Without the hypervisor machine-id we cannot build a valid identity
		// for compute.vm (both id fields would be empty). Return empty so the
		// detector keeps the previous good snapshot (ok=true means "I see
		// nothing now, delete what I had" — do not use that here).
		return entity.Observation{}
	}

	hypervisorHostID := map[string]any{"host.id": hypervHostID}

	obs := entity.Observation{}

	for _, vm := range vms {
		if vm.GUID == "" {
			continue
		}

		guestMachineID := resolveGuest(vm.GUID)

		var vmEntityType string
		var vmEntityID map[string]any
		var vmEntityAttrs map[string]any

		if guestMachineID != "" {
			// Case 1: guest machine-id known — emit a real host entity that
			// reconciles with an agent running inside the VM.
			vmEntityType = "host"
			vmEntityID = map[string]any{"host.id": guestMachineID}
			vmEntityAttrs = map[string]any{
				"host.type": "vm",
			}
			if vm.VMName != "" {
				vmEntityAttrs["host.name"] = vm.VMName
			}
		} else {
			// Case 2 (common): no guest machine-id — emit a compute.vm entity
			// whose identity is {hypervisor host.id, hypervisor vmid}.
			vmEntityType = "compute.vm"
			vmEntityID = map[string]any{
				"host.id": hypervHostID,
				"vmid":    vm.GUID,
			}
			vmEntityAttrs = map[string]any{}
			if vm.VMName != "" {
				vmEntityAttrs["vm.name"] = vm.VMName
			}
			if vm.State != "" {
				vmEntityAttrs["vm.state"] = vm.State
			}
		}

		obs.Entities = append(obs.Entities, entity.Entity{
			Type:       vmEntityType,
			ID:         vmEntityID,
			Attributes: vmEntityAttrs,
		})

		// runs_on: the VM entity runs on the hypervisor host.
		obs.Relations = append(obs.Relations, entity.Relation{
			Type:     "runs_on",
			FromType: vmEntityType,
			FromID:   vmEntityID,
			ToType:   "host",
			ToID:     hypervisorHostID,
		})

		// monitors: agent service.instance → the VM entity.
		if agentID != "" {
			obs.Relations = append(obs.Relations, entity.Relation{
				Type:     "monitors",
				FromType: "service.instance",
				FromID:   map[string]any{"service.instance.id": agentID},
				ToType:   vmEntityType,
				ToID:     vmEntityID,
			})
		}
	}

	return obs
}
