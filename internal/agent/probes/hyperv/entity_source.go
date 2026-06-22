// Package hyperv — entity rail.
//
// ADR 0018 / Toise frozen contract: host.id is ALWAYS a machine-id, NEVER
// a vmid or hypervisor slot id. A VM observed only from the hypervisor
// therefore becomes a "compute.vm" entity (the hypervisor allocation slot)
// whose identity is a composite {host.id: <hypervisor machine-id>, vmid:
// <hypervisor vm GUID>}. Toise registers the compute.vm + runs_on edge on
// its side from the state events this source emits.
//
// Per VM: emit a compute.vm entity keyed {host.id: <hypervisor machine-id>,
// vmid: <VM GUID>}, runs_on the hypervisor host, with a monitors edge from the
// agent and the power state on the `status` stateKey. When Hyper-V KVP surfaces
// the guest's machine-id, carry it as the descriptive `guest.host.id` evidence
// (the join key the future ADR 0020 same_as overlay consumes). The hypervisor
// NEVER mints the in-guest host facet — the two are reconciled by same_as, never
// merged.
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
	// VCPU is the configured logical CPU count (Msvm_ComputerSystem
	// NumberOfProcessors); 0 → omitted.
	VCPU int64
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

// vmPowerStatus maps the probe's normalized VM state to the Toise compute.vm
// power-state vocabulary (running / stopped / suspended). It is emitted under
// the "status" key — one of the recognized stateKeys (ADR 0006) — so a VM
// powering off classifies as entity.state_changed in the causal timeline, not a
// silent attribute update. Hyper-V "paused" / "saved" both map to "suspended".
func vmPowerStatus(state string) string {
	switch state {
	case "":
		return ""
	case "running":
		return "running"
	case "stopped":
		return "stopped"
	default: // paused, saved, suspended, unknown → suspended
		return "suspended"
	}
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

		// A VM seen from the hypervisor is ALWAYS a compute.vm (identity
		// {hypervisor host.id, vmid}); the hypervisor never mints the in-guest
		// host facet (ADR 0020: never merge). When Hyper-V KVP surfaces the
		// guest's machine-id, carry it as the descriptive `guest.host.id`
		// evidence — the join key the future same_as overlay will consume to
		// reconcile this compute.vm with the in-guest host{machine-id}, exactly
		// as redfish/ibmi carry hw.serial_number on both facets. Producer-
		// asserted same_as is the ratified target, but its relation type is not
		// registered yet (ADR 0020 grafts on later), so we emit the evidence
		// attribute, not the edge.
		vmEntityType := "compute.vm"
		vmEntityID := map[string]any{
			"host.id": hypervHostID,
			"vmid":    vm.GUID,
		}
		vmEntityAttrs := map[string]any{
			// The VM's hypervisor platform — reuse the AT11 host.virtualization
			// vocabulary (Toise Q2): a Hyper-V guest is "hyperv".
			"host.virtualization": "hyperv",
		}
		if vm.VMName != "" {
			vmEntityAttrs["host.name"] = vm.VMName // VM display name → host.name (Toise Q2)
		}
		if vm.VCPU > 0 {
			vmEntityAttrs["host.cpu.logical.count"] = vm.VCPU // configured vCPUs (AT10 key)
		}
		if st := vmPowerStatus(vm.State); st != "" {
			vmEntityAttrs["status"] = st
		}
		if guestMachineID := resolveGuest(vm.GUID); guestMachineID != "" {
			vmEntityAttrs["guest.host.id"] = guestMachineID
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
