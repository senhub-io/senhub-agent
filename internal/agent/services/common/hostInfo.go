package common

import (
	"fmt"
	"github.com/shirou/gopsutil/v3/host"
	"senhub-agent.go/internal/agent/tags"
)

// GetHostTags returns common tags based on host information
func GetHostTags() ([]tags.Tag, error) {
	hostInfo, err := host.Info()
	if err != nil {
		return nil, fmt.Errorf("error getting host info: %v", err)
	}

	return []tags.Tag{
		{Key: "hostname", Value: hostInfo.Hostname, Private: false},
		{Key: "os", Value: hostInfo.OS, Private: false},
		{Key: "platform", Value: hostInfo.Platform, Private: false},
		{Key: "platform_version", Value: hostInfo.PlatformVersion, Private: false},
		{Key: "kernel_version", Value: hostInfo.KernelVersion, Private: false},
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
