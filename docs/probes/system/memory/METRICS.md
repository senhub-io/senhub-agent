# Memory Metrics Complete Reference

This document provides a comprehensive reference for all memory metrics collected by the SenHub Agent Memory probe across different operating systems.

## Table of Contents

- [Introduction](#introduction)
- [Cross-Platform Metrics](#cross-platform-metrics)
- [Unix/Linux/macOS Metrics](#unixlinuxmacos-metrics)
- [Windows Metrics](#windows-metrics)
- [Swap Memory Metrics](#swap-memory-metrics)
- [Metric Tags](#metric-tags)
- [Calculation Details](#calculation-details)
- [Use Cases by Metric](#use-cases-by-metric)

## Introduction

The Memory probe collects memory performance metrics using platform-specific APIs:

- **Windows**: Performance Data Helper (PDH) counters
- **Linux/Unix/macOS**: gopsutil library (wraps `/proc/meminfo`, `vm_stat`, etc.)

All metrics are collected at the configured interval (default: 30 seconds) and include standard tags for hostname, OS, and architecture.

## Cross-Platform Metrics

These metrics are available on all supported platforms (Windows, Linux, macOS, BSD).

### Total Memory

| Metric Name | Display Name | Type | Unit | Description |
|------------|--------------|------|------|-------------|
| `memory_total` | Memory Total | Gauge | `bytes` | Total installed physical memory |

**Tags:** `hostname`, `os`, `arch`

**Example Value:** `17179869184` (16 GB)

**Use Cases:**
- Verify installed memory capacity
- Calculate memory usage percentages
- Capacity planning baseline
- Hardware inventory

**Platform-Specific Details:**
- **Windows**: `Commit Limit` from PDH (total available including page file)
- **Unix/Linux**: Total from `/proc/meminfo` (MemTotal)
- **macOS**: `hw.memsize` from sysctl

**Note:** Windows reports commit limit (physical + page file), while Unix reports physical memory only.

### Available Memory

| Metric Name | Display Name | Type | Unit | Description |
|------------|--------------|------|------|-------------|
| `memory_available` | Memory Available | Gauge | `bytes` | Memory available for applications without swapping |

**Tags:** `hostname`, `os`, `arch`

**Example Value:** `5368709120` (5 GB)

**Use Cases:**
- **Primary indicator of memory pressure**
- Determine if applications can allocate memory
- Trigger alerts for low memory
- Capacity planning

**Platform-Specific Details:**
- **Windows**: `Available Bytes` from PDH
- **Unix/Linux**: `MemAvailable` from `/proc/meminfo` (kernel 3.14+)
- **macOS**: Free + inactive memory

**Calculation (Linux):**
```
Available = Free + Buffers + Cached - (Active + Dirty + Writeback)
```

**Low Memory Thresholds:**
- `< 10%`: Critical memory pressure
- `10-20%`: Warning level
- `> 20%`: Healthy

### Memory Used Percentage

| Metric Name | Display Name | Type | Unit | Description |
|------------|--------------|------|------|-------------|
| `memory_used_percent` | Memory Used | Gauge | `%` | Percentage of memory in use (0-100%) |

**Tags:** `hostname`, `os`, `arch`

**Example Value:** `68.5` (68.5% used)

**Use Cases:**
- Quick memory health check
- Alert on high memory usage
- Trending memory consumption
- Capacity planning

**Calculation:**
```
Used % = ((Total - Available) / Total) * 100
```

**Alert Thresholds:**
- `< 70%`: Normal operation
- `70-85%`: Monitor closely
- `85-95%`: High memory usage
- `> 95%`: Critical - applications may fail

## Unix/Linux/macOS Metrics

These metrics are specific to Unix-like operating systems.

### Memory Used

| Metric Name | Display Name | Type | Unit | Description |
|------------|--------------|------|------|-------------|
| `memory_used` | Memory Used | Gauge | `bytes` | Memory actively used by processes and kernel |

**Tags:** `hostname`, `os`, `arch`

**Example Value:** `11744927744` (10.9 GB)

**Use Cases:**
- Track actual memory consumption
- Identify memory allocation trends
- Correlate with application activity
- Capacity planning

**Calculation (Linux):**
```
Used = Total - Free - Buffers - Cached
```

**Note:** Does not include cached memory, which is reclaimable.

### Memory Free

| Metric Name | Display Name | Type | Unit | Description |
|------------|--------------|------|------|-------------|
| `memory_free` | Memory Free | Gauge | `bytes` | Completely unused memory (not cached or buffered) |

**Tags:** `hostname`, `os`, `arch`

**Example Value:** `2147483648` (2 GB)

**Use Cases:**
- Identify truly unused memory
- Understand cache vs. free memory
- System memory availability

**Important:** Low free memory is normal on Linux/Unix - the kernel uses unused memory for caching. Check `memory_available` instead for actual availability.

**Platform Differences:**
- **Linux**: `MemFree` from `/proc/meminfo`
- **macOS**: Free pages × page size
- **BSD**: Similar to Linux

### Memory Cached

| Metric Name | Display Name | Type | Unit | Description |
|------------|--------------|------|------|-------------|
| `memory_cached` | Memory Cached | Gauge | `bytes` | Memory used for page cache (reclaimable) |

**Tags:** `hostname`, `os`, `arch`

**Example Value:** `8589934592` (8 GB)

**Use Cases:**
- Monitor page cache effectiveness
- Identify file I/O patterns
- Understand cache vs. application memory
- Optimize cache utilization

**Purpose:**
The kernel caches frequently accessed files in RAM to avoid disk I/O. This memory is automatically reclaimed when applications need it.

**Cache Behavior:**
```
High cache = Good (kernel optimizing disk access)
Growing cache = Active file I/O
Shrinking cache = Memory pressure (being reclaimed)
```

**Platform Notes:**
- **Linux**: `Cached` from `/proc/meminfo`
- **macOS**: File cache + active/inactive pages
- **BSD**: Buffer cache

### Memory Buffers

| Metric Name | Display Name | Type | Unit | Description |
|------------|--------------|------|------|-------------|
| `memory_buffers` | Memory Buffers | Gauge | `bytes` | Memory used for kernel buffers (reclaimable) |

**Tags:** `hostname`, `os`, `arch`

**Example Value:** `536870912` (512 MB)

**Use Cases:**
- Monitor kernel buffer usage
- Identify I/O buffering patterns
- Tune kernel buffer sizes
- Troubleshoot I/O performance

**Purpose:**
Kernel-level buffers for block device I/O, separate from page cache.

**Typical Values:**
- Desktop: 100-500 MB
- Server: 500 MB - 2 GB
- High I/O: 2-8 GB

**Platform Notes:**
- **Linux**: `Buffers` from `/proc/meminfo`
- **macOS**: Included in cached memory
- **BSD**: Similar to Linux

## Windows Metrics

These metrics are specific to Windows operating systems.

### Memory Committed

| Metric Name | Display Name | Type | Unit | Description |
|------------|--------------|------|------|-------------|
| `memory_committed` | Memory Committed | Gauge | `bytes` | Total committed virtual memory (physical + page file) |

**Tags:** `hostname`, `os`, `arch`

**Example Value:** `12884901888` (12 GB)

**Use Cases:**
- Monitor virtual memory usage
- Detect memory overcommitment
- Identify virtual memory leaks
- Capacity planning for page file

**Windows Memory Model:**
```
Committed = Memory promised to applications
Physical = Actual RAM backing committed memory
Page File = Disk space backing committed memory

Committed can exceed Physical (uses page file)
```

**Alert Thresholds:**
```
Committed / Commit Limit:
- < 80%: Normal
- 80-90%: Monitor closely
- > 90%: Increase RAM or page file
```

**Related to:** `memory_total` (commit limit)

### Modified Page List

| Metric Name | Display Name | Type | Unit | Description |
|------------|--------------|------|------|-------------|
| `memory_modified_page_list` | Modified Page List | Gauge | `bytes` | Memory pages modified but not yet written to disk |

**Tags:** `hostname`, `os`, `arch`

**Example Value:** `1073741824` (1 GB)

**Use Cases:**
- Monitor write-back cache
- Identify dirty page buildup
- Troubleshoot storage performance
- Detect write bottlenecks

**Purpose:**
Pages that have been modified in RAM but not yet flushed to disk. High values indicate write-back pressure.

**Typical Values:**
- Normal: 100-500 MB
- High I/O: 500 MB - 2 GB
- Write bottleneck: > 2 GB

**Investigation:**
High modified page list suggests:
- Slow disk write performance
- Storage bottleneck
- Insufficient disk throughput
- Write-intensive workload

### Non-Paged Pool

| Metric Name | Display Name | Type | Unit | Description |
|------------|--------------|------|------|-------------|
| `memory_nonpaged_pool` | Non-Paged Pool | Gauge | `bytes` | Kernel memory that cannot be paged to disk |

**Tags:** `hostname`, `os`, `arch`

**Example Value:** `536870912` (512 MB)

**Use Cases:**
- Monitor kernel memory usage
- Detect kernel memory leaks
- Troubleshoot driver issues
- Identify kernel resource exhaustion

**Purpose:**
Critical kernel memory that must always remain in RAM (device drivers, kernel data structures).

**Typical Values:**
- Desktop: 100-300 MB
- Server: 300 MB - 1 GB
- High load: 1-2 GB

**Warning Signs:**
- Continuous growth: Kernel memory leak
- > 2 GB: Investigate drivers
- Near limit: System instability possible

**Limit:** Windows has a hard limit (~75% of RAM or 2 GB, whichever is less)

### Paged Pool

| Metric Name | Display Name | Type | Unit | Description |
|------------|--------------|------|------|-------------|
| `memory_paged_pool` | Paged Pool | Gauge | `bytes` | Kernel memory that can be paged to disk |

**Tags:** `hostname`, `os`, `arch`

**Example Value:** `1073741824` (1 GB)

**Use Cases:**
- Monitor kernel memory allocation
- Detect kernel memory pressure
- Troubleshoot system service issues
- Identify resource exhaustion

**Purpose:**
Less critical kernel memory that can be paged to disk if needed.

**Typical Values:**
- Desktop: 200-500 MB
- Server: 500 MB - 2 GB
- High load: 2-4 GB

**Alert Thresholds:**
- < 2 GB: Normal
- 2-3 GB: Monitor
- > 3 GB: Investigate

### System Cache

| Metric Name | Display Name | Type | Unit | Description |
|------------|--------------|------|------|-------------|
| `memory_cache` | System Cache | Gauge | `bytes` | Memory used for system file cache |

**Tags:** `hostname`, `os`, `arch`

**Example Value:** `4294967296` (4 GB)

**Use Cases:**
- Monitor file cache effectiveness
- Understand cache vs. application memory
- Optimize cache utilization
- Identify file I/O patterns

**Purpose:**
Windows equivalent of Linux page cache - caches files to avoid disk I/O.

**Cache Behavior:**
- Grows with file I/O activity
- Shrinks under memory pressure
- Dynamically managed by Windows

**Optimization:**
High cache = Good for file-intensive workloads
Low cache = More memory for applications

### Page Faults

| Metric Name | Display Name | Type | Unit | Description |
|------------|--------------|------|------|-------------|
| `memory_page_faults` | Page Faults/sec | Counter | `/s` | Page faults per second (all types) |

**Tags:** `hostname`, `os`, `arch`

**Example Value:** `1234.56` (faults/second)

**Use Cases:**
- **Primary indicator of memory pressure**
- Detect insufficient RAM
- Identify memory access patterns
- Correlate with performance issues

**Page Fault Types:**
```
Soft Faults: Data in RAM (fast, normal)
Hard Faults: Data must be read from disk (slow, problematic)

This metric = Soft + Hard faults
```

**Typical Values:**
- Idle: < 500/s
- Normal: 500-2000/s
- High: 2000-5000/s
- Excessive: > 5000/s

**Investigation:**
High page faults + high `memory_pages_input` = RAM shortage

### Pages Input

| Metric Name | Display Name | Type | Unit | Description |
|------------|--------------|------|------|-------------|
| `memory_pages_input` | Pages Input/sec | Counter | `/s` | Pages read from disk per second (hard faults) |

**Tags:** `hostname`, `os`, `arch`

**Example Value:** `567.89` (pages/second)

**Use Cases:**
- **Detect memory shortage**
- Identify paging activity
- Measure hard fault rate
- Correlate with disk I/O

**Critical Metric:**
High pages input = System is paging from disk = Performance degradation

**Typical Values:**
- Normal: < 10/s
- Moderate paging: 10-100/s
- Heavy paging: 100-1000/s
- Thrashing: > 1000/s

**Page Size:** Windows uses 4 KB pages
- 100 pages/s = 400 KB/s from disk
- 1000 pages/s = 4 MB/s from disk

**Alert Threshold:**
- `> 100 pages/s` sustained: Add RAM

### Pages Output

| Metric Name | Display Name | Type | Unit | Description |
|------------|--------------|------|------|-------------|
| `memory_pages_output` | Pages Output/sec | Counter | `/s` | Pages written to disk per second |

**Tags:** `hostname`, `os`, `arch`

**Example Value:** `234.56` (pages/second)

**Use Cases:**
- Monitor page file write activity
- Detect memory reclamation
- Identify paging patterns
- Troubleshoot performance

**Purpose:**
Pages evicted from RAM and written to page file.

**Typical Values:**
- Normal: < 10/s
- Moderate: 10-100/s
- High: > 100/s

**Note:** Some output is normal during memory pressure, but sustained high values indicate insufficient RAM.

### Page File Usage

| Metric Name | Display Name | Type | Unit | Description |
|------------|--------------|------|------|-------------|
| `pagefile_usage` | Page File Usage | Gauge | `%` | Current page file usage percentage |

**Tags:** `hostname`, `os`, `arch`

**Example Value:** `25.5` (25.5% used)

**Use Cases:**
- Monitor page file consumption
- Detect memory pressure
- Capacity planning for page file
- Alert on high page file usage

**Alert Thresholds:**
- `< 50%`: Normal
- `50-75%`: Monitor closely
- `75-90%`: Consider increasing RAM
- `> 90%`: Critical - increase RAM or page file

**Interpretation:**
```
Low usage: System has adequate RAM
Growing usage: Memory pressure
High usage: Insufficient RAM
```

### Page File Usage Peak

| Metric Name | Display Name | Type | Unit | Description |
|------------|--------------|------|------|-------------|
| `pagefile_usage_peak` | Page File Usage Peak | Gauge | `%` | Peak page file usage since boot |

**Tags:** `hostname`, `os`, `arch`

**Example Value:** `42.3` (42.3% peak)

**Use Cases:**
- Identify peak memory usage
- Capacity planning
- Size page file appropriately
- Detect memory usage patterns

**Purpose:**
Historical peak to understand maximum memory pressure experienced.

**Analysis:**
```
Peak << Current: Memory usage was higher in the past
Peak ≈ Current: System at or near peak usage
Peak > 90%: Page file may have been too small
```

## Swap Memory Metrics

Available on all platforms, swap metrics monitor virtual memory usage.

### Swap Total

| Metric Name | Display Name | Type | Unit | Description |
|------------|--------------|------|------|-------------|
| `swap_total` | Swap Total | Gauge | `bytes` | Total swap space available |

**Tags:** `hostname`, `os`, `arch`

**Example Value:** `4294967296` (4 GB)

**Use Cases:**
- Verify swap configuration
- Capacity planning
- Calculate swap usage percentages
- System inventory

**Platform-Specific:**
- **Linux**: Swap partitions/files from `/proc/swaps`
- **macOS**: Dynamic swap file pool
- **Windows**: Page file size (`pagefile.sys`)

**Recommendations:**
```
Desktop: RAM × 1 (e.g., 16 GB RAM = 16 GB swap)
Server: RAM × 0.5 to 1
High RAM: Fixed amount (16-32 GB)
```

### Swap Used

| Metric Name | Display Name | Type | Unit | Description |
|------------|--------------|------|------|-------------|
| `swap_used` | Swap Used | Gauge | `bytes` | Amount of swap space in use |

**Tags:** `hostname`, `os`, `arch`

**Example Value:** `536870912` (512 MB)

**Use Cases:**
- Monitor swap activity
- Detect memory pressure
- Identify performance degradation
- Alert on excessive swapping

**Warning Signs:**
```
Growing swap: Memory pressure
High swap + low RAM: Add RAM
Swap decreasing: Memory pressure relieved
```

**Performance Impact:**
Swap I/O is ~1000x slower than RAM access.

### Swap Free

| Metric Name | Display Name | Type | Unit | Description |
|------------|--------------|------|------|-------------|
| `swap_free` | Swap Free | Gauge | `bytes` | Available swap space (Unix/Linux/macOS) |

**Tags:** `hostname`, `os`, `arch`

**Example Value:** `3758096384` (3.5 GB)

**Use Cases:**
- Monitor swap capacity
- Alert on swap exhaustion
- Capacity planning

**Calculation:**
```
Free = Total - Used
```

**Alert Threshold:**
- `< 10%` free: Risk of swap exhaustion

### Swap Used Percentage

| Metric Name | Display Name | Type | Unit | Description |
|------------|--------------|------|------|-------------|
| `swap_used_percent` | Swap Used | Gauge | `%` | Percentage of swap space in use |

**Tags:** `hostname`, `os`, `arch`

**Example Value:** `12.5` (12.5% used)

**Use Cases:**
- Quick swap health check
- Alert on high swap usage
- Trend swap consumption
- Capacity planning

**Calculation:**
```
Used % = (Used / Total) * 100
```

**Alert Thresholds:**
- `< 25%`: Normal swap usage
- `25-50%`: Monitor memory pressure
- `50-75%`: High swap usage - consider adding RAM
- `> 75%`: Critical - add RAM immediately

**Best Practice:**
Minimize swap usage - systems should rely on RAM, not swap.

## Metric Tags

All memory metrics include standard tags for filtering and aggregation.

### Standard Tags

| Tag Name | Type | Description | Example |
|----------|------|-------------|---------|
| `hostname` | string | System hostname | `web-server-01` |
| `os` | string | Operating system | `linux`, `windows`, `darwin` |
| `arch` | string | CPU architecture | `amd64`, `arm64`, `386` |
| `probe_name` | string | Probe identifier | `memory` |

### Tag Usage Examples

**Filter by OS:**
```promql
memory_used_percent{os="linux"}
```

**Filter by hostname:**
```promql
memory_available{hostname="web-01"}
```

**Aggregate swap across all hosts:**
```promql
avg(swap_used_percent)
```

## Calculation Details

### Unix/Linux Memory Available

Modern Linux (kernel 3.14+) provides `MemAvailable` directly. For older kernels:

```
Available ≈ Free + Buffers + Cached - (Active + Dirty + Writeback)
```

### Windows Commit Limit

```
Commit Limit = Physical RAM + Page File Size

Example:
- RAM: 16 GB
- Page File: 8 GB
- Commit Limit: 24 GB
```

### Memory Used Percentage

```
Used % = ((Total - Available) / Total) × 100

Example:
- Total: 16 GB
- Available: 4 GB
- Used: (16 - 4) / 16 × 100 = 75%
```

### Swap Usage Percentage

```
Swap Used % = (Swap Used / Swap Total) × 100

Example:
- Total: 4 GB
- Used: 512 MB (0.5 GB)
- Used %: (0.5 / 4) × 100 = 12.5%
```

## Use Cases by Metric

### Memory Pressure Detection

| Goal | Primary Metrics | Alert Threshold |
|------|----------------|----------------|
| Low memory | `memory_available` | < 10% of total |
| High memory usage | `memory_used_percent` | > 85% |
| Swapping (Linux) | `swap_used_percent` | > 25% |
| Paging (Windows) | `memory_pages_input` | > 100 pages/s |

### Memory Leak Detection

| Goal | Primary Metrics | Alert Condition |
|------|----------------|----------------|
| Growing usage | `memory_used_percent` | Continuous increase over hours |
| Swap growth | `swap_used` | Continuous increase |
| Kernel leak (Windows) | `memory_nonpaged_pool` | Continuous increase |
| Commit leak (Windows) | `memory_committed` | Growing beyond physical RAM |

### Performance Troubleshooting

| Goal | Primary Metrics | Investigation |
|------|----------------|---------------|
| Slow performance | `swap_used_percent`, `memory_pages_input` | Check if paging/swapping |
| Cache effectiveness | `memory_cached`, `memory_buffers` | Verify adequate cache |
| I/O bottleneck | `memory_modified_page_list`, `memory_pages_output` | Check write-back pressure |

### Capacity Planning

| Goal | Primary Metrics | Decision Criteria |
|------|----------------|------------------|
| Need more RAM? | `memory_used_percent`, `swap_used_percent` | Sustained > 85% + swap usage |
| Right-size VMs | `memory_available` trend | Consistent low availability |
| Page file sizing | `pagefile_usage_peak` | Peak > 75% → increase page file |

## Platform Comparison

### Equivalent Metrics Across Platforms

| Concept | Unix/Linux | Windows | Notes |
|---------|-----------|---------|-------|
| Total RAM | `memory_total` | `memory_total` (commit limit) | Windows includes page file |
| Available | `memory_available` | `memory_available` | Both usable by apps |
| Cache | `memory_cached` + `memory_buffers` | `memory_cache` | Similar purpose |
| Virtual memory | `swap_used` | `memory_committed` | Different implementations |
| Paging | `swap_used_percent` | `memory_pages_input` | Different metrics |

## Monitoring Best Practices

### Alert Configurations

**Basic Alerts:**
```yaml
# Low memory warning (all platforms)
- alert: LowMemory
  expr: memory_used_percent > 85
  for: 5m

# Critical memory shortage
- alert: CriticalMemory
  expr: memory_used_percent > 95
  for: 2m

# High swap usage (Unix/Linux)
- alert: HighSwapUsage
  expr: swap_used_percent > 50
  for: 5m

# Windows paging alert
- alert: WindowsPaging
  expr: memory_pages_input > 100
  for: 5m

# Windows page file alert
- alert: HighPageFileUsage
  expr: pagefile_usage > 75
  for: 5m
```

### Dashboard Panels

**Essential Memory Panels:**
1. **Memory Usage** - Gauge of `memory_used_percent`
2. **Available Memory** - Time series of `memory_available`
3. **Memory Breakdown** - Stacked area of used, cached, buffers, free
4. **Swap Usage** - Time series of `swap_used_percent`
5. **Page Faults** (Windows) - Time series of `memory_page_faults`, `memory_pages_input`

### Collection Intervals

| Monitoring Type | Interval | Reason |
|----------------|----------|--------|
| Real-time | 10s | Detect rapid memory changes |
| Standard | 30s | Balance accuracy/overhead |
| Long-term | 60s | Historical trending |

## Advanced Analysis

### Memory Leak Detection

**Unix/Linux:**
```
Leak Indicator = memory_used continuously increasing
+ swap_used continuously increasing
+ available continuously decreasing
```

**Windows:**
```
Leak Indicator = memory_committed continuously increasing
+ pagefile_usage continuously increasing
+ memory_nonpaged_pool growing (kernel leak)
```

### Cache Effectiveness

**Unix/Linux:**
```
Cache Hit Ratio = (Cached + Buffers) / Total
Good: > 30%
Excellent: > 50%
```

### Memory Pressure Level

**Unix/Linux:**
```
Pressure = 100 - (Available / Total × 100)
< 70%: Normal
70-85%: Moderate pressure
> 85%: High pressure
```

**Windows:**
```
Pressure = (memory_pages_input > 10) AND (memory_used_percent > 80)
```

### Swap Performance Impact

```
Swap Impact = swap_used_percent × performance_penalty

Performance Penalty:
- SSD swap: 10x slower than RAM
- HDD swap: 100x slower than RAM

Example:
25% swap usage on HDD = ~25% performance loss
```

## Related Documentation

- [Memory Probe README](./README.md) - Configuration and quick start
- [System Monitoring Guide](../../guides/system-monitoring.md) - Comprehensive system monitoring
- [Performance Tuning](../../guides/performance-tuning.md) - Optimize agent performance
- [Troubleshooting Guide](../../troubleshooting/) - Common issues and solutions
