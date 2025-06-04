//go:build !windows

// Package host provides system monitoring capabilities
package logicaldisk

import (
	"fmt"
	"os/exec"
	"runtime"
	"strings"
	"syscall"
	"time"

	"senhub-agent.go/internal/agent/services/common"
	"senhub-agent.go/internal/agent/services/data_store"
	"senhub-agent.go/internal/agent/services/logger"
	"senhub-agent.go/internal/agent/tags"

	"golang.org/x/sys/unix"
)

type unixLogicalDiskCollector struct{}

// newLogicalDiskCollector creates a new collector instance
func newLogicalDiskCollector(config map[string]interface{}, logger *logger.Logger) (logicaldiskCollector, error) {
	return &unixLogicalDiskCollector{}, nil
}

// shouldCollectMount determines if metrics should be collected for a given filesystem
// using the same logic as the df command
func (c *unixLogicalDiskCollector) shouldCollectMount(fsType string, mountPoint string, device string) bool {
	// Skip if device is "none" or empty
	if device == "none" || device == "" {
		return false
	}

	// Skip special filesystem types that df doesn't show
	excludedTypes := map[string]bool{
		"sysfs":       true,
		"proc":        true,
		"devpts":      true,
		"securityfs":  true,
		"cgroup2":     true,
		"pstore":      true,
		"bpf":         true,
		"fusectl":     true,
		"debugfs":     true,
		"tracefs":     true,
		"configfs":    true,
		"ramfs":       true,
		"hugetlbfs":   true,
		"mqueue":      true,
		"nsfs":        true,
		"autofs":      true,
		"binfmt_misc": true,
		"squashfs":    true,
		"overlay":     true,
	}

	if excludedTypes[fsType] {
		return false
	}

	// Handle tmpfs specially - only include specific mount points
	if fsType == "tmpfs" {
		// Liste explicite des points de montage autorisés
		allowedTmpfsMounts := map[string]bool{
			"/run":           true,
			"/dev/shm":       true,
			"/run/lock":      true,
			"/run/user/1001": true,
		}
		return allowedTmpfsMounts[mountPoint]
	}

	// Include standard filesystem types
	standardFS := map[string]bool{
		"ext4": true,
		"ext3": true,
		"ext2": true,
		"xfs":  true,
		"vfat": true,
		"fuse": true,
	}

	if standardFS[fsType] {
		return true
	}

	return false
}

func (c *unixLogicalDiskCollector) Collect(timestamp time.Time) ([]data_store.DataPoint, error) {
	var dataPoints []data_store.DataPoint

	baseTags, err := common.GetHostTags()
	if err != nil {
		return nil, fmt.Errorf("error getting host tags: %v", err)
	}

	// Get statistics about mounted filesystems
	var stat syscall.Statfs_t
	mounts, err := c.getMountPoints()
	if err != nil {
		return nil, fmt.Errorf("error getting mount points: %v", err)
	}

	for _, mount := range mounts {
		if !c.shouldCollectMount(mount.fstype, mount.mountpoint, mount.device) {
			continue
		}

		err := syscall.Statfs(mount.mountpoint, &stat)
		if err != nil {
			fmt.Printf("Cannot get stats for mount point %s: %v\n", mount.mountpoint, err)
			continue
		}

		// Calculate metrics
		totalBytes := uint64(stat.Blocks) * uint64(stat.Bsize)
		freeBytes := uint64(stat.Bfree) * uint64(stat.Bsize)
		availBytes := uint64(stat.Bavail) * uint64(stat.Bsize)
		usedBytes := totalBytes - freeBytes

		// Calculate usage percentage
		var usedPercent float32
		if totalBytes > 0 {
			usedPercent = float32(usedBytes) / float32(totalBytes) * 100
		}

		// Calculate inode metrics
		totalInodes := uint64(stat.Files)
		freeInodes := uint64(stat.Ffree)
		usedInodes := totalInodes - freeInodes

		// Calculate inode usage percentage
		var inodesUsedPercent float32
		if totalInodes > 0 {
			inodesUsedPercent = float32(usedInodes) / float32(totalInodes) * 100
		}

		// Create mount-specific tags
		mountTags := append([]tags.Tag{}, baseTags...)
		mountTags = append(mountTags,
			tags.Tag{Key: "mount_point", Value: mount.mountpoint},
			tags.Tag{Key: "fs_type", Value: mount.fstype},
			tags.Tag{Key: "device", Value: mount.device},
		)

		// Define metrics to collect
		metrics := []struct {
			name  string
			value float32
		}{
			{"fs_total_bytes", float32(totalBytes)},
			{"fs_free_bytes", float32(freeBytes)},
			{"fs_used_bytes", float32(usedBytes)},
			{"fs_available_bytes", float32(availBytes)},
			{"fs_used_percent", usedPercent},
			{"fs_inodes_total", float32(totalInodes)},
			{"fs_inodes_free", float32(freeInodes)},
			{"fs_inodes_used", float32(usedInodes)},
			{"fs_inodes_used_percent", inodesUsedPercent},
		}

		// Add data points
		for _, metric := range metrics {
			dataPoints = append(dataPoints, data_store.DataPoint{
				Name:      metric.name,
				Timestamp: timestamp,
				Value:     metric.value,
				Tags:      mountTags,
			})
		}
	}

	return dataPoints, nil
}

