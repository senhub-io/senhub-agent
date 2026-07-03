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

// statfsTimeout bounds a single syscall.Statfs call. A stale network/FUSE
// mount that slips past the blocklist can wedge statfs indefinitely; capping it
// keeps one bad mount from stalling the whole collection cycle. The mount is
// skipped on timeout (the blocking goroutine is left to unwind if the kernel
// ever returns — one leaked goroutine per genuinely-hung mount is the accepted
// trade-off versus a stalled probe).
const statfsTimeout = 5 * time.Second

type unixLogicalDiskCollector struct {
	logger *logger.ModuleLogger
}

// newLogicalDiskCollector creates a new collector instance
func newLogicalDiskCollector(config map[string]interface{}, baseLogger *logger.Logger) (logicaldiskCollector, error) {
	return &unixLogicalDiskCollector{
		logger: logger.NewModuleLogger(baseLogger, "probe.logicaldisk"),
	}, nil
}

// statfsResult carries a statfs outcome back from the bounded goroutine.
type statfsResult struct {
	stat syscall.Statfs_t
	err  error
}

// statfsWithTimeout runs syscall.Statfs under statfsTimeout so a hung mount
// cannot block the collection cycle. The worker goroutine sends on a buffered
// channel, so it never blocks on send even after we stopped waiting.
func statfsWithTimeout(path string) (syscall.Statfs_t, error) {
	ch := make(chan statfsResult, 1)
	go func() {
		var st syscall.Statfs_t
		err := syscall.Statfs(path, &st)
		ch <- statfsResult{stat: st, err: err}
	}()
	select {
	case r := <-ch:
		return r.stat, r.err
	case <-time.After(statfsTimeout):
		return syscall.Statfs_t{}, fmt.Errorf("statfs on %s timed out after %s (stale mount?)", path, statfsTimeout)
	}
}

