package common

import (
	"fmt"
	"strings"
	"sync"

	"github.com/shirou/gopsutil/v3/cpu"
	"github.com/shirou/gopsutil/v3/disk"
	"github.com/shirou/gopsutil/v3/host"
	"github.com/shirou/gopsutil/v3/mem"
	"senhub-agent.go/internal/agent/tags"
)

// HostIdentity is the stable identity plus descriptive facts of the machine
// the agent runs on. ID is the machine-id / UUID (stable across rename and
// reboot) — the identifying attribute for the host entity. The rest are
// descriptive nameplate facts mirroring the OTel host.* / os.* / hw.* keys.
type HostIdentity struct {
	ID            string
	Name          string
	OSType        string
	Arch          string // host.arch — KernelArch
	OSName        string // os.name — Platform
	OSVersion     string // os.version — PlatformVersion
	OSBuildID     string // os.build_id — KernelVersion
	OSDescription string // os.description — Platform + PlatformVersion
	CPUModel      string // host.cpu.model.name
	CPUVendor     string // host.cpu.vendor.id
	HWVendor      string // hw.vendor — DMI system vendor
	HWModel       string // hw.model — DMI product name
	HWSerial      string // hw.serial_number — DMI product serial (same_as glue to a BMC facet)

	// Capacity nameplate (AT10) and substrate (AT11/AT12). 0/"" → omitted.
	CPULogicalCount  int64  // host.cpu.logical.count
	CPUPhysicalCount int64  // host.cpu.physical.count
	CPUFreqHz        int64  // host.cpu.frequency.nominal — Hz
	MemTotal         int64  // host.memory.total — bytes
	DiskTotal        int64  // host.disk.total — bytes
	Virtualization   string // host.virtualization — none/kvm/vmware/…
	ChassisType      string // host.chassis.type — desktop/laptop/server/blade/vm/other

	// Cloud / container / orchestrator nameplate (#536). Best-effort, resolved
	// from cloud metadata (IMDS), /proc container heuristics, and the
	// downward-API env var; "" → omitted. Cached with the rest of the nameplate.
	CloudProvider    string // cloud.provider — aws/gcp/azure
	CloudRegion      string // cloud.region
	ContainerRuntime string // container.runtime — docker/containerd/lxc/podman
	K8sNodeName      string // k8s.node.name — downward-API NODE_NAME
}

// normalizeHostname lowercases the hostname and strips surrounding
// whitespace plus a trailing FQDN root dot, so the same machine yields an
// identical host label on every emission path (metric tags, OTLP resource,
// entity nameplate). Windows reports an UPPER-CASE NetBIOS computer name
// while DNS answers lower-case FQDNs; without one normalization point the
// metric↔entity join by host name silently breaks (#627).
func normalizeHostname(raw string) string {
	return strings.ToLower(strings.TrimSuffix(strings.TrimSpace(raw), "."))
}

// canonicalHostname is the single hostname the agent emits everywhere: the
// machine's fully-qualified DNS name when the platform provides one
// (Windows — see resolveHostFQDN), else the OS-reported hostname, both
// normalized to lower-case.
func canonicalHostname(raw string) string {
	if fqdn := resolveHostFQDN(); fqdn != "" {
		return normalizeHostname(fqdn)
	}
	return normalizeHostname(raw)
}

// GetHostIdentity returns the host's stable identity plus descriptive nameplate
// facts for entity detection. ID comes from gopsutil's HostID (the OS
// machine-id), which — unlike the hostname — does not change on rename, so it is
// safe to use as immutable entity identity. host.Info() is re-read each call so
// a hostname change is picked up; the static CPU/hardware nameplate is cached
// (see getHostNameplate) so the per-heartbeat reconcile stays cheap.
func GetHostIdentity() (HostIdentity, error) {
	hostInfo, err := host.Info()
	if err != nil {
		return HostIdentity{}, fmt.Errorf("error getting host info: %v", err)
	}
	virt := normalizeVirtualization(hostInfo.VirtualizationSystem, hostInfo.VirtualizationRole)
	if virt == "none" {
		if fb := readVirtualizationFallback(); fb != "" {
			virt = fb
		}
	}
	np := getHostNameplate(virt)
	return HostIdentity{
		ID:               hostInfo.HostID,
		Name:             canonicalHostname(hostInfo.Hostname),
		OSType:           hostInfo.OS,
		Arch:             hostInfo.KernelArch,
		OSName:           hostInfo.Platform,
		OSVersion:        hostInfo.PlatformVersion,
		OSBuildID:        hostInfo.KernelVersion,
		OSDescription:    strings.TrimSpace(hostInfo.Platform + " " + hostInfo.PlatformVersion),
		CPUModel:         np.cpuModel,
		CPUVendor:        np.cpuVendor,
		HWVendor:         np.hwVendor,
		HWModel:          np.hwModel,
		HWSerial:         np.hwSerial,
		CPULogicalCount:  np.cpuLogical,
		CPUPhysicalCount: np.cpuPhysical,
		CPUFreqHz:        np.cpuFreqHz,
		MemTotal:         np.memTotal,
		DiskTotal:        np.diskTotal,
		Virtualization:   virt,
		ChassisType:      chassisName(np.chassisCode, virt),
		CloudProvider:    np.cloudProvider,
		CloudRegion:      np.cloudRegion,
		ContainerRuntime: np.containerRuntime,
		K8sNodeName:      np.k8sNodeName,
	}, nil
}