type mountInfo struct {
	device     string
	mountpoint string
	fstype     string
}

func (c *unixLogicalDiskCollector) getMountPoints() ([]mountInfo, error) {
	// Use different approaches based on OS
	switch runtime.GOOS {
	case "darwin":
		return c.getMountPointsDarwin()
	case "linux":
		return c.getMountPointsLinux()
	default:
		return c.getMountPointsLinux() // Default to Linux approach for other Unix systems
	}
}

func (c *unixLogicalDiskCollector) getMountPointsLinux() ([]mountInfo, error) {
	var mounts []mountInfo

	// Read /proc/mounts
	mountsFile, err := unix.Open("/proc/mounts", unix.O_RDONLY, 0)
	if err != nil {
		return nil, fmt.Errorf("error opening /proc/mounts: %v", err)
	}
	defer unix.Close(mountsFile)

	buf := make([]byte, 4096)
	var offset int64
	for {
		n, err := unix.Pread(mountsFile, buf, offset)
		if err != nil {
			return nil, fmt.Errorf("error reading /proc/mounts: %v", err)
		}
		if n == 0 {
			break
		}

		data := string(buf[:n])
		lines := strings.Split(data, "\n")
		for _, line := range lines {
			if line == "" {
				continue
			}

			fields := strings.Fields(line)
			if len(fields) < 3 {
				continue
			}

			mounts = append(mounts, mountInfo{
				device:     fields[0],
				mountpoint: fields[1],
				fstype:     fields[2],
			})
		}
		offset += int64(n)
	}

	return mounts, nil
}

func (c *unixLogicalDiskCollector) getMountPointsDarwin() ([]mountInfo, error) {
	var mounts []mountInfo

	// Use df command to get mounted filesystems on macOS
	cmd := exec.Command("df", "-h")
	output, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("error running df command: %v", err)
	}

	lines := strings.Split(string(output), "\n")
	for i, line := range lines {
		if i == 0 || line == "" {
			continue // Skip header and empty lines
		}

		fields := strings.Fields(line)
		if len(fields) < 9 {
			continue // Skip malformed lines
		}

		device := fields[0]
		mountpoint := fields[8]
		
		// Determine filesystem type from device name
		fstype := c.determineFSTypeDarwin(device)

		// Filter out pseudo filesystems and system mounts
		if c.shouldMonitorMountDarwin(mountpoint, fstype) {
			mounts = append(mounts, mountInfo{
				device:     device,
				mountpoint: mountpoint,
				fstype:     fstype,
			})
		}
	}

	return mounts, nil
}

// determineFSTypeDarwin determines filesystem type from device name on macOS
func (c *unixLogicalDiskCollector) determineFSTypeDarwin(device string) string {
	if strings.HasPrefix(device, "/dev/disk") {
		return "apfs" // Modern macOS uses APFS by default
	}
	if device == "devfs" {
		return "devfs"
	}
	if strings.HasPrefix(device, "map") {
		return "autofs"
	}
	return "unknown"
}

// shouldMonitorMountDarwin determines if a mount point should be monitored on macOS
func (c *unixLogicalDiskCollector) shouldMonitorMountDarwin(mountPoint, fsType string) bool {
	// Exclude virtual/system filesystems
	excludedTypes := map[string]bool{
		"devfs":   true,
		"autofs":  true,
		"unknown": true,
	}

	if excludedTypes[fsType] {
		return false
	}

	// Exclude system mount points (but allow /System/Volumes/Data for user data)
	excludedMounts := map[string]bool{
		"/dev":                       true,
		"/System/Volumes/Preboot":    true,
		"/System/Volumes/VM":         true,
		"/System/Volumes/Update":     true,
		"/System/Volumes/Data/home":  true,
	}

	if excludedMounts[mountPoint] {
		return false
	}

	// Include root filesystem
	if mountPoint == "/" {
		return true
	}

	// Include user data volume
	if mountPoint == "/System/Volumes/Data" {
		return true
	}

	// Include external volumes
	if strings.HasPrefix(mountPoint, "/Volumes/") {
		return true
	}

	// Include APFS filesystems
	if fsType == "apfs" {
		return true
	}

	return false
}

// Close performs any necessary cleanup
func (c *unixLogicalDiskCollector) Close() error {
	return nil
}
