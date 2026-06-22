package hyperv

import (
	"testing"

	"senhub-agent.go/internal/agent/services/entity"
)

// noGuestID is a stub resolver that always returns "" (the common case: no
// Integration Services or VM is off).
func noGuestID(_ string) string { return "" }

// stubGuestID returns a resolver that maps a GUID to a guest machine-id.
func stubGuestID(m map[string]string) func(string) string {
	return func(guid string) string { return m[guid] }
}

// findEntity returns the first entity in obs matching the given type, or nil.
func findEntity(obs entity.Observation, typ string) *entity.Entity {
	for i := range obs.Entities {
		if obs.Entities[i].Type == typ {
			return &obs.Entities[i]
		}
	}
	return nil
}

// findRelations returns all relations in obs with the given type.
func findRelations(obs entity.Observation, typ string) []entity.Relation {
	var out []entity.Relation
	for _, r := range obs.Relations {
		if r.Type == typ {
			out = append(out, r)
		}
	}
	return out
}

// TestBuildHypervObservation_EmptyHostID returns an empty observation so the
// detector keeps the previous snapshot rather than deleting everything.
func TestBuildHypervObservation_EmptyHostID(t *testing.T) {
	vms := []vmInfo{{GUID: "guid-1", VMName: "VM1", State: "running"}}
	obs := buildHypervObservation(vms, "", noGuestID, "")
	if len(obs.Entities) != 0 || len(obs.Relations) != 0 {
		t.Errorf("empty hostID must yield empty observation, got %+v", obs)
	}
}

// TestBuildHypervObservation_NoVMs returns an empty but valid observation —
// "I see nothing now" (ok=true signals deletion of stale VMs).
func TestBuildHypervObservation_NoVMs(t *testing.T) {
	obs := buildHypervObservation(nil, "host-uuid", noGuestID, "")
	if len(obs.Entities) != 0 || len(obs.Relations) != 0 {
		t.Errorf("no VMs must yield empty observation, got %+v", obs)
	}
}

// TestBuildHypervObservation_ComputeVM verifies the common case: no guest
// machine-id → compute.vm entity with {host.id, vmid} identity.
func TestBuildHypervObservation_ComputeVM(t *testing.T) {
	hostID := "hyperv-host-machine-id"
	vm := vmInfo{GUID: "vm-guid-abc", VMName: "TestVM", State: "running", VCPU: 4}
	obs := buildHypervObservation([]vmInfo{vm}, hostID, noGuestID, "")

	if len(obs.Entities) != 1 {
		t.Fatalf("expected 1 entity, got %d: %+v", len(obs.Entities), obs.Entities)
	}
	e := obs.Entities[0]
	if e.Type != "compute.vm" {
		t.Errorf("entity type: want compute.vm, got %q", e.Type)
	}
	if e.ID["host.id"] != hostID {
		t.Errorf("entity id host.id: want %q, got %q", hostID, e.ID["host.id"])
	}
	if e.ID["vmid"] != "vm-guid-abc" {
		t.Errorf("entity id vmid: want %q, got %q", "vm-guid-abc", e.ID["vmid"])
	}
	// The VM display name rides host.name (Toise Q2), not a vm.name attribute.
	if e.Attributes["host.name"] != "TestVM" {
		t.Errorf("host.name attribute: want TestVM, got %v", e.Attributes["host.name"])
	}
	if _, leaked := e.Attributes["vm.name"]; leaked {
		t.Errorf("vm.name must be replaced by host.name: %v", e.Attributes)
	}
	if e.Attributes["host.virtualization"] != "hyperv" {
		t.Errorf("host.virtualization: want hyperv, got %v", e.Attributes["host.virtualization"])
	}
	if e.Attributes["host.cpu.logical.count"] != int64(4) {
		t.Errorf("host.cpu.logical.count: want 4, got %v", e.Attributes["host.cpu.logical.count"])
	}
	// Power state rides the "status" stateKey (not a plain vm.state attribute),
	// so a VM powering off classifies as entity.state_changed.
	if e.Attributes["status"] != "running" {
		t.Errorf("status stateKey: want running, got %v", e.Attributes["status"])
	}
	if _, leaked := e.Attributes["vm.state"]; leaked {
		t.Errorf("vm.state must be replaced by the status stateKey: %v", e.Attributes)
	}
}

func TestVMPowerStatus(t *testing.T) {
	cases := map[string]string{
		"running": "running", "stopped": "stopped",
		"paused": "suspended", "saved": "suspended", "weird": "suspended", "": "",
	}
	for in, want := range cases {
		if got := vmPowerStatus(in); got != want {
			t.Errorf("vmPowerStatus(%q) = %q, want %q", in, got, want)
		}
	}
}

