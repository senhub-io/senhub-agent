package entity

// HostIdentity is the identity + descriptive facts of the machine the agent
// runs on. ID is the stable machine identifier (machine-id / UUID), not the
// hostname — the hostname is descriptive and may change.
type HostIdentity struct {
	ID            string // host.id — stable across rename/reboot
	Name          string // host.name — descriptive
	OSType        string // os.type — descriptive
	Arch          string // host.arch — descriptive
	OSName        string // os.name — descriptive
	OSVersion     string // os.version — descriptive
	OSBuildID     string // os.build_id — kernel/build, descriptive
	OSDescription string // os.description — descriptive
	CPUModel      string // host.cpu.model.name — nameplate
	CPUVendor     string // host.cpu.vendor.id — nameplate
	HWVendor      string // hw.vendor — DMI nameplate
	HWModel       string // hw.model — DMI nameplate
	HWSerial      string // hw.serial_number — DMI nameplate, same_as glue to a BMC facet

	CPULogicalCount  int64  // host.cpu.logical.count (AT10)
	CPUPhysicalCount int64  // host.cpu.physical.count (AT10)
	CPUFreqHz        int64  // host.cpu.frequency.nominal — Hz (AT10)
	MemTotal         int64  // host.memory.total — bytes (AT10)
	DiskTotal        int64  // host.disk.total — bytes (AT10)
	Virtualization   string // host.virtualization (AT11)
	ChassisType      string // host.chassis.type (AT12)

	CloudProvider         string // cloud.provider — IMDS, best-effort (#536)
	CloudRegion           string // cloud.region — IMDS, best-effort (#536)
	CloudAvailabilityZone string // cloud.availability_zone — IMDS, best-effort (#536)
	CloudAccountID        string // cloud.account.id — IMDS, best-effort (#536)
	HostType              string // host.type — cloud instance type, IMDS, best-effort (#536)
	ContainerRuntime      string // container.runtime — /proc heuristics (#536)
	K8sNodeName           string // k8s.node.name — downward-API NODE_NAME (#536)

	// Governance is the operator-supplied governance attribute map
	// (entity.owner.*, service.criticality, entity.location.*, …) stamped on the
	// host entity. nil/empty by default.
	Governance map[string]any
}

// AgentIdentity is the identity + descriptive facts of the agent process.
// InstanceID is the persisted agent key (not the pid, not the hostname).
type AgentIdentity struct {
	InstanceID     string // service.instance.id — persisted agent key
	ServiceName    string // service.name — descriptive
	ServiceVersion string // service.version — descriptive
}

// DetectFoundation builds the Lot 1 observation: the host the agent runs on,
// the agent's own service.instance, and the runs_on edge between them
// (service.instance → host). The detector stamps event_time and the liveness
// interval and folds runs_on onto the service.instance entity.
//
// It always returns the COMPLETE current descriptive attribute set per
// entity (entity.state is a full state, never a delta).
func DetectFoundation(h HostIdentity, a AgentIdentity) Observation {
	host := Entity{
		Type:       "host",
		ID:         map[string]any{"host.id": h.ID},
		Attributes: map[string]any{},
	}
	if h.Name != "" {
		host.Attributes["host.name"] = h.Name
	}
	if h.OSType != "" {
		host.Attributes["os.type"] = h.OSType
	}
	if h.Arch != "" {
		host.Attributes["host.arch"] = h.Arch
	}
	if h.OSName != "" {
		host.Attributes["os.name"] = h.OSName
	}
	if h.OSVersion != "" {
		host.Attributes["os.version"] = h.OSVersion
	}
	if h.OSBuildID != "" {
		host.Attributes["os.build_id"] = h.OSBuildID
	}
	if h.OSDescription != "" {
		host.Attributes["os.description"] = h.OSDescription
	}
	if h.CPUModel != "" {
		host.Attributes["host.cpu.model.name"] = h.CPUModel
	}
	if h.CPUVendor != "" {
		host.Attributes["host.cpu.vendor.id"] = h.CPUVendor
	}
	if h.HWVendor != "" {
		host.Attributes["hw.vendor"] = h.HWVendor
	}
	if h.HWModel != "" {
		host.Attributes["hw.model"] = h.HWModel
	}
	if h.HWSerial != "" {
		host.Attributes["hw.serial_number"] = h.HWSerial
	}
	if h.CPULogicalCount > 0 {
		host.Attributes["host.cpu.logical.count"] = h.CPULogicalCount
	}
	if h.CPUPhysicalCount > 0 {
		host.Attributes["host.cpu.physical.count"] = h.CPUPhysicalCount
	}
	if h.CPUFreqHz > 0 {
		host.Attributes["host.cpu.frequency.nominal"] = h.CPUFreqHz
	}
	if h.MemTotal > 0 {
		host.Attributes["host.memory.total"] = h.MemTotal
	}
	if h.DiskTotal > 0 {
		host.Attributes["host.disk.total"] = h.DiskTotal
	}
	if h.Virtualization != "" {
		host.Attributes["host.virtualization"] = h.Virtualization
	}
	if h.ChassisType != "" {
		host.Attributes["host.chassis.type"] = h.ChassisType
	}
	if h.CloudProvider != "" {
		host.Attributes["cloud.provider"] = h.CloudProvider
	}
	if h.CloudRegion != "" {
		host.Attributes["cloud.region"] = h.CloudRegion
	}
	if h.CloudAvailabilityZone != "" {
		host.Attributes["cloud.availability_zone"] = h.CloudAvailabilityZone
	}
	if h.CloudAccountID != "" {
		host.Attributes["cloud.account.id"] = h.CloudAccountID
	}
	if h.HostType != "" {
		host.Attributes["host.type"] = h.HostType
	}
	if h.ContainerRuntime != "" {
		host.Attributes["container.runtime"] = h.ContainerRuntime
	}
	if h.K8sNodeName != "" {
		host.Attributes["k8s.node.name"] = h.K8sNodeName
	}
	for k, v := range h.Governance {
		host.Attributes[k] = v
	}

	svc := Entity{
		Type:       "service.instance",
		ID:         map[string]any{"service.instance.id": a.InstanceID},
		Attributes: map[string]any{},
	}
	if a.ServiceName != "" {
		svc.Attributes["service.name"] = a.ServiceName
	}
	if a.ServiceVersion != "" {
		svc.Attributes["service.version"] = a.ServiceVersion
	}

	runsOn := Relation{
		Type:     "runs_on",
		FromType: "service.instance",
		FromID:   map[string]any{"service.instance.id": a.InstanceID},
		ToType:   "host",
		ToID:     map[string]any{"host.id": h.ID},
	}

	return Observation{
		Entities:  []Entity{host, svc},
		Relations: []Relation{runsOn},
	}
}
