---
title: "Logical Disk"
weight: 4
---

{{< hint info >}}
**License: Free** - No license required. Available in all tiers.
{{< /hint >}}

# LogicalDisk Probe

The LogicalDisk probe monitors disk and filesystem performance across all major operating systems, providing comprehensive metrics for capacity, I/O operations, throughput, and filesystem health.

## Quick Start

### Basic Configuration

```yaml
probes:
  - name: logicaldisk
    params:
      interval: 30  # Collection interval in seconds (default: 30)
```

### Minimal Configuration

```yaml
probes:
  - name: logicaldisk
    params: {}
```

The LogicalDisk probe requires no mandatory parameters and works out-of-the-box with default settings.

## Supported Platforms

- **Windows**: Windows Server 2012+ / Windows 10+
- **Linux**: All modern distributions (Ubuntu, RHEL, CentOS, Debian, etc.)
- **macOS**: macOS 10.13+
- **BSD**: FreeBSD, OpenBSD, NetBSD

Platform-specific metrics are automatically detected and collected based on the operating system.

## Key Metrics Summary

### Cross-Platform Capacity Metrics

| Metric | Description | Available On |
|--------|-------------|--------------|
| `disk_free_mb` | Free disk space in megabytes | Windows |
| `disk_free_percent` | Free disk space percentage | Windows |
| `fs_free_bytes` | Free filesystem space in bytes | Unix/Linux/macOS |
| `fs_used_bytes` | Used filesystem space in bytes | Unix/Linux/macOS |
| `fs_used_percent` | Used filesystem space percentage | Unix/Linux/macOS |
| `fs_total_bytes` | Total filesystem capacity | Unix/Linux/macOS |

### Cross-Platform I/O Metrics

| Metric | Description | Available On |
|--------|-------------|--------------|
| `disk_reads_sec` | Disk read operations per second | Windows |
| `disk_writes_sec` | Disk write operations per second | Windows |
| `disk_read_bytes_sec` | Disk read throughput (bytes/sec) | Windows |
| `disk_write_bytes_sec` | Disk write throughput (bytes/sec) | Windows |
| `disk_queue_length` | Current disk queue length | Windows |

### Unix/Linux/macOS Specific

| Metric | Description |
|--------|-------------|
| `fs_available_bytes` | Available space for non-root users |
| `fs_inodes_total` | Total inode count |
| `fs_inodes_free` | Free inode count |
| `fs_inodes_used` | Used inode count |
| `fs_inodes_used_percent` | Inode usage percentage |

## Configuration Parameters

| Parameter | Type | Default | Description |
|-----------|------|---------|-------------|
| `interval` | integer | `30` | Collection interval in seconds |
| `filters.include` | array | `[]` | Drive patterns to include (Windows only) |
| `filters.exclude` | array | `["HarddiskVolume*", "_Total"]` | Drive patterns to exclude (Windows only) |

### Example Configurations

**High-frequency monitoring (every 10 seconds):**
```yaml
probes:
  - name: logicaldisk
    params:
      interval: 10
```

**Standard monitoring (every minute):**
```yaml
probes:
  - name: logicaldisk
    params:
      interval: 60
```

**Windows: Monitor specific drives only:**
```yaml
probes:
  - name: logicaldisk
    params:
      interval: 30
      filters:
        include: ["C:", "D:", "E:"]
        exclude: ["HarddiskVolume*", "_Total"]
```

**Windows: Exclude drives matching patterns:**
```yaml
probes:
  - name: logicaldisk
    params:
      interval: 30
      filters:
        exclude: ["HarddiskVolume*", "_Total", "X:", "Y:"]
```

## Monitoring Tool Integration

### PRTG Network Monitor

Access LogicalDisk metrics in PRTG JSON format:

```bash
# All disk metrics
curl http://localhost:8080/api/{agentkey}/prtg/metrics

# Configure PRTG HTTP Advanced Sensor:
# - URL: http://agent-host:8080/api/{agentkey}/prtg/metrics
# - Method: POST
# - Request body: {"probe": "logicaldisk"}
```

**PRTG Channels Available:**
- Disk Free Space (MB or %)
- Disk IOPS (reads/writes per second)
- Disk Throughput (read/write bytes per second)
- Disk Queue Length
- Filesystem Usage (bytes and %)
- Inode Usage (Unix/Linux)

### Nagios/Icinga

Access LogicalDisk metrics in Nagios format:

```bash
# All disk metrics with performance data
curl http://localhost:8080/api/{agentkey}/nagios/metrics?probe=logicaldisk

# Example output:
# OK - Disk monitoring active | disk_free_percent_C:=45.2%;10;5 disk_queue_length=0.8;;;
```

**Nagios Performance Data:**
- `disk_free_percent` - Free space percentage with 10% warning, 5% critical
- `fs_used_percent` - Used space percentage (Unix)
- `disk_queue_length` - Disk queue depth (Windows)
- `fs_inodes_used_percent` - Inode usage (Unix)

### Grafana/Prometheus

Access metrics in Prometheus-compatible format:

```bash
# Prometheus format
curl http://localhost:8080/api/{agentkey}/prometheus/metrics

# Example output:
# disk_free_percent{hostname="server01",drive="C:"} 45.2
# disk_reads_sec{hostname="server01",drive="C:"} 123.4
# fs_used_percent{hostname="server01",mount_point="/",device="/dev/sda1"} 67.8
```

### Web Interface

View LogicalDisk metrics in the built-in dashboard:

```
http://localhost:8080/web/{agentkey}/dashboard
```

Features:
- Real-time disk usage visualization
- Per-disk/mount point breakdown
- I/O throughput graphs
- Capacity trend analysis

## Use Cases

### Capacity Planning
Monitor disk space to prevent:
- Out-of-disk-space incidents
- Application failures due to full disks
- Database transaction log issues
- Backup failures

**Key Metrics:**
- `disk_free_percent` / `fs_used_percent`
- `fs_inodes_used_percent` (Unix/Linux)

**Alert Thresholds:**
- Warning: < 20% free
- Critical: < 10% free
- Inode Warning: > 80% used

### I/O Bottleneck Detection
Identify storage performance issues:
- Slow disk I/O
- High queue depths
- Excessive IOPS
- Throughput saturation

**Key Metrics:**
- `disk_queue_length` (Windows)
- `disk_reads_sec` / `disk_writes_sec`
- `disk_read_bytes_sec` / `disk_write_bytes_sec`

**Alert Thresholds:**
- Queue Length: > 2 sustained
- IOPS: Depends on disk type (HDD: ~200, SSD: ~10000)

### Filesystem Health Monitoring
Track filesystem integrity and performance:
- Inode exhaustion (Unix/Linux)
- Filesystem type optimization
- Mount point availability
- Storage subsystem health

**Key Metrics:**
- `fs_inodes_used_percent` (Unix/Linux)
- Availability of expected mount points
- Filesystem type distribution

### Application-Specific Monitoring
Monitor critical application storage:
- Database data and log volumes
- Application temp directories
- Cache directories
- User data volumes

**Implementation:**
- Use drive/mount_point tags for filtering
- Set application-specific thresholds
- Correlate with application metrics

## Platform-Specific Behavior

### Windows

**Collection Method:**
- Uses Performance Data Helper (PDH) counters
- Monitors LogicalDisk performance object
- Real-time per-drive metrics

**Drive Identification:**
- Drive letters: `C:`, `D:`, `E:`, etc.
- Tag: `drive=C:`

**Default Filters:**
- Includes: All drives (empty = all)
- Excludes: `HarddiskVolume*` (system volumes), `_Total` (aggregate)

**Example Metrics:**
```
disk_free_mb{drive="C:"} = 102400
disk_free_percent{drive="C:"} = 45.2
disk_reads_sec{drive="C:"} = 123.4
disk_writes_sec{drive="C:"} = 87.6
disk_read_bytes_sec{drive="C:"} = 5242880
disk_write_bytes_sec{drive="C:"} = 3145728
disk_queue_length{drive="C:"} = 0.8
```

### Unix/Linux

**Collection Method:**
- Uses `/proc/mounts` for filesystem discovery
- `syscall.Statfs` for capacity metrics
- Filters pseudo-filesystems automatically

**Filesystem Type Filtering:**
- **Included:** ext4, ext3, ext2, xfs, vfat, fuse
- **Excluded:** sysfs, proc, devpts, overlay, squashfs, etc.
- **tmpfs:** Only specific mounts (`/run`, `/dev/shm`, `/run/lock`)

**Mount Point Identification:**
- Full mount path: `/`, `/home`, `/var`, etc.
- Tags: `mount_point=/home`, `fs_type=ext4`, `device=/dev/sda1`

