<img src="../../assets/probe-logos/logicaldisk.svg" alt="" class="probe-page-logo probe-page-logo-mdi">

!!! info
    **License: Free** - No license required. Available in all tiers.

# LogicalDisk Probe

The LogicalDisk probe monitors disk and filesystem performance across all major operating systems, providing comprehensive metrics for capacity, I/O operations, throughput, and filesystem health.

## Quick Start

### Basic Configuration

```yaml
# probes.d/10-logicaldisk.yaml — each file under probes.d/ is a YAML array of probes
- name: logicaldisk
  type: logicaldisk
  params:
    interval: 30  # Collection interval in seconds (default: 30)
```

### Minimal Configuration

```yaml
# probes.d/10-logicaldisk.yaml
- name: logicaldisk
  type: logicaldisk
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

<!-- schema:params:start -->
<!-- Generated from the probe's schema. Run `make docs-params` after changing it. -->
<!-- sha256:198837a23e7d6cd31759bcf772e2e80a2a10db796bd6022edbb2ccee3d91bf7f -->

| Parameter | Must set | Default | Description |
|---|---|---|---|
| `interval` | No | `30` | Collection interval in seconds |
| `filters` | No | - | Drive selection (Windows only) |
| `filters.include` | No | - | Drive patterns to include |
| `filters.exclude` | No | `[HarddiskVolume* _Total]` | Drive patterns to exclude |

<!-- schema:params:end -->

### Example Configurations

**High-frequency monitoring (every 10 seconds):**
```yaml
# probes.d/10-logicaldisk.yaml
- name: logicaldisk
  type: logicaldisk
  params:
    interval: 10
```

**Standard monitoring (every minute):**
```yaml
# probes.d/10-logicaldisk.yaml
- name: logicaldisk
  type: logicaldisk
  params:
    interval: 60
```

**Windows: Monitor specific drives only:**
```yaml
# probes.d/10-logicaldisk.yaml
- name: logicaldisk
  type: logicaldisk
  params:
    interval: 30
    filters:
      include: ["C:", "D:", "E:"]
      exclude: ["HarddiskVolume*", "_Total"]
```

**Windows: Exclude drives matching patterns:**
```yaml
# probes.d/10-logicaldisk.yaml
- name: logicaldisk
  type: logicaldisk
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

### Nagios

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
- Tags: `drive=C:`, `device=C:`, `fs_type=ntfs`

**Default Filters:**
- Includes: All drives (empty = all)
- Excludes: `HarddiskVolume*` (system volumes), `_Total` (aggregate)

**Example Metrics:**
```
disk_free_mb{drive="C:",device="C:",fs_type="ntfs"} = 102400
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
senhub-agent run --filter probe.logicaldisk
```

**Verify probe is enabled:**
```bash
# Check configuration (multi-file layout)
grep -rA5 "name: logicaldisk" /etc/senhub-agent/probes.d/
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
sudo senhub-agent run

# Option 2: Grant capabilities (Linux)
sudo setcap cap_sys_ptrace=eip /opt/senhub/bin/senhub-agent
```

### Windows: Drives Not Appearing

**Symptom:** Expected drives missing from metrics

**Possible Causes:**
1. **Drive filtered out:** Check `filters.exclude` configuration
2. **System volume:** `HarddiskVolume*` excluded by default
3. **Network drive:** May not appear in LogicalDisk counters

**Solution:**
```yaml
# probes.d/10-logicaldisk.yaml
- name: logicaldisk
  type: logicaldisk
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
senhub-agent run --filter probe.logicaldisk
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
# probes.d/10-logicaldisk.yaml
- name: logicaldisk_capacity
  type: logicaldisk
  params:
    interval: 300  # Check capacity every 5 minutes

- name: logicaldisk_io
  type: logicaldisk
  params:
    interval: 30  # Check I/O every 30 seconds
```

**Note:** This will create duplicate metrics. Use unique probe names for different collection intervals.

### Integration with Other Probes

