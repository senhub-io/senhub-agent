package entity

import (
	"testing"
)

func TestDetectFoundation(t *testing.T) {
	h := HostIdentity{
		ID: "h-001", Name: "web-server-1", OSType: "linux",
		Arch: "amd64", OSVersion: "22.04", OSDescription: "ubuntu 22.04",
	}
	a := AgentIdentity{InstanceID: "agent-7f3a", ServiceName: "senhub-agent", ServiceVersion: "1.0.0"}

	obs := DetectFoundation(h, a)
	if len(obs.Entities) != 2 {
		t.Fatalf("got %d entities, want 2 (host + service.instance)", len(obs.Entities))
	}
	if len(obs.Relations) != 1 {
		t.Fatalf("got %d relations, want 1 (runs_on)", len(obs.Relations))
	}

	host := obs.Entities[0]
	if host.Type != "host" || host.ID["host.id"] != "h-001" {
		t.Errorf("entity[0] = %+v, want host h-001", host)
	}
	if host.Attributes["host.name"] != "web-server-1" || host.Attributes["os.type"] != "linux" {
		t.Errorf("host attributes = %v", host.Attributes)
	}
	if host.Attributes["host.arch"] != "amd64" || host.Attributes["os.version"] != "22.04" ||
		host.Attributes["os.description"] != "ubuntu 22.04" {
		t.Errorf("host descriptive attributes missing/wrong: %v", host.Attributes)
	}

	svc := obs.Entities[1]
	if svc.Type != "service.instance" || svc.ID["service.instance.id"] != "agent-7f3a" {
		t.Errorf("entity[1] = %+v, want service.instance agent-7f3a", svc)
	}

	// runs_on is service.instance → host: its source endpoint is the
	// service.instance entity, so the detector folds it onto svc.
	runsOn := obs.Relations[0]
	if runsOn.Type != "runs_on" {
		t.Errorf("relation type = %q, want runs_on", runsOn.Type)
	}
	if runsOn.FromType != "service.instance" || runsOn.FromID["service.instance.id"] != "agent-7f3a" {
		t.Errorf("runs_on source = %s %v, want service.instance agent-7f3a", runsOn.FromType, runsOn.FromID)
	}
	if runsOn.ToType != "host" || runsOn.ToID["host.id"] != "h-001" {
		t.Errorf("runs_on target = %s %v, want host h-001", runsOn.ToType, runsOn.ToID)
	}
}

// TestDetectFoundation_Nameplate pins that the host nameplate attributes ride
// the host entity when present and are omitted when empty.
func TestDetectFoundation_Nameplate(t *testing.T) {
	h := HostIdentity{
		ID: "h-002", OSType: "linux", Arch: "amd64",
		OSName: "Ubuntu", OSVersion: "22.04", OSBuildID: "5.15.0-105",
		CPUModel: "Intel(R) Xeon(R) Silver 4310", CPUVendor: "GenuineIntel",
		HWVendor: "Dell Inc.", HWModel: "PowerEdge R750", HWSerial: "CZ12345",
	}
	a := AgentIdentity{InstanceID: "agent-1"}

	host := DetectFoundation(h, a).Entities[0]
	want := map[string]string{
		"os.name": "Ubuntu", "os.build_id": "5.15.0-105",
		"host.cpu.model.name": "Intel(R) Xeon(R) Silver 4310",
		"host.cpu.vendor.id":  "GenuineIntel",
		"hw.vendor":           "Dell Inc.",
		"hw.model":            "PowerEdge R750",
		"hw.serial_number":    "CZ12345",
	}
	for k, v := range want {
		if got := host.Attributes[k]; got != v {
			t.Errorf("host.Attributes[%q] = %v, want %v", k, got, v)
		}
	}

	// Empty nameplate fields must not appear as empty-string attributes.
	bare := DetectFoundation(HostIdentity{ID: "h-003"}, a).Entities[0]
	for _, k := range []string{"os.name", "host.cpu.model.name", "hw.vendor", "hw.serial_number"} {
		if _, present := bare.Attributes[k]; present {
			t.Errorf("attribute %q must be omitted when empty", k)
		}
	}
}

// TestDetectFoundation_Governance pins that operator governance attributes ride
// the host entity when provided.
func TestDetectFoundation_Governance(t *testing.T) {
	h := HostIdentity{ID: "h-1", Governance: map[string]any{
		"entity.owner.team":    "ops",
		"service.criticality":  "high",
		"entity.location.rack": "R12",
	}}
	host := DetectFoundation(h, AgentIdentity{InstanceID: "a"}).Entities[0]
	if host.Attributes["entity.owner.team"] != "ops" ||
		host.Attributes["service.criticality"] != "high" ||
		host.Attributes["entity.location.rack"] != "R12" {
		t.Errorf("governance not stamped on the host entity: %v", host.Attributes)
	}

	// Nil governance must not break the host emission.
	bare := DetectFoundation(HostIdentity{ID: "h-2"}, AgentIdentity{InstanceID: "a"}).Entities[0]
	if _, present := bare.Attributes["entity.owner.team"]; present {
		t.Error("no governance configured → no governance attributes")
	}
}