// TestBuildHypervObservation_ComputeVM_NoHostID_inID pins that host.id in the
// compute.vm identity is the hypervisor machine-id, NOT the vmid — verifying
// the ADR 0018 contract.
func TestBuildHypervObservation_ComputeVM_HostIDIsHypervisorMachineID(t *testing.T) {
	hostID := "hyperv-machine-id-XYZ"
	vm := vmInfo{GUID: "some-vm-guid", VMName: "MyVM"}
	obs := buildHypervObservation([]vmInfo{vm}, hostID, noGuestID, "")

	if len(obs.Entities) != 1 {
		t.Fatalf("expected 1 entity, got %d", len(obs.Entities))
	}
	e := obs.Entities[0]
	// host.id must be the HYPERVISOR machine-id, never the vmid.
	if e.ID["host.id"] == "some-vm-guid" {
		t.Errorf("host.id must be the hypervisor machine-id, not the vmid (ADR 0018)")
	}
	if e.ID["host.id"] != hostID {
		t.Errorf("host.id: want %q, got %q", hostID, e.ID["host.id"])
	}
}

// TestBuildHypervObservation_GuestMachineID verifies that when KVP surfaces the
// guest machine-id, the entity is STILL a compute.vm (the hypervisor never mints
// the in-guest host facet — ADR 0020 never-merge); the guest machine-id rides as
// the descriptive guest.host.id evidence (the future same_as join key).
func TestBuildHypervObservation_GuestMachineID(t *testing.T) {
	hostID := "hyperv-host-id"
	guestID := "guest-machine-id-123"
	vm := vmInfo{GUID: "vm-guid-1", VMName: "GuestVM", State: "running"}

	resolver := stubGuestID(map[string]string{"vm-guid-1": guestID})
	obs := buildHypervObservation([]vmInfo{vm}, hostID, resolver, "")

	if len(obs.Entities) != 1 {
		t.Fatalf("expected 1 entity, got %d: %+v", len(obs.Entities), obs.Entities)
	}
	e := obs.Entities[0]
	if e.Type != "compute.vm" {
		t.Errorf("entity type: want compute.vm (never a host facet), got %q", e.Type)
	}
	// Identity stays {hypervisor host.id, vmid} — the guest id is NOT the identity.
	if e.ID["host.id"] != hostID || e.ID["vmid"] != "vm-guid-1" {
		t.Errorf("identity must stay {hypervisor host.id, vmid}: %+v", e.ID)
	}
	if e.Attributes["guest.host.id"] != guestID {
		t.Errorf("guest.host.id evidence: want %q, got %v", guestID, e.Attributes["guest.host.id"])
	}
	if e.ID["host.id"] == guestID {
		t.Errorf("the guest machine-id must never become the identity host.id: %+v", e.ID)
	}
}

// TestBuildHypervObservation_RunsOnEdge verifies that a runs_on relation is
// emitted from the VM entity to the hypervisor host for both cases.
func TestBuildHypervObservation_RunsOnEdge(t *testing.T) {
	hostID := "hv-host-id"

	t.Run("compute.vm", func(t *testing.T) {
		vm := vmInfo{GUID: "guid-1", VMName: "VM1"}
		obs := buildHypervObservation([]vmInfo{vm}, hostID, noGuestID, "")

		runsOn := findRelations(obs, "runs_on")
		if len(runsOn) != 1 {
			t.Fatalf("expected 1 runs_on relation, got %d", len(runsOn))
		}
		r := runsOn[0]
		if r.FromType != "compute.vm" {
			t.Errorf("FromType: want compute.vm, got %q", r.FromType)
		}
		if r.ToType != "host" {
			t.Errorf("ToType: want host, got %q", r.ToType)
		}
		if r.ToID["host.id"] != hostID {
			t.Errorf("ToID host.id: want %q, got %q", hostID, r.ToID["host.id"])
		}
	})

	t.Run("compute.vm with guest machine-id known", func(t *testing.T) {
		guestID := "guest-id-99"
		vm := vmInfo{GUID: "guid-2", VMName: "GuestVM"}
		resolver := stubGuestID(map[string]string{"guid-2": guestID})
		obs := buildHypervObservation([]vmInfo{vm}, hostID, resolver, "")

		runsOn := findRelations(obs, "runs_on")
		if len(runsOn) != 1 {
			t.Fatalf("expected 1 runs_on relation, got %d", len(runsOn))
		}
		r := runsOn[0]
		// Still compute.vm --runs_on--> hypervisor host (no host facet minted).
		if r.FromType != "compute.vm" {
			t.Errorf("FromType: want compute.vm, got %q", r.FromType)
		}
		if r.FromID["vmid"] != "guid-2" || r.FromID["host.id"] != hostID {
			t.Errorf("FromID must be the compute.vm identity {hypervisor host.id, vmid}: %+v", r.FromID)
		}
		if r.ToID["host.id"] != hostID {
			t.Errorf("ToID host.id: want hypervisor id %q, got %q", hostID, r.ToID["host.id"])
		}
	})
}