Correlate disk metrics with system metrics:

```yaml
# probes.d/00-host.yaml
- name: cpu
  type: cpu
  params:
    interval: 30

- name: memory
  type: memory
  params:
    interval: 30

- name: logicaldisk
  type: logicaldisk
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
# probes.d/10-logicaldisk.yaml
- name: logicaldisk
  type: logicaldisk
  params:
    interval: 30
    filters:
      include: ["D:", "E:", "F:"]  # Only data drives
      exclude: ["_Total"]
```

**Scenario:** Exclude temporary/cache drives

```yaml
# probes.d/10-logicaldisk.yaml
- name: logicaldisk
  type: logicaldisk
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

## Metric reference

Every metric this probe can emit. **Metric** is the OpenTelemetry name the
OTLP, Prometheus and Zabbix outputs derive theirs from. **Name** is what a
[Nagios check](../nagios.md) and the API `metrics=` filter match.
**PRTG channel** is the label PRTG shows, placeholders filled from the
series' tags.

<!-- schema:metrics:start -->
<!-- Generated from the probe's definition. Run `make docs-metrics` after changing it. -->

| Metric | Name | PRTG channel | Unit | Description |
|---|---|---|---|---|
| `system.filesystem.usage` | `disk_free_mb` | Disk Free Mb ({drive}) | MB | Free disk space in megabytes on the logical drive |
| `system.filesystem.utilization` | `disk_free_percent` | Disk Free Percent ({drive}) | % | Percentage of total disk space that is free on the logical drive |
| `system.filesystem.utilization` | `disk_used_percent` | Disk Used Percent ({drive}) | % | Percentage of total disk space currently in use on the logical drive |
| `senhub.system.disk.operations` | `disk_reads_sec` | Disk Reads Sec ({drive}) | # | Number of read operations per second on the logical drive |
| `senhub.system.disk.operations` | `disk_writes_sec` | Disk Writes Sec ({drive}) | # | Number of write operations per second on the logical drive |
| `senhub.system.disk.io` | `disk_read_bytes_sec` | Disk Read Bytes Sec ({drive}) | bytes | Rate of data read from the logical drive in bytes per second |
| `senhub.system.disk.io` | `disk_write_bytes_sec` | Disk Write Bytes Sec ({drive}) | bytes | Rate of data written to the logical drive in bytes per second |
| `senhub.system.disk.queue_length` | `disk_queue_length` | Disk Queue Length ({drive}) | # | Number of outstanding I/O requests waiting in the disk queue |
| `system.filesystem.limit` | `fs_total_bytes` | Total Bytes ({mount_point}) | bytes | Total capacity of the filesystem in bytes |
| `system.filesystem.usage` | `fs_free_bytes` | Free Bytes ({mount_point}) | bytes | Free space available on the filesystem in bytes |
| `system.filesystem.usage` | `fs_used_bytes` | Used Bytes ({mount_point}) | bytes | Space currently consumed on the filesystem in bytes |
| `system.filesystem.usage` | `fs_available_bytes` | Available Bytes ({mount_point}) | bytes | Space available to non-root users on the filesystem in bytes |
| `system.filesystem.utilization` | `fs_used_percent` | Used Percent ({mount_point}) | % | Percentage of filesystem capacity currently in use |
| `senhub.system.filesystem.inode.limit` | `fs_inodes_total` | Inodes Total ({mount_point}) | # | Total number of inodes available on the filesystem |
| `senhub.system.filesystem.inode.usage` | `fs_inodes_free` | Inodes Free ({mount_point}) | # | Number of unused inodes available on the filesystem |
| `senhub.system.filesystem.inode.usage` | `fs_inodes_used` | Inodes Used ({mount_point}) | # | Number of inodes currently allocated on the filesystem |
| `senhub.system.filesystem.inode.utilization` | `fs_inodes_used_percent` | Inodes Used Percent ({mount_point}) | % | Percentage of total inodes currently in use on the filesystem |

<!-- schema:metrics:end -->