// shouldCollectMount determines if metrics should be collected for a given filesystem
// using the same logic as the df command
func (c *unixLogicalDiskCollector) shouldCollectMount(fsType string, mountPoint string, device string) bool {
	// Skip if device is "none" or empty
	if device == "none" || device == "" {
		return false
	}

	// macOS firmlink / synthetic system volumes (iSCPreboot, xarts,
	// Hardware, Update, etc.) share the same APFS container as the
	// main user volumes — statfs reports the parent container's space,
	// producing N near-duplicate series per filesystem metric. Only
	// keep / and the writable Data volume (Catalina+ split).
	if runtime.GOOS == "darwin" && strings.HasPrefix(mountPoint, "/System/Volumes/") && mountPoint != "/System/Volumes/Data" {
		return false
	}

	// Blocklist model: collect every real on-disk filesystem and only skip the
	// ones that carry no meaningful capacity for an operator. A whitelist would
	// silently drop any filesystem it doesn't name (btrfs, zfs, f2fs, ntfs, …);
	// the industry-standard collectors (node_exporter, telegraf) all blocklist
	// pseudo filesystems and keep the rest, which is what this does.
	excludedTypes := map[string]bool{
		// Kernel / pseudo filesystems with no real capacity.
		"sysfs":       true,
		"proc":        true,
		"procfs":      true,
		"devpts":      true,
		"devtmpfs":    true,
		"devfs":       true,
		"securityfs":  true,
		"selinuxfs":   true,
		"efivarfs":    true,
		"cgroup":      true,
		"cgroup2":     true,
		"pstore":      true,
		"bpf":         true,
		"fusectl":     true,
		"fuse.portal": true,
		"debugfs":     true,
		"tracefs":     true,
		"configfs":    true,
		"ramfs":       true,
		"hugetlbfs":   true,
		"mqueue":      true,
		"nsfs":        true,
		"autofs":      true,
		"binfmt_misc": true,
		"rpc_pipefs":  true,
		"squashfs":    true,
		"iso9660":     true,
		"overlay":     true,
		// Network filesystems: excluded because a stale NFS/CIFS mount makes
		// statfs block. statfsWithTimeout now bounds each call, but a hung
		// network mount would still burn statfsTimeout every cycle, so keep them
		// out by type rather than paying that cost on every collection.
		"nfs":        true,
		"nfs4":       true,
		"cifs":       true,
		"smb3":       true,
		"smbfs":      true,
		"sshfs":      true,
		"fuse.sshfs": true,
		"ceph":       true,
		"glusterfs":  true,
		"davfs":      true,
		"afs":        true,
		"ncpfs":      true,
	}

	if excludedTypes[fsType] {
		return false
	}

	// FUSE network/user filesystems surface as fuse.<backend> (fuse.rclone,
	// fuse.s3fs, fuse.gcsfuse, fuse.davfs2, fuse.ceph, fuse.glusterfs, gvfs, …).
	// A stale one wedges statfs exactly like NFS/CIFS, so exclude the whole
	// fuse.* family by default. Plain "fuse" (local passthrough) stays included;
	// fusectl/fuse.portal are already covered by excludedTypes above.
	if strings.HasPrefix(fsType, "fuse.") {
		return false
	}

	// Handle tmpfs specially - only include specific mount points
	if fsType == "tmpfs" {
		// Explicit allowlist of persistent, fleet-consistent tmpfs mounts.
		allowedTmpfsMounts := map[string]bool{
			"/run":      true,
			"/dev/shm":  true,
			"/run/lock": true,
		}
		if allowedTmpfsMounts[mountPoint] {
			return true
		}
		// Per-user runtime dirs are /run/user/<uid> for whatever uids exist on
		// the host; match by prefix instead of hardcoding one uid.
		return strings.HasPrefix(mountPoint, "/run/user/")
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
	mounts, err := c.getMountPoints()
	if err != nil {
		return nil, fmt.Errorf("error getting mount points: %w", err)
	}

	for _, mount := range mounts {
		if !c.shouldCollectMount(mount.fstype, mount.mountpoint, mount.device) {
			continue
		}

		stat, err := statfsWithTimeout(mount.mountpoint)
		if err != nil {
			c.logger.Warn().
				Str("mount_point", mount.mountpoint).
				Str("fs_type", mount.fstype).
				Err(err).
				Msg("cannot stat mount point; skipping")
			continue
		}

		// Calculate metrics
		totalBytes := uint64(stat.Blocks) * uint64(stat.Bsize)
		freeBytes := uint64(stat.Bfree) * uint64(stat.Bsize)
		availBytes := uint64(stat.Bavail) * uint64(stat.Bsize)
		usedBytes := totalBytes - freeBytes

		// Calculate usage percentage
		var usedPercent float64
		if totalBytes > 0 {
			usedPercent = float64(usedBytes) / float64(totalBytes) * 100
		}

		// Calculate inode metrics
		totalInodes := uint64(stat.Files)
		freeInodes := uint64(stat.Ffree)
		usedInodes := totalInodes - freeInodes

		// Calculate inode usage percentage
		var inodesUsedPercent float64
		if totalInodes > 0 {
			inodesUsedPercent = float64(usedInodes) / float64(totalInodes) * 100
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
			value float64
		}{
			{"fs_total_bytes", float64(totalBytes)},
			{"fs_free_bytes", float64(freeBytes)},
			{"fs_used_bytes", float64(usedBytes)},
			{"fs_available_bytes", float64(availBytes)},
			{"fs_used_percent", usedPercent},
			{"fs_inodes_total", float64(totalInodes)},
			{"fs_inodes_free", float64(freeInodes)},
			{"fs_inodes_used", float64(usedInodes)},
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

	appendLine := func(line string) {
		if line == "" {
			return
		}
		fields := strings.Fields(line)
		if len(fields) < 3 {
			return
		}
		mounts = append(mounts, mountInfo{
			device:     fields[0],
			mountpoint: fields[1],
			fstype:     fields[2],
		})
	}

	buf := make([]byte, 4096)
	var offset int64
	// carry holds a partial trailing line whose newline lands in a later chunk.
	// Without it, a mount line straddling a 4 KiB boundary (common on hosts with
	// many cgroup/overlay mounts) is split in two and both halves are corrupted.
	var carry string
	for {
		n, err := unix.Pread(mountsFile, buf, offset)
		if err != nil {
			return nil, fmt.Errorf("error reading /proc/mounts: %w", err)
		}
		if n == 0 {
			break
		}

		data := carry + string(buf[:n])
		if idx := strings.LastIndexByte(data, '\n'); idx >= 0 {
			for _, line := range strings.Split(data[:idx], "\n") {
				appendLine(line)
			}
			carry = data[idx+1:]
		} else {
			carry = data
		}
		offset += int64(n)
	}
	// A final line without a trailing newline (rare, but don't drop it).
	appendLine(strings.TrimRight(carry, "\n"))

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
		"/dev":                      true,
		"/System/Volumes/Preboot":   true,
		"/System/Volumes/VM":        true,
		"/System/Volumes/Update":    true,
		"/System/Volumes/Data/home": true,
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
