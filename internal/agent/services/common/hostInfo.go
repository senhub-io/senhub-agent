package common

import (
	"fmt"
	"strings"

	"github.com/shirou/gopsutil/v3/host"
	"senhub-agent.go/internal/agent/tags"
)

// HostIdentity is the stable identity plus descriptive facts of the machine
// the agent runs on. ID is the machine-id / UUID (stable across rename and
// reboot) — the identifying attribute for the host entity. Name and OSType
// are descriptive.
type HostIdentity struct {
	ID     string
	Name   string
	OSType string
}

// GetHostIdentity returns the host's stable identity for entity detection.
// ID comes from gopsutil's HostID (the OS machine-id), which — unlike the
// hostname — does not change on rename, so it is safe to use as immutable
// entity identity.
func GetHostIdentity() (HostIdentity, error) {
	hostInfo, err := host.Info()
	if err != nil {
		return HostIdentity{}, fmt.Errorf("error getting host info: %v", err)
	}
	return HostIdentity{
		ID:     hostInfo.HostID,
		Name:   hostInfo.Hostname,
		OSType: hostInfo.OS,
	}, nil
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
