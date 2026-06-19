package common

import (
	"fmt"
	"strings"
	"sync"

	"github.com/shirou/gopsutil/v3/cpu"
	"github.com/shirou/gopsutil/v3/host"
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
	np := getHostNameplate()
	return HostIdentity{
		ID:            hostInfo.HostID,
		Name:          hostInfo.Hostname,
		OSType:        hostInfo.OS,
		Arch:          hostInfo.KernelArch,
		OSName:        hostInfo.Platform,
		OSVersion:     hostInfo.PlatformVersion,
		OSBuildID:     hostInfo.KernelVersion,
		OSDescription: strings.TrimSpace(hostInfo.Platform + " " + hostInfo.PlatformVersion),
		CPUModel:      np.cpuModel,
		CPUVendor:     np.cpuVendor,
		HWVendor:      np.hwVendor,
		HWModel:       np.hwModel,
		HWSerial:      np.hwSerial,
	}, nil
}

// hostNameplate holds the host's static CPU and hardware identity. These never
// change for the process lifetime, so they are gathered once.
type hostNameplate struct {
	cpuModel, cpuVendor         string
	hwVendor, hwModel, hwSerial string
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
func getHostNameplate() hostNameplate {
	nameplateOnce.Do(func() {
		if infos, err := cpu.Info(); err == nil && len(infos) > 0 {
			nameplate.cpuModel = strings.TrimSpace(infos[0].ModelName)
			nameplate.cpuVendor = strings.TrimSpace(infos[0].VendorID)
		}
		hw := readHardwareNameplate()
		nameplate.hwVendor = hw.vendor
		nameplate.hwModel = hw.model
		nameplate.hwSerial = hw.serial
	})
	return nameplate
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
	set("host.name", hostInfo.Hostname)
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
		{Key: "host", Value: hostInfo.Hostname, Private: false},
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
