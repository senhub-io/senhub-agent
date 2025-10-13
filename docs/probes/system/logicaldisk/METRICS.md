# LogicalDisk Metrics Complete Reference

This document provides a comprehensive reference for all LogicalDisk metrics collected by the SenHub Agent LogicalDisk probe across different operating systems.

## Table of Contents

- [Introduction](#introduction)
- [Windows Metrics](#windows-metrics)
- [Unix/Linux/macOS Metrics](#unixlinuxmacos-metrics)
- [Metric Tags](#metric-tags)
- [Calculation Details](#calculation-details)
- [Use Cases by Metric](#use-cases-by-metric)
- [Platform Comparison](#platform-comparison)

## Introduction

The LogicalDisk probe collects disk and filesystem performance metrics using platform-specific APIs:

- **Windows**: Performance Data Helper (PDH) counters from LogicalDisk object
- **Unix/Linux**: `/proc/mounts` for discovery, `syscall.Statfs` for metrics
- **macOS**: `df` command for discovery, `syscall.Statfs` for metrics

All metrics are collected at the configured interval (default: 30 seconds) and include standard tags for hostname, OS, and architecture, plus platform-specific tags for drive/mount identification.

## Windows Metrics

Windows metrics are collected via PDH counters from the LogicalDisk performance object.

### Disk Free Space (MB)

| Metric Name | Display Name | Type | Unit | Description |
|------------|--------------|------|------|-------------|
| `disk_free_mb` | Disk Free Space (MB) | Gauge | `MB` | Free disk space in megabytes per drive |

**Tags:** `hostname`, `os`, `arch`, `drive` (e.g., `drive=C:`)

**Example Value:** `102400` (100 GB free)

**Use Cases:**
- Monitor absolute free space
- Alert on low disk space (< 10 GB)
- Capacity planning for specific drives
- Database transaction log monitoring

**Alert Thresholds:**
- Warning: < 20 GB free
- Critical: < 10 GB free
- Database volumes: < 50 GB free

**PDH Counter:** `\LogicalDisk(C:)\Free Megabytes`

### Disk Free Space (Percent)

| Metric Name | Display Name | Type | Unit | Description |
|------------|--------------|------|------|-------------|
| `disk_free_percent` | Disk Free Space (%) | Gauge | `%` | Free disk space as percentage (0-100%) |

**Tags:** `hostname`, `os`, `arch`, `drive`

**Example Value:** `45.2` (45.2% free)

**Use Cases:**
- **Primary capacity monitoring metric**
- Normalized alerting across different drive sizes
- Capacity trending and forecasting
- SLA compliance monitoring

**Alert Thresholds:**
- Warning: < 20% free
- Critical: < 10% free
- Severe: < 5% free

**Calculation:**
```
disk_free_percent = (free_space / total_space) * 100
```

**PDH Counter:** `\LogicalDisk(C:)\% Free Space`

### Disk Reads Per Second

| Metric Name | Display Name | Type | Unit | Description |
|------------|--------------|------|------|-------------|
| `disk_reads_sec` | Disk Reads/sec | Gauge | `/s` | Number of read operations per second |

**Tags:** `hostname`, `os`, `arch`, `drive`

**Example Value:** `123.4` (123.4 IOPS read)

**Use Cases:**
- Monitor read I/O workload
- Identify read-intensive applications
- Validate storage performance
- Capacity planning for IOPS

**Typical Values:**
- **HDD (7200 RPM):** 80-120 IOPS
- **HDD (10K RPM):** 120-180 IOPS
- **HDD (15K RPM):** 180-250 IOPS
- **SSD (SATA):** 10,000-90,000 IOPS
- **SSD (NVMe):** 100,000-600,000 IOPS

**Alert Thresholds:**
- HDD: > 150 IOPS sustained = potential bottleneck
- SSD: > 80% of rated IOPS = monitor closely

**PDH Counter:** `\LogicalDisk(C:)\Disk Reads/sec`

### Disk Writes Per Second

| Metric Name | Display Name | Type | Unit | Description |
|------------|--------------|------|------|-------------|
| `disk_writes_sec` | Disk Writes/sec | Gauge | `/s` | Number of write operations per second |

**Tags:** `hostname`, `os`, `arch`, `drive`

**Example Value:** `87.6` (87.6 IOPS write)

**Use Cases:**
- Monitor write I/O workload
- Identify write-intensive applications
- Database transaction log monitoring
- SSD wear leveling estimation

**Typical Values:**
- Same as read IOPS for most storage
- May be lower on some RAID configurations
- Write-back cache can show higher burst values

**Alert Thresholds:**
- Combined read + write > storage IOPS capacity
- Sustained high writes on SSD = wear concern

**PDH Counter:** `\LogicalDisk(C:)\Disk Writes/sec`

### Disk Read Throughput

| Metric Name | Display Name | Type | Unit | Description |
|------------|--------------|------|------|-------------|
| `disk_read_bytes_sec` | Disk Read Bytes/sec | Gauge | `Bps` | Bytes read from disk per second |

**Tags:** `hostname`, `os`, `arch`, `drive`

**Example Value:** `5242880` (5 MB/s)

**Use Cases:**
- Monitor read throughput
- Identify bandwidth-intensive operations
- Validate storage performance
- Capacity planning for bandwidth

**Typical Values:**
- **HDD (7200 RPM):** 80-160 MB/s
- **HDD (10K RPM):** 140-210 MB/s
- **SSD (SATA):** 400-550 MB/s
- **SSD (NVMe Gen3):** 2000-3500 MB/s
- **SSD (NVMe Gen4):** 5000-7000 MB/s

**Alert Thresholds:**
- Sustained throughput > 80% of disk capacity
- Low throughput + high IOPS = small I/O pattern

**Conversion:**
```
MB/s = disk_read_bytes_sec / 1,048,576
GB/s = disk_read_bytes_sec / 1,073,741,824
```

**PDH Counter:** `\LogicalDisk(C:)\Disk Read Bytes/sec`

### Disk Write Throughput

| Metric Name | Display Name | Type | Unit | Description |
|------------|--------------|------|------|-------------|
| `disk_write_bytes_sec` | Disk Write Bytes/sec | Gauge | `Bps` | Bytes written to disk per second |

**Tags:** `hostname`, `os`, `arch`, `drive`

**Example Value:** `3145728` (3 MB/s)

**Use Cases:**
- Monitor write throughput
- Database write performance
- Backup operation monitoring
- Log file write patterns

**Typical Values:**
- Same as read throughput for most storage
- May be affected by RAID write penalties (RAID 5/6)
- Write-back cache improves burst performance

**Alert Thresholds:**
- Sustained throughput > 80% of disk capacity
- Combined read + write exceeds disk bandwidth

**Conversion:**
```
MB/s = disk_write_bytes_sec / 1,048,576
GB/s = disk_write_bytes_sec / 1,073,741,824
```

**PDH Counter:** `\LogicalDisk(C:)\Disk Write Bytes/sec`

### Disk Queue Length

| Metric Name | Display Name | Type | Unit | Description |
|------------|--------------|------|------|-------------|
| `disk_queue_length` | Current Disk Queue Length | Gauge | `#` | Number of I/O requests waiting in queue |

**Tags:** `hostname`, `os`, `arch`, `drive`

**Example Value:** `0.8` (average 0.8 requests queued)

**Use Cases:**
- **Primary disk bottleneck indicator**
- Detect storage performance issues
- Validate disk sizing and configuration
- Correlate with application latency

**Interpretation:**
```
Queue Length = Outstanding I/O requests waiting to be serviced

Typical values:
- 0-1: Optimal (no queueing)
- 1-2: Acceptable (minimal queueing)
- 2-4: Moderate load (monitor)
- > 4: High load (investigate)
- > 10: Severe bottleneck (critical)
```

**Alert Thresholds:**
- Warning: > 2 sustained (5 minutes)
- Critical: > 4 sustained (5 minutes)
- Emergency: > 10

**Causes of High Queue Length:**
- Insufficient IOPS capacity
- Slow disk hardware
- RAID performance issues
- Storage controller bottleneck
- Network storage latency

**Troubleshooting:**
```powershell
# Check disk queue in Performance Monitor
typeperf "\LogicalDisk(C:)\Current Disk Queue Length" -sc 10

# Identify I/O intensive processes
Get-Counter "\Process(*)\IO Read Bytes/sec"
Get-Counter "\Process(*)\IO Write Bytes/sec"
```

**PDH Counter:** `\LogicalDisk(C:)\Current Disk Queue Length`

## Unix/Linux/macOS Metrics

Unix-like systems use filesystem-based metrics collected via `syscall.Statfs`.

### Filesystem Total Capacity

| Metric Name | Display Name | Type | Unit | Description |
|------------|--------------|------|------|-------------|
| `fs_total_bytes` | Filesystem Total Bytes | Gauge | `bytes` | Total filesystem capacity in bytes |

**Tags:** `hostname`, `os`, `arch`, `mount_point`, `fs_type`, `device`

**Example Value:** `107374182400` (100 GB)

**Use Cases:**
- Filesystem capacity inventory
- Capacity planning baseline
- Validate provisioned storage
- SAN/NAS capacity tracking

**Conversion:**
```
GB = fs_total_bytes / 1,073,741,824
TB = fs_total_bytes / 1,099,511,627,776
```

**Source:** `syscall.Statfs_t.Blocks * syscall.Statfs_t.Bsize`

### Filesystem Free Space

| Metric Name | Display Name | Type | Unit | Description |
|------------|--------------|------|------|-------------|
| `fs_free_bytes` | Filesystem Free Bytes | Gauge | `bytes` | Free space including reserved blocks |

**Tags:** `hostname`, `os`, `arch`, `mount_point`, `fs_type`, `device`

**Example Value:** `48318382080` (45 GB free)

**Use Cases:**
- Monitor absolute free space
- Capacity planning calculations
- Reserved space tracking
- Root filesystem monitoring

**Note:** Includes reserved blocks (typically 5% on ext filesystems) reserved for root user.

**Conversion:**
```
GB = fs_free_bytes / 1,073,741,824
```

**Source:** `syscall.Statfs_t.Bfree * syscall.Statfs_t.Bsize`

### Filesystem Used Space

| Metric Name | Display Name | Type | Unit | Description |
|------------|--------------|------|------|-------------|
| `fs_used_bytes` | Filesystem Used Bytes | Gauge | `bytes` | Used space in bytes |

**Tags:** `hostname`, `os`, `arch`, `mount_point`, `fs_type`, `device`

**Example Value:** `59055800320` (55 GB used)

**Use Cases:**
- Track space consumption
- Growth rate analysis
- Application data monitoring
- Compliance and quota enforcement

**Calculation:**
```
fs_used_bytes = fs_total_bytes - fs_free_bytes
```

**Conversion:**
```
GB = fs_used_bytes / 1,073,741,824
```

### Filesystem Available Space

| Metric Name | Display Name | Type | Unit | Description |
|------------|--------------|------|------|-------------|
| `fs_available_bytes` | Filesystem Available Bytes | Gauge | `bytes` | Space available to non-root users |

**Tags:** `hostname`, `os`, `arch`, `mount_point`, `fs_type`, `device`

**Example Value:** `45318382080` (42 GB available)

**Use Cases:**
- **Primary capacity metric for non-root applications**
- Application-specific alerting
- User quota monitoring
- Prevent non-root application failures

**Difference from Free Space:**
- `fs_free_bytes` includes reserved blocks
- `fs_available_bytes` excludes reserved blocks
- Only root can use reserved space

**Example (ext4 with 5% reserved):**
```
Total: 100 GB
Free: 10 GB (including 5 GB reserved)
Available: 5 GB (for non-root users)
```

**Alert Thresholds:**
- Base alerts on `fs_available_bytes` for non-root applications
- Base alerts on `fs_free_bytes` for root-only services

**Source:** `syscall.Statfs_t.Bavail * syscall.Statfs_t.Bsize`

### Filesystem Used Percent

| Metric Name | Display Name | Type | Unit | Description |
|------------|--------------|------|------|-------------|
| `fs_used_percent` | Filesystem Used (%) | Gauge | `%` | Percentage of filesystem used (0-100%) |

**Tags:** `hostname`, `os`, `arch`, `mount_point`, `fs_type`, `device`

**Example Value:** `55.0` (55% used)

**Use Cases:**
- **Primary capacity monitoring metric**
- Normalized alerting across filesystem sizes
- Capacity trending and forecasting
- Dashboard visualization

**Calculation:**
```
fs_used_percent = (fs_used_bytes / fs_total_bytes) * 100
```

**Alert Thresholds:**
- Warning: > 80% used
- Critical: > 90% used
- Emergency: > 95% used

**Platform-Specific Behavior:**
- Can exceed 100% on some filesystems (root can use reserved space)
- Excludes reserved blocks from calculation

### Inode Total Count

| Metric Name | Display Name | Type | Unit | Description |
|------------|--------------|------|------|-------------|
| `fs_inodes_total` | Filesystem Inodes Total | Gauge | `#` | Total number of inodes (file entries) |

**Tags:** `hostname`, `os`, `arch`, `mount_point`, `fs_type`, `device`

**Example Value:** `6553600` (6.5 million inodes)

**Use Cases:**
- Inode capacity planning
- Small file workload analysis
- Filesystem sizing validation
- Application architecture review

**What are Inodes:**
```
Inode = Filesystem data structure representing a file or directory
- One inode per file/directory
- Fixed at filesystem creation (most filesystems)
- Running out of inodes prevents file creation even with free space
```

**Typical Inode Ratios:**
- Default ext4: 1 inode per 16 KB
- High file count: 1 inode per 4 KB
- Large files: 1 inode per 64 KB

**Source:** `syscall.Statfs_t.Files`

### Inode Free Count

| Metric Name | Display Name | Type | Unit | Description |
|------------|--------------|------|------|-------------|
| `fs_inodes_free` | Filesystem Inodes Free | Gauge | `#` | Number of free inodes available |

**Tags:** `hostname`, `os`, `arch`, `mount_point`, `fs_type`, `device`

**Example Value:** `5242880` (5.2 million free)

**Use Cases:**
- Monitor inode availability
- Prevent inode exhaustion
- Small file workload monitoring
- Mail server filesystem monitoring

**Alert Thresholds:**
- Warning: < 20% free
- Critical: < 10% free

**Source:** `syscall.Statfs_t.Ffree`

### Inode Used Count

| Metric Name | Display Name | Type | Unit | Description |
|------------|--------------|------|------|-------------|
| `fs_inodes_used` | Filesystem Inodes Used | Gauge | `#` | Number of inodes in use |

**Tags:** `hostname`, `os`, `arch`, `mount_point`, `fs_type`, `device`

**Example Value:** `1310720` (1.3 million used)

**Use Cases:**
- Track inode consumption
- Growth rate analysis for file count
- Application file creation patterns
- Temporary file cleanup validation

**Calculation:**
```
fs_inodes_used = fs_inodes_total - fs_inodes_free
```

### Inode Used Percent

| Metric Name | Display Name | Type | Unit | Description |
|------------|--------------|------|------|-------------|
| `fs_inodes_used_percent` | Filesystem Inodes Used (%) | Gauge | `%` | Percentage of inodes used (0-100%) |

**Tags:** `hostname`, `os`, `arch`, `mount_point`, `fs_type`, `device`

**Example Value:** `20.0` (20% inodes used)

**Use Cases:**
- **Primary inode monitoring metric**
- Prevent inode exhaustion
- Small file workload alerting
- Capacity planning for file count

**Calculation:**
```
fs_inodes_used_percent = (fs_inodes_used / fs_inodes_total) * 100
```

**Alert Thresholds:**
- Warning: > 80% used
- Critical: > 90% used
- Emergency: > 95% used

**Inode Exhaustion Symptoms:**
```bash
# Disk has space but can't create files
$ df -h /data
Filesystem      Size  Used Avail Use% Mounted on
/dev/sda1       100G   50G   50G  50% /data

$ df -i /data
Filesystem     Inodes  IUsed  IFree IUse% Mounted on
/dev/sda1      6.5M    6.5M      0  100% /data  ← Problem!

$ touch /data/test.txt
touch: cannot touch '/data/test.txt': No space left on device
```

**Common Causes:**
- Mail servers with many small files
- Cache directories with many temp files
- Log rotation creating many small logs
- Application creating many small files

**Troubleshooting:**
```bash
# Find directories with high file counts
find /data -xdev -type d -exec sh -c 'echo $(find "$1" -maxdepth 1 | wc -l) "$1"' _ {} \; | sort -rn | head -20

# Clean up old temp files
find /tmp -type f -mtime +7 -delete
```

## Metric Tags

All LogicalDisk metrics include tags for filtering and aggregation.

### Standard Tags (All Platforms)

| Tag Name | Type | Description | Example |
|----------|------|-------------|---------|
| `hostname` | string | System hostname | `web-server-01` |
| `os` | string | Operating system | `linux`, `windows`, `darwin` |
| `arch` | string | CPU architecture | `amd64`, `arm64` |
| `probe_name` | string | Probe identifier | `logicaldisk` |

### Windows-Specific Tags

| Tag Name | Type | Description | Example |
|----------|------|-------------|---------|
| `drive` | string | Drive letter | `C:`, `D:`, `E:` |

### Unix/Linux/macOS-Specific Tags

| Tag Name | Type | Description | Example |
|----------|------|-------------|---------|
| `mount_point` | string | Filesystem mount path | `/`, `/home`, `/var` |
| `fs_type` | string | Filesystem type | `ext4`, `xfs`, `apfs` |
| `device` | string | Block device path | `/dev/sda1`, `/dev/disk1s1` |

### Tag Usage Examples

**Windows: Filter by drive letter:**
```promql
disk_free_percent{drive="C:"}
disk_queue_length{drive="D:"}
```

**Unix/Linux: Filter by mount point:**
```promql
fs_used_percent{mount_point="/"}
fs_inodes_used_percent{mount_point="/var"}
```

**Unix/Linux: Filter by filesystem type:**
```promql
fs_free_bytes{fs_type="ext4"}
fs_used_percent{fs_type="xfs"}
```

**Cross-platform: Filter by OS:**
```promql
disk_free_percent{os="windows"}
fs_used_percent{os="linux"}
```

**Aggregate across all filesystems:**
```promql
# Unix/Linux total used space
sum(fs_used_bytes{hostname="server01"})

# Windows total free space
sum(disk_free_mb{hostname="server01"})
```

## Calculation Details

### Windows Capacity Metrics

Windows metrics are reported directly by PDH counters:
- `disk_free_mb`: Megabytes free (no calculation)
- `disk_free_percent`: Percentage free (no calculation)

### Unix/Linux Capacity Metrics

Unix metrics are calculated from `syscall.Statfs` structure:

```go
type Statfs_t struct {
    Blocks  uint64  // Total blocks in filesystem
    Bfree   uint64  // Free blocks (including reserved)
    Bavail  uint64  // Free blocks available to non-root
    Files   uint64  // Total inodes
    Ffree   uint64  // Free inodes
    Bsize   int64   // Block size in bytes
    // ...
}
```

**Capacity Calculations:**
```go
fs_total_bytes = stat.Blocks * uint64(stat.Bsize)
fs_free_bytes = stat.Bfree * uint64(stat.Bsize)
fs_available_bytes = stat.Bavail * uint64(stat.Bsize)
fs_used_bytes = fs_total_bytes - fs_free_bytes
fs_used_percent = (float32(fs_used_bytes) / float32(fs_total_bytes)) * 100
```

**Inode Calculations:**
```go
fs_inodes_total = stat.Files
fs_inodes_free = stat.Ffree
fs_inodes_used = fs_inodes_total - fs_inodes_free
fs_inodes_used_percent = (float32(fs_inodes_used) / float32(fs_inodes_total)) * 100
```

### Windows I/O Metrics

Windows I/O metrics are instantaneous rates from PDH counters:
- `disk_reads_sec`: Reads per second (no calculation)
- `disk_writes_sec`: Writes per second (no calculation)
- `disk_read_bytes_sec`: Bytes per second (no calculation)
- `disk_write_bytes_sec`: Bytes per second (no calculation)
- `disk_queue_length`: Current queue depth (no calculation)

**Note:** PDH automatically calculates rates from counter samples.

### Reserved Blocks (Unix/Linux)

Most Unix/Linux filesystems reserve space for root user:

```
Reserved % = ((fs_free_bytes - fs_available_bytes) / fs_total_bytes) * 100

Typical values:
- ext2/ext3/ext4: 5% (default)
- xfs: 0% (no reserved space by default)
- btrfs: 0% (no reserved space by default)
```

**Example (ext4 with 5% reserved):**
```
Total: 100 GB
Free: 10 GB
Available: 5 GB
Reserved: 5 GB (10 - 5)
Reserved %: 5%
```

**Change reserved space (ext4):**
```bash
# Check current reserved blocks
tune2fs -l /dev/sda1 | grep -i reserved

# Set reserved to 2%
sudo tune2fs -m 2 /dev/sda1

# Set reserved to 0 (not recommended for root filesystem)
sudo tune2fs -m 0 /dev/sda1
```

## Use Cases by Metric

### Capacity Monitoring

| Goal | Primary Metrics (Windows) | Primary Metrics (Unix/Linux) | Alert Threshold |
|------|---------------------------|------------------------------|----------------|
| Out-of-space prevention | `disk_free_percent` | `fs_used_percent`, `fs_available_bytes` | > 90% used |
| Capacity planning | `disk_free_mb` | `fs_used_bytes`, `fs_total_bytes` | Trend analysis |
| Inode exhaustion | N/A | `fs_inodes_used_percent` | > 90% used |

### I/O Performance Monitoring

| Goal | Primary Metrics | Alert Threshold |
|------|----------------|----------------|
| Disk bottleneck detection | `disk_queue_length` | > 2 sustained |
| IOPS monitoring | `disk_reads_sec`, `disk_writes_sec` | > storage capacity |
| Throughput monitoring | `disk_read_bytes_sec`, `disk_write_bytes_sec` | > storage bandwidth |

### Application-Specific Monitoring

| Application Type | Key Metrics | Thresholds |
|-----------------|-------------|------------|
| Database (Windows) | `disk_queue_length`, `disk_writes_sec` | Queue > 2, IOPS > disk capacity |
| Database (Unix) | `fs_used_percent`, `fs_available_bytes` | > 80% used |
| Web Server (Unix) | `fs_inodes_used_percent` | > 80% (temp files) |
| Mail Server (Unix) | `fs_inodes_used_percent` | > 80% (many small files) |
| Backup Volumes | `fs_used_percent`, `disk_free_mb` | > 90% used |

## Platform Comparison

### Equivalent Metrics Across Platforms

| Concept | Windows | Unix/Linux/macOS | Notes |
|---------|---------|------------------|-------|
| Free Space (MB) | `disk_free_mb` | `fs_free_bytes / 1048576` | Different units |
| Free Space (%) | `disk_free_percent` | `100 - fs_used_percent` | Inverse metrics |
| Disk Identifier | `drive` tag (C:, D:) | `mount_point` tag (/, /home) | Different tagging |
| I/O Operations | `disk_reads_sec`, `disk_writes_sec` | Not available | Windows only |
| Throughput | `disk_read_bytes_sec`, `disk_write_bytes_sec` | Not available | Windows only |
| Queue Depth | `disk_queue_length` | Not available | Windows only |
| Inodes | Not available | `fs_inodes_*` metrics | Unix/Linux only |

### Feature Comparison

| Feature | Windows | Unix/Linux | macOS |
|---------|---------|-----------|-------|
| Capacity Metrics | Yes (MB, %) | Yes (bytes, %) | Yes (bytes, %) |
| I/O Metrics | Yes (IOPS, throughput, queue) | No | No |
| Inode Metrics | No | Yes | Yes |
| Drive Filtering | Yes (include/exclude patterns) | Automatic | Automatic |
| Real-time I/O | Yes (PDH counters) | No | No |
| Filesystem Type | Not exposed | Yes (`fs_type` tag) | Yes (`fs_type` tag) |

### Cross-Platform Alerting

**Capacity Alerts:**
```yaml
# Windows: Alert on low free space percentage
- alert: WindowsLowDiskSpace
  expr: disk_free_percent < 10
  for: 5m

# Unix/Linux: Alert on high used space percentage
- alert: UnixLowDiskSpace
  expr: fs_used_percent > 90
  for: 5m

# Unix/Linux: Alert on low available space (absolute)
- alert: UnixLowAvailableSpace
  expr: fs_available_bytes < 10737418240  # 10 GB
  for: 5m
```

**I/O Alerts (Windows Only):**
```yaml
# High disk queue (bottleneck indicator)
- alert: WindowsHighDiskQueue
  expr: disk_queue_length > 2
  for: 5m

# High IOPS (approaching disk capacity)
- alert: WindowsHighIOPS
  expr: (disk_reads_sec + disk_writes_sec) > 200  # HDD threshold
  for: 5m
```

**Inode Alerts (Unix/Linux Only):**
```yaml
# High inode usage
- alert: UnixHighInodeUsage
  expr: fs_inodes_used_percent > 80
  for: 5m
```

## Monitoring Best Practices

### Alert Configurations

**Capacity Alerts:**
```yaml
# Windows disk space alerts
- alert: WindowsDiskSpaceWarning
  expr: disk_free_percent < 20
  for: 10m

- alert: WindowsDiskSpaceCritical
  expr: disk_free_percent < 10
  for: 5m

# Unix/Linux disk space alerts
- alert: UnixDiskSpaceWarning
  expr: fs_used_percent > 80
  for: 10m

- alert: UnixDiskSpaceCritical
  expr: fs_used_percent > 90
  for: 5m

# Unix/Linux inode alerts
- alert: UnixInodeWarning
  expr: fs_inodes_used_percent > 80
  for: 10m

- alert: UnixInodeCritical
  expr: fs_inodes_used_percent > 90
  for: 5m
```

**I/O Performance Alerts (Windows):**
```yaml
# Disk bottleneck detection
- alert: WindowsDiskBottleneck
  expr: disk_queue_length > 2
  for: 5m

# High IOPS (HDD)
- alert: WindowsHighIOPSHDD
  expr: (disk_reads_sec + disk_writes_sec) > 200
  for: 5m

# High IOPS (SSD)
- alert: WindowsHighIOPSSSD
  expr: (disk_reads_sec + disk_writes_sec) > 10000
  for: 5m
```

### Dashboard Panels

**Essential LogicalDisk Panels:**
1. **Disk Usage Overview** - Gauge of `disk_free_percent` / `fs_used_percent` per drive/mount
2. **Capacity Trend** - Time series of capacity usage over time
3. **I/O Operations** (Windows) - Time series of `disk_reads_sec` + `disk_writes_sec`
4. **Disk Throughput** (Windows) - Time series of `disk_read_bytes_sec` + `disk_write_bytes_sec`
5. **Disk Queue** (Windows) - Time series of `disk_queue_length`
6. **Inode Usage** (Unix/Linux) - Gauge of `fs_inodes_used_percent` per mount
7. **Filesystem Table** - Table showing all mounts with capacity and inode metrics

### Collection Intervals

| Metric Type | Interval | Reason |
|------------|----------|--------|
| Capacity metrics | 60-300s | Changes slowly |
| I/O metrics (Windows) | 30-60s | Capture patterns |
| Critical volumes | 10-30s | Rapid detection |

## Advanced Analysis

### Capacity Growth Rate

**Calculate daily growth rate:**
```promql
# Unix/Linux
rate(fs_used_bytes[24h])

# Windows (convert MB to bytes)
rate(disk_free_mb[24h]) * -1048576
```

**Predict time to full:**
```
Days until full = (free_space / daily_growth_rate)
```

### I/O Pattern Analysis (Windows)

**Average I/O size:**
```
Avg Read Size = disk_read_bytes_sec / disk_reads_sec
Avg Write Size = disk_write_bytes_sec / disk_writes_sec

Interpretation:
- < 4 KB: Random small I/O (OLTP, web server)
- 4-64 KB: Mixed workload
- > 64 KB: Sequential large I/O (data warehouse, backup)
```

**Read/Write Ratio:**
```
Read Ratio = disk_reads_sec / (disk_reads_sec + disk_writes_sec)
Write Ratio = disk_writes_sec / (disk_reads_sec + disk_writes_sec)

Typical patterns:
- Read-heavy: > 70% reads (web server, cache)
- Write-heavy: > 70% writes (logging, data ingestion)
- Balanced: 40-60% each (database OLTP)
```

### Inode Exhaustion Risk (Unix/Linux)

**Calculate average file size:**
```
Avg File Size = fs_used_bytes / fs_inodes_used

Interpretation:
- < 4 KB: Many small files (high inode usage risk)
- 4 KB - 1 MB: Mixed file sizes
- > 1 MB: Large files (low inode usage risk)
```

**Inode exhaustion prediction:**
```
Inodes Growth Rate = rate(fs_inodes_used[24h])
Days until inode exhaustion = fs_inodes_free / Inodes Growth Rate / 86400
```

### Filesystem Efficiency (Unix/Linux)

**Reserved space calculation:**
```
Reserved Space = fs_free_bytes - fs_available_bytes
Reserved Percent = (Reserved Space / fs_total_bytes) * 100
```

**Effective capacity:**
```
Effective Capacity = fs_total_bytes - Reserved Space
Usage % (effective) = (fs_used_bytes / Effective Capacity) * 100
```

## Related Documentation

- [LogicalDisk Probe README](./README.md) - Configuration and quick start
- [System Monitoring Guide](../../guides/system-monitoring.md) - Comprehensive system monitoring
- [Performance Tuning](../../guides/performance-tuning.md) - Optimize agent performance
- [Troubleshooting Guide](../../troubleshooting/) - Common issues and solutions