// TestBuildHypervObservation_MonitorsEdge verifies that a monitors relation
// from service.instance → VM entity is emitted when agentID is non-empty.
func TestBuildHypervObservation_MonitorsEdge(t *testing.T) {
	hostID := "hv-host"
	agentID := "agent-instance-id-xyz"
	vm := vmInfo{GUID: "guid-m", VMName: "MonVM"}
	obs := buildHypervObservation([]vmInfo{vm}, hostID, noGuestID, agentID)

	monitors := findRelations(obs, "monitors")
	if len(monitors) != 1 {
		t.Fatalf("expected 1 monitors relation, got %d", len(monitors))
	}
	r := monitors[0]
	if r.FromType != "service.instance" {
		t.Errorf("FromType: want service.instance, got %q", r.FromType)
	}
	if r.FromID["service.instance.id"] != agentID {
		t.Errorf("FromID service.instance.id: want %q, got %q", agentID, r.FromID["service.instance.id"])
	}
	if r.ToType != "compute.vm" {
		t.Errorf("ToType: want compute.vm, got %q", r.ToType)
	}
}

// TestBuildHypervObservation_NoMonitorsEdge_WhenNoAgentID pins that the
// monitors edge is omitted when agentID is empty (entity emission disabled).
func TestBuildHypervObservation_NoMonitorsEdge_WhenNoAgentID(t *testing.T) {
	vm := vmInfo{GUID: "guid-x", VMName: "X"}
	obs := buildHypervObservation([]vmInfo{vm}, "hv-host", noGuestID, "")

	for _, r := range obs.Relations {
		if r.Type == "monitors" {
			t.Errorf("monitors edge must not be emitted when agentID is empty, got %+v", r)
		}
	}
}

// TestBuildHypervObservation_MultipleVMs verifies entity + relation counts
// scale correctly with multiple VMs.
func TestBuildHypervObservation_MultipleVMs(t *testing.T) {
	hostID := "hv-host"
	agentID := "agent-1"
	vms := []vmInfo{
		{GUID: "guid-a", VMName: "A", State: "running"},
		{GUID: "guid-b", VMName: "B", State: "stopped"},
		{GUID: "guid-c", VMName: "C", State: "paused"},
	}
	obs := buildHypervObservation(vms, hostID, noGuestID, agentID)

	if len(obs.Entities) != 3 {
		t.Errorf("expected 3 entities, got %d", len(obs.Entities))
	}
	runsOn := findRelations(obs, "runs_on")
	if len(runsOn) != 3 {
		t.Errorf("expected 3 runs_on relations, got %d", len(runsOn))
	}
	monitors := findRelations(obs, "monitors")
	if len(monitors) != 3 {
		t.Errorf("expected 3 monitors relations, got %d", len(monitors))
	}
}

// TestBuildHypervObservation_SkipsEmptyGUID pins that VMs with an empty GUID
// are skipped — an empty GUID cannot anchor a stable identity.
func TestBuildHypervObservation_SkipsEmptyGUID(t *testing.T) {
	vms := []vmInfo{
		{GUID: "", VMName: "Phantom"},
		{GUID: "real-guid", VMName: "Real"},
	}
	obs := buildHypervObservation(vms, "hv-host", noGuestID, "")
	if len(obs.Entities) != 1 {
		t.Errorf("expected 1 entity (empty GUID skipped), got %d", len(obs.Entities))
	}
}

// TestHypervEntitySource_ObserveBeforeUpdate verifies ok=false before the
// first successful update.
func TestHypervEntitySource_ObserveBeforeUpdate(t *testing.T) {
	src := newHypervEntitySource("hv-host", nil)
	src.resolveGuestMachineID = noGuestID

	obs, ok := src.Observe()
	if ok {
		t.Error("Observe must return ok=false before the first update")
	}
	if len(obs.Entities) != 0 {
		t.Errorf("expected empty observation before update, got %+v", obs)
	}
}

// TestHypervEntitySource_ObserveAfterUpdate verifies ok=true and correct
// entity count after update.
func TestHypervEntitySource_ObserveAfterUpdate(t *testing.T) {
	src := newHypervEntitySource("hv-host-id", nil)
	src.resolveGuestMachineID = noGuestID

	src.update([]vmInfo{
		{GUID: "guid-1", VMName: "VM1", State: "running"},
		{GUID: "guid-2", VMName: "VM2", State: "stopped"},
	})

	obs, ok := src.Observe()
	if !ok {
		t.Error("Observe must return ok=true after a successful update")
	}
	if len(obs.Entities) != 2 {
		t.Errorf("expected 2 entities, got %d", len(obs.Entities))
	}
}