// hostNameplate holds the host's static CPU and hardware identity plus capacity
// nameplate. These change rarely (cpu/ram/disk add, chassis), so they are
// gathered once at startup and refreshed only on agent restart.
type hostNameplate struct {
	cpuModel, cpuVendor         string
	hwVendor, hwModel, hwSerial string
	cpuLogical, cpuPhysical     int64
	cpuFreqHz                   int64
	memTotal, diskTotal         int64
	chassisCode                 int // raw SMBIOS chassis_type (Linux DMI), 0 = unknown

	cloudProvider, cloudRegion string // cloud.provider / cloud.region (IMDS, #536)
	containerRuntime           string // container.runtime (/proc heuristics, #536)
	k8sNodeName                string // k8s.node.name (downward-API env, #536)
}

// hardwareNameplate is the DMI / system-board identity of the host, read by the
// platform-specific readHardwareNameplate (sysfs on Linux; empty elsewhere).
type hardwareNameplate struct {
	vendor, model, serial string
}

var (
	nameplateOnce sync.Once
	nameplate     hostNameplate
)

// getHostNameplate gathers the host's static CPU + hardware nameplate exactly
// once (sync.Once), since it is immutable for the process lifetime. Keeping it
// off the per-heartbeat path avoids re-reading cpu.Info() and sysfs every
// reconcile cycle.
func getHostNameplate(virt string) hostNameplate {
	nameplateOnce.Do(func() {
		if infos, err := cpu.Info(); err == nil && len(infos) > 0 {
			nameplate.cpuModel = strings.TrimSpace(infos[0].ModelName)
			nameplate.cpuVendor = strings.TrimSpace(infos[0].VendorID)
			if mhz := infos[0].Mhz; mhz > 0 {
				nameplate.cpuFreqHz = int64(mhz * 1e6) // MHz → Hz
			}
		}
		if n, err := cpu.Counts(true); err == nil {
			nameplate.cpuLogical = int64(n)
		}
		if n, err := cpu.Counts(false); err == nil {
			nameplate.cpuPhysical = int64(n)
		}
		if vm, err := mem.VirtualMemory(); err == nil {
			nameplate.memTotal = int64(vm.Total)
		}
		nameplate.diskTotal = totalDiskBytes()
		nameplate.chassisCode = readChassisType()

		hw := readHardwareNameplate()
		nameplate.hwVendor = hw.vendor
		nameplate.hwModel = hw.model
		nameplate.hwSerial = hw.serial

		nameplate.containerRuntime = detectContainerRuntime()
		nameplate.k8sNodeName = detectK8sNodeName()
		// IMDS is a network call; only worth attempting when the host is a guest
		// (cloud VMs are virtualized). Bare metal skips it to avoid the timeout.
		if virt != "" && virt != "none" {
			nameplate.cloudProvider, nameplate.cloudRegion = detectCloud(defaultCloudTimeout)
		}
	})
	return nameplate
}

// totalDiskBytes sums the capacity of the host's distinct physical filesystems
// (deduped by backing device), the cross-platform proxy for host.disk.total.
func totalDiskBytes() int64 {
	parts, err := disk.Partitions(false)
	if err != nil {
		return 0
	}
	seen := map[string]bool{}
	var total int64
	for _, p := range parts {
		if p.Device == "" || seen[p.Device] {
			continue
		}
		seen[p.Device] = true
		if u, err := disk.Usage(p.Mountpoint); err == nil {
			total += int64(u.Total)
		}
	}
	return total
}