**Example Metrics:**
```
fs_total_bytes{mount_point="/",device="/dev/sda1",fs_type="ext4"} = 107374182400
fs_free_bytes{mount_point="/",device="/dev/sda1",fs_type="ext4"} = 48318382080
fs_used_bytes{mount_point="/",device="/dev/sda1",fs_type="ext4"} = 59055800320
fs_available_bytes{mount_point="/",device="/dev/sda1",fs_type="ext4"} = 45318382080
fs_used_percent{mount_point="/",device="/dev/sda1",fs_type="ext4"} = 55.0
fs_inodes_total{mount_point="/",device="/dev/sda1",fs_type="ext4"} = 6553600
fs_inodes_free{mount_point="/",device="/dev/sda1",fs_type="ext4"} = 5242880
fs_inodes_used{mount_point="/",device="/dev/sda1",fs_type="ext4"} = 1310720
fs_inodes_used_percent{mount_point="/",device="/dev/sda1",fs_type="ext4"} = 20.0
```

### macOS

**Collection Method:**
- Uses `df -h` command for filesystem discovery
- `syscall.Statfs` for capacity metrics
- APFS-specific handling with automatic whitelist inclusion

**Filesystem Type Detection:**
- **APFS**: Modern macOS filesystem for volumes starting with `/dev/disk*` (automatically included)
- **Excludes**: devfs, autofs, system volumes

**APFS Support:**
The LogicalDisk probe automatically recognizes and monitors APFS filesystems on macOS. APFS (Apple File System) is the default filesystem for macOS 10.13+ and includes:
- Container-based storage with shared space pools
- Multiple volumes within a single container
- Snapshot and cloning capabilities
- Strong encryption support

All APFS volumes are included in the `standardFS` whitelist for automatic monitoring.

**Mount Point Filtering:**
- **Included:** `/`, `/System/Volumes/Data`, `/Volumes/*`, APFS volumes
- **Excluded:** `/dev`, `/System/Volumes/Preboot`, `/System/Volumes/VM`, `/System/Volumes/Update`

**Example Metrics:**
```
fs_total_bytes{mount_point="/",device="/dev/disk1s1",fs_type="apfs"} = 250685575168
fs_free_bytes{mount_point="/",device="/dev/disk1s1",fs_type="apfs"} = 123456789120
fs_used_percent{mount_point="/",device="/dev/disk1s1",fs_type="apfs"} = 50.7
fs_inodes_total{mount_point="/",device="/dev/disk1s1",fs_type="apfs"} = 9223372036854775807
fs_inodes_used_percent{mount_point="/",device="/dev/disk1s1",fs_type="apfs"} = 0.01
```

**Note**: APFS uses a dynamic inode allocation model, so inode metrics may show very large totals.

## Troubleshooting

### No Metrics Collected

**Check probe status:**
```bash
# View agent logs with LogicalDisk probe debugging
./agent run --authentication-key YOUR_KEY --verbose --debug-modules probe.logicaldisk
```

**Verify probe is enabled:**
```bash
# Check configuration
cat agent-config.yaml | grep -A5 "name: logicaldisk"
```

### Windows: PDH Counter Errors

**Symptom:** Error messages about Performance Data Helper (PDH) counters

**Solution:**
1. Verify Performance Counter service is running:
   ```powershell
   Get-Service | Where-Object {$_.Name -eq "PerfHost"}
   ```

2. Rebuild Performance Counters:
   ```powershell
   lodctr /R
   ```

3. Check Windows Event Log for PDH errors

### Unix/Linux: Permission Denied

**Symptom:** Cannot read `/proc/mounts` or perform `statfs()` calls

**Solution:**
Run the agent with appropriate permissions:
```bash
# Option 1: Run as root
sudo ./agent run --authentication-key YOUR_KEY

# Option 2: Grant capabilities (Linux)
sudo setcap cap_sys_ptrace=eip ./agent
```

### Windows: Drives Not Appearing

**Symptom:** Expected drives missing from metrics

**Possible Causes:**
1. **Drive filtered out:** Check `filters.exclude` configuration
2. **System volume:** `HarddiskVolume*` excluded by default
3. **Network drive:** May not appear in LogicalDisk counters

**Solution:**
```yaml
probes:
  - name: logicaldisk
    params:
      filters:
        include: ["C:", "D:", "E:", "Z:"]  # Explicitly include network drive
        exclude: ["_Total"]  # Keep minimal exclusions
```

### Unix/Linux: Filesystems Not Appearing

**Symptom:** Expected mount points missing from metrics

**Possible Causes:**
1. **Filesystem type excluded:** Check `shouldCollectMount()` logic
2. **tmpfs mount not in allowed list:** Only specific tmpfs mounts included
3. **Pseudo-filesystem:** sysfs, proc, devpts, etc. are excluded