// TestDetectFoundation_CapacityVirtChassis pins the AT10-AT12 host attributes:
// numeric capacity rides as int64, virtualization/chassis as strings, all
// omitted when zero/empty.
func TestDetectFoundation_CapacityVirtChassis(t *testing.T) {
	h := HostIdentity{
		ID: "h-9", CPULogicalCount: 48, CPUPhysicalCount: 24, CPUFreqHz: 2100000000,
		MemTotal: 137438953472, DiskTotal: 1920383410176,
		Virtualization: "kvm", ChassisType: "vm",
	}
	host := DetectFoundation(h, AgentIdentity{InstanceID: "a"}).Entities[0]

	if host.Attributes["host.cpu.logical.count"] != int64(48) ||
		host.Attributes["host.cpu.physical.count"] != int64(24) ||
		host.Attributes["host.cpu.frequency.nominal"] != int64(2100000000) {
		t.Errorf("cpu capacity wrong: %v", host.Attributes)
	}
	if host.Attributes["host.memory.total"] != int64(137438953472) ||
		host.Attributes["host.disk.total"] != int64(1920383410176) {
		t.Errorf("mem/disk total wrong: %v", host.Attributes)
	}
	if host.Attributes["host.virtualization"] != "kvm" || host.Attributes["host.chassis.type"] != "vm" {
		t.Errorf("virt/chassis wrong: %v", host.Attributes)
	}

	bare := DetectFoundation(HostIdentity{ID: "h-0"}, AgentIdentity{InstanceID: "a"}).Entities[0]
	for _, k := range []string{"host.cpu.logical.count", "host.memory.total", "host.virtualization", "host.chassis.type"} {
		if _, present := bare.Attributes[k]; present {
			t.Errorf("attribute %q must be omitted when zero/empty", k)
		}
	}
}

// TestDetectFoundation_CloudContainerK8s pins the #536 nameplate attributes:
// cloud.provider/cloud.region/cloud.availability_zone/cloud.account.id,
// host.type, container.runtime and k8s.node.name ride the host entity when
// resolved and are omitted when empty.
func TestDetectFoundation_CloudContainerK8s(t *testing.T) {
	h := HostIdentity{
		ID: "h-c1", CloudProvider: "aws", CloudRegion: "eu-west-1",
		CloudAvailabilityZone: "eu-west-1a", CloudAccountID: "123456789012",
		HostType:         "t3.medium",
		ContainerRuntime: "containerd", K8sNodeName: "node-7",
	}
	host := DetectFoundation(h, AgentIdentity{InstanceID: "a"}).Entities[0]

	if host.Attributes["cloud.provider"] != "aws" || host.Attributes["cloud.region"] != "eu-west-1" {
		t.Errorf("cloud attrs wrong: %v", host.Attributes)
	}
	if host.Attributes["cloud.availability_zone"] != "eu-west-1a" {
		t.Errorf("cloud.availability_zone wrong: %v", host.Attributes)
	}
	if host.Attributes["cloud.account.id"] != "123456789012" {
		t.Errorf("cloud.account.id wrong: %v", host.Attributes)
	}
	if host.Attributes["host.type"] != "t3.medium" {
		t.Errorf("host.type wrong: %v", host.Attributes)
	}
	if host.Attributes["container.runtime"] != "containerd" {
		t.Errorf("container.runtime wrong: %v", host.Attributes)
	}
	if host.Attributes["k8s.node.name"] != "node-7" {
		t.Errorf("k8s.node.name wrong: %v", host.Attributes)
	}

	bare := DetectFoundation(HostIdentity{ID: "h-c0"}, AgentIdentity{InstanceID: "a"}).Entities[0]
	for _, k := range []string{
		"cloud.provider", "cloud.region", "cloud.availability_zone",
		"cloud.account.id", "host.type", "container.runtime", "k8s.node.name",
	} {
		if _, present := bare.Attributes[k]; present {
			t.Errorf("attribute %q must be omitted when empty", k)
		}
	}
}

// TestDetectFoundation_FoldEmbedsRunsOn pins that folding the foundation
// observation embeds runs_on on the service.instance entity (and nothing on
// the host), matching the Toise conformance fixture.
func TestDetectFoundation_FoldEmbedsRunsOn(t *testing.T) {
	h := HostIdentity{ID: "h-001"}
	a := AgentIdentity{InstanceID: "agent-7f3a"}

	entities, orphans := DetectFoundation(h, a).foldRelationships()
	if len(orphans) != 0 {
		t.Fatalf("got %d orphan relations, want 0", len(orphans))
	}
	byType := map[string]Entity{}
	for _, e := range entities {
		byType[e.Type] = e
	}
	if rels := byType["host"].Relationships; len(rels) != 0 {
		t.Errorf("host carries %d relationships, want 0", len(rels))
	}
	svcRels := byType["service.instance"].Relationships
	if len(svcRels) != 1 {
		t.Fatalf("service.instance carries %d relationships, want 1", len(svcRels))
	}
	r := svcRels[0]
	if r.Type != "runs_on" || r.TargetType != "host" || r.TargetID["host.id"] != "h-001" {
		t.Errorf("embedded relationship = %+v, want runs_on → host h-001", r)
	}
}