// normalizeVirtualization maps gopsutil's virtualization system/role to the
// AT11 host.virtualization enum. Only a guest is virtualized; a hypervisor host
// or bare metal is "none". An undetected/unknown guest system is "unknown".
func normalizeVirtualization(system, role string) string {
	if role != "guest" {
		return "none"
	}
	switch strings.ToLower(system) {
	case "kvm":
		return "kvm"
	case "vmware":
		return "vmware"
	case "xen":
		return "xen"
	case "hyperv", "microsoft", "hv":
		return "hyperv"
	case "vbox", "virtualbox", "oracle":
		return "virtualbox"
	case "qemu":
		return "qemu"
	case "lxc", "lxc-libvirt":
		return "lxc"
	case "openvz":
		return "openvz"
	case "bhyve":
		return "bhyve"
	default:
		return "unknown"
	}
}

// isDMIPlaceholder rejects the firmware default strings OEMs ship, so the
// nameplate never carries "To Be Filled By O.E.M." as a vendor/model/serial.
// Shared by the Linux sysfs reader and the Windows SMBIOS reader.
func isDMIPlaceholder(v string) bool {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "", "to be filled by o.e.m.", "to be filled by o.e.m",
		"system manufacturer", "system product name", "system serial number",
		"default string", "not specified", "not applicable",
		"none", "unknown", "n/a", "o.e.m.", "oem":
		return true
	}
	return false
}

// chassisName maps a raw SMBIOS chassis-type code to the AT12
// host.chassis.type enum. An Other/Unknown/unmapped code on a virtualized host
// is "vm" (AT12 derivation rule); otherwise "other".
func chassisName(code int, virt string) string {
	switch code {
	case 3, 4, 6, 7, 13, 15, 16, 24:
		return "desktop"
	case 8, 9, 10, 11, 14, 30, 31, 32:
		return "laptop"
	case 17, 22, 23, 25:
		return "server"
	case 28, 29:
		return "blade"
	}
	if virt != "" && virt != "none" {
		return "vm"
	}
	return "other"
}

// GetHostResourceAttributes returns the host described in OTel resource
// semantic-convention keys (host.* / os.*). These go on the OTLP resource of
// every signal the agent emits, so the agent's own metrics and logs carry the
// SAME host.id as the host entity on the entity rail — the join that lets a
// backend correlate the host node in the infra graph with its telemetry.
//
// Only non-empty values are returned; host.id is authoritative (same gopsutil
// HostID as the host entity identity).
func GetHostResourceAttributes() (map[string]string, error) {
	hostInfo, err := host.Info()
	if err != nil {
		return nil, fmt.Errorf("error getting host info: %v", err)
	}

	attrs := map[string]string{}
	set := func(k, v string) {
		if v != "" {
			attrs[k] = v
		}
	}
	set("host.id", hostInfo.HostID)
	set("host.name", canonicalHostname(hostInfo.Hostname))
	set("host.arch", hostInfo.KernelArch)
	set("os.type", hostInfo.OS)
	set("os.name", hostInfo.Platform)
	set("os.version", hostInfo.PlatformVersion)
	set("os.build_id", hostInfo.KernelVersion)
	set("os.description", strings.TrimSpace(hostInfo.Platform+" "+hostInfo.PlatformVersion))
	return attrs, nil
}

// GetHostTags returns common tags based on host information
func GetHostTags() ([]tags.Tag, error) {
	hostInfo, err := host.Info()
	if err != nil {
		return nil, fmt.Errorf("error getting host info: %v", err)
	}

	return []tags.Tag{
		{Key: "host", Value: canonicalHostname(hostInfo.Hostname), Private: false},
		{Key: "os", Value: hostInfo.OS, Private: false},
		{Key: "platform", Value: hostInfo.Platform, Private: false},
		//  {Key: "platform_version", Value: hostInfo.PlatformVersion, Private: false},
		//  {Key: "kernel_version", Value: hostInfo.KernelVersion, Private: false},
	}, nil
}

// IsWindows returns true if the OS is Windows
func IsWindows() (bool, error) {
	hostInfo, err := host.Info()
	if err != nil {
		return false, fmt.Errorf("error getting host info: %v", err)
	}
	return hostInfo.OS == "windows", nil
}

// IsLinux returns true if the OS is Linux
func IsLinux() (bool, error) {
	hostInfo, err := host.Info()
	if err != nil {
		return false, fmt.Errorf("error getting host info: %v", err)
	}
	return hostInfo.OS == "linux", nil
}
