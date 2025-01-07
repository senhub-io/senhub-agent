//go:build !windows

// internal/agent/probes/host/logicaldiskProbe_unix.go
//
package host

import (
	"fmt"
	"strings"
	"syscall"
	"time"

	"senhub-agent.go/internal/agent/services/common"
	"senhub-agent.go/internal/agent/services/data_store"
	"senhub-agent.go/internal/agent/services/logger"
	"senhub-agent.go/internal/agent/tags"

	"golang.org/x/sys/unix"
)

type unixLogicalDiskCollector struct {
	excludedFsTypes     []string
	excludedMountPoints []string
}

func newLogicalDiskCollector(config map[string]interface{}, logger *logger.Logger) (logicaldiskCollector, error) {
	collector := &unixLogicalDiskCollector{
		// List of filesystem types to exclude from monitoring
		excludedFsTypes: []string{
			"proc", "sysfs", "fusectl", "debugfs", "securityfs", "devpts", "cgroup",
			"cgroup2", "pstore", "bpf", "hugetlbfs", "mqueue", "devtmpfs", "none",
			"sunrpc", "ramfs", "tmpfs", "tracefs", "nsfs", "autofs", "binfmt_misc",
			"rpc_pipefs", "nfsd", "overlay", "configfs", "selinuxfs",
		},
		// List of mount points to exclude from monitoring
		excludedMountPoints: []string{
			"/proc", "/sys", "/dev", "/run",
		},
	}

	// Override exclusions from configuration if specified
	if excludedTypes, ok := config["excluded_fs_types"].([]string); ok {
		collector.excludedFsTypes = excludedTypes
	}
	if excludedMounts, ok := config["excluded_mount_points"].([]string); ok {
		collector.excludedMountPoints = excludedMounts
	}

	return collector, nil
}

func (c *unixLogicalDiskCollector) shouldCollectMount(fsType string, mountPoint string) bool {
	// Check if filesystem type is excluded
	for _, excluded := range c.excludedFsTypes {
		if fsType == excluded {
			return false
		}
	}

	// Check if mount point is excluded
	for _, excluded := range c.excludedMountPoints {
		if mountPoint == excluded {
			return false
		}
	}

	return true
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
		if !c.shouldCollectMount(mount.fstype, mount.mountpoint) {
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
		totalInodes := stat.Files
		freeInodes := stat.Ffree
		usedInodes := totalInodes - freeInodes

		var inodeUsedPercent float32
		if totalInodes > 0 {
			inodeUsedPercent = float32(usedInodes) / float32(totalInodes) * 100
		}

		// Prepare mount-specific tags
		mountTags := append([]tags.Tag{}, baseTags...)
		mountTags = append(mountTags,
			tags.Tag{Key: "mount_point", Value: mount.mountpoint},
			tags.Tag{Key: "fs_type", Value: mount.fstype},
		)

		// Add metrics
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
			{"fs_inodes_used_percent", inodeUsedPercent},
		}

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
	var mounts []mountInfo

	// Read /proc/mounts on Linux
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

func (c *unixLogicalDiskCollector) Close() error {
	return nil
}