**Debug:**
```bash
# List all mount points
cat /proc/mounts

# Check which filesystems are collected
./agent run --authentication-key YOUR_KEY --verbose --debug-modules probe.logicaldisk
```

**Solution (if needed):**
Modify configuration to include specific mount points or filesystem types (requires code modification for custom filters).

### Inode Exhaustion Not Detected

**Symptom:** Disk has space but applications fail (Unix/Linux)

**Cause:** Inode exhaustion (many small files)

**Solution:**
Monitor `fs_inodes_used_percent` metric:
```bash
# Check inode usage manually
df -i

# Set alerts on inode usage
# Alert if fs_inodes_used_percent > 80%
```

### Disk Queue Length Always Zero

**Symptom:** `disk_queue_length` metric shows 0 even under load (Windows)

**Possible Causes:**
1. **Disk not busy:** Normal behavior for idle disks
2. **SSD with high IOPS:** Queue empties too quickly to observe
3. **Counter issue:** PDH counter not updating

**Debug:**
```powershell
# Check queue length in Performance Monitor
typeperf "\LogicalDisk(C:)\Current Disk Queue Length" -sc 10
```

## Performance Considerations

### Collection Overhead

The LogicalDisk probe has minimal overhead:
- **Windows**: ~50ms per collection (PDH counters)
- **Unix/Linux**: ~100ms per collection (filesystem syscalls)
- **macOS**: ~150ms per collection (`df` command + syscalls)

### Memory Usage

Typical memory footprint per collection:
- Base probe: ~200 KB
- Per-drive/mount metrics: ~100 KB per filesystem
- Example: 5 drives/mounts = ~700 KB total

### Recommended Intervals

| Use Case | Interval | Reason |
|----------|----------|--------|
| Capacity monitoring | 60-300s | Disk space changes slowly |
| I/O monitoring | 30-60s | Capture I/O patterns |
| Critical volumes | 10-30s | Rapid detection of issues |

**Note:** I/O metrics (IOPS, throughput, queue) benefit from shorter intervals. Capacity metrics can use longer intervals.

## Advanced Configuration

### Multi-Instance Monitoring

Monitor with different intervals for capacity vs. I/O:

```yaml
probes:
  - name: logicaldisk_capacity
    params:
      interval: 300  # Check capacity every 5 minutes

  - name: logicaldisk_io
    params:
      interval: 30  # Check I/O every 30 seconds
```

**Note:** This will create duplicate metrics. Use unique probe names for different collection intervals.

### Integration with Other Probes

Correlate disk metrics with system metrics:

```yaml
probes:
  - name: cpu
    params:
      interval: 30

  - name: memory
    params:
      interval: 30

  - name: logicaldisk
    params:
      interval: 30
```

This provides comprehensive system monitoring:
- High CPU I/O wait + high disk queue = disk bottleneck
- High memory usage + high disk I/O = swapping/paging
- Low disk free + application errors = capacity issue

### Windows Drive Letter Filtering

**Scenario:** Monitor only production data drives

```yaml
probes:
  - name: logicaldisk
    params:
      interval: 30
      filters:
        include: ["D:", "E:", "F:"]  # Only data drives
        exclude: ["_Total"]
```

**Scenario:** Exclude temporary/cache drives

```yaml
probes:
  - name: logicaldisk
    params:
      interval: 30
      filters:
        exclude: ["HarddiskVolume*", "_Total", "T:", "X:"]
```

### Unix/Linux Filesystem Filtering

**Note:** Unix/Linux filesystem filtering is built-in and automatic. To customize:

1. **Include standard filesystems:** ext4, ext3, xfs, vfat (automatic)
2. **Exclude pseudo-filesystems:** sysfs, proc, overlay (automatic)
3. **Custom tmpfs mounts:** Modify `allowedTmpfsMounts` in code

**Typical Use Cases:**
- Monitor root filesystem: `/` (automatic)
- Monitor data volumes: `/data`, `/var`, `/home` (automatic)
- Monitor external mounts: `/mnt/*` (depends on fs_type)

## Authentication

The LogicalDisk probe requires no authentication as it collects local system metrics only.

## Requirements

### Windows
- Windows Server 2012+ or Windows 10+
- Performance Counter service enabled
- No special permissions required (runs as service account)

### Linux/Unix/macOS
- Read access to `/proc/mounts` (Linux)
- `syscall.Statfs` permissions (standard user)
- `df` command available (macOS)

### Network
- No network access required (local metrics only)
- HTTP strategy required for remote access to metrics
