# CPU Metrics Complete Reference

This document provides a comprehensive reference for all CPU metrics collected by the SenHub Agent CPU probe across different operating systems.

## Table of Contents

- [Introduction](#introduction)
- [Cross-Platform Metrics](#cross-platform-metrics)
- [Unix/Linux/macOS Metrics](#unixlinuxmacos-metrics)
- [Windows Metrics](#windows-metrics)
- [Per-Core Metrics](#per-core-metrics)
- [Metric Tags](#metric-tags)
- [Calculation Details](#calculation-details)
- [Use Cases by Metric](#use-cases-by-metric)

## Introduction

The CPU probe collects processor performance metrics using platform-specific APIs:

- **Windows**: Performance Data Helper (PDH) counters
- **Linux/Unix/macOS**: gopsutil library (wraps `/proc`, `sysctl`, etc.)

All metrics are collected at the configured interval (default: 30 seconds) and include standard tags for hostname, OS, and architecture.

## Cross-Platform Metrics

These metrics are available on all supported platforms (Windows, Linux, macOS, BSD).

### Total CPU Usage

| Metric Name | Display Name | Type | Unit | Description |
|------------|--------------|------|------|-------------|
| `cpu_usage_total` | CPU Total Usage | Gauge | `%` | Overall CPU usage across all cores (0-100%) |

**Tags:** `hostname`, `os`, `arch`, `core=total`

**Example Value:** `42.5` (42.5% total CPU usage)

**Use Cases:**
- System-wide CPU monitoring
- Alert on high CPU usage (>80%)
- Capacity planning
- Performance baseline tracking

**Platform-Specific Details:**
- **Windows**: `% Processor Time (_Total)` from PDH
- **Unix/Linux**: Calculated from `/proc/stat` CPU time deltas
- **macOS**: System `host_processor_info()` API

### CPU User Time

| Metric Name | Display Name | Type | Unit | Description |
|------------|--------------|------|------|-------------|
| `cpu_user` | CPU User | Gauge | `s` (Unix) / `%` (Windows) | Time spent executing user-mode code |

**Tags:** `hostname`, `os`, `arch`, `core=total`

**Example Values:**
- Unix/Linux: `12345.67` (seconds since boot)
- Windows: `35.8` (35.8% user time)

**Use Cases:**
- Identify user application CPU consumption
- Distinguish application load from system load
- Optimize application performance

**Platform-Specific Details:**
- **Windows**: `% User Time (_Total)` (percentage)
- **Unix/Linux**: Cumulative seconds from `/proc/stat` user + nice
- **Calculation**: Unix values are cumulative; use rate() for percentage

### CPU System Time

| Metric Name | Display Name | Type | Unit | Description |
|------------|--------------|------|------|-------------|
| `cpu_system` | CPU System | Gauge | `s` (Unix) / `%` (Windows) | Time spent executing kernel-mode code |

**Tags:** `hostname`, `os`, `arch`, `core=total`

**Example Values:**
- Unix/Linux: `4567.89` (seconds since boot)
- Windows: `12.3` (12.3% system time)

**Use Cases:**
- Identify kernel overhead
- Detect driver or system service issues
- Monitor syscall intensive applications
- Investigate I/O related system time

**Platform-Specific Details:**
- **Windows**: `% Privileged Time (_Total)` (percentage)
- **Unix/Linux**: Cumulative seconds from `/proc/stat` system

### CPU Interrupt Time

| Metric Name | Display Name | Type | Unit | Description |
|------------|--------------|------|------|-------------|
| `cpu_irq` | CPU IRQ | Gauge | `s` (Unix) / `%` (Windows) | Time spent servicing hardware interrupts |

**Tags:** `hostname`, `os`, `arch`, `core=total`

**Example Values:**
- Unix/Linux: `123.45` (seconds since boot)
- Windows: `2.1` (2.1% interrupt time)

**Use Cases:**
- Detect hardware interrupt storms
- Identify misbehaving hardware or drivers
- Monitor high-throughput network interfaces
- Troubleshoot storage controller issues

**Platform-Specific Details:**
- **Windows**: `% Interrupt Time (_Total)` (percentage)
- **Unix/Linux**: Cumulative seconds from `/proc/stat` irq field

### CPU Soft Interrupt Time

| Metric Name | Display Name | Type | Unit | Description |
|------------|--------------|------|------|-------------|
| `cpu_softirq` | CPU SoftIRQ | Gauge | `s` (Unix) / `%` (Windows) | Time spent servicing software interrupts |

**Tags:** `hostname`, `os`, `arch`, `core=total`

**Example Values:**
- Unix/Linux: `234.56` (seconds since boot)
- Windows: `1.8` (1.8% DPC time)

**Use Cases:**
- Monitor network packet processing overhead
- Identify deferred work queue issues
- Troubleshoot driver performance problems
- Analyze network-intensive workloads

**Platform-Specific Details:**
- **Windows**: `% DPC Time (_Total)` (Deferred Procedure Calls)
- **Unix/Linux**: Cumulative seconds from `/proc/stat` softirq field

## Unix/Linux/macOS Metrics

These metrics are specific to Unix-like operating systems.

### CPU Idle Time

| Metric Name | Display Name | Type | Unit | Description |
|------------|--------------|------|------|-------------|
| `cpu_idle` | CPU Idle | Gauge | `s` | Time CPU spent idle (not executing processes) |

**Tags:** `hostname`, `os`, `arch`

**Example Value:** `98765.43` (seconds since boot)

**Use Cases:**
- Calculate actual CPU utilization: `100 - (idle / total * 100)`
- Identify idle capacity
- Capacity planning for consolidation

**Calculation:**
```
CPU Usage % = 100 - ((idle_now - idle_prev) / (total_now - total_prev) * 100)
```

### CPU Nice Time

| Metric Name | Display Name | Type | Unit | Description |
|------------|--------------|------|------|-------------|
| `cpu_nice` | CPU Nice | Gauge | `s` | Time spent executing processes with positive nice value |

**Tags:** `hostname`, `os`, `arch`

**Example Value:** `345.67` (seconds since boot)

**Use Cases:**
- Monitor low-priority task execution
- Verify nice level effectiveness
- Identify background job overhead

**Note:** Nice values range from -20 (highest priority) to +19 (lowest priority). This metric tracks time spent on processes with nice > 0.

### CPU I/O Wait Time

| Metric Name | Display Name | Type | Unit | Description |
|------------|--------------|------|------|-------------|
| `cpu_iowait` | CPU IO Wait | Gauge | `s` | Time CPU spent waiting for I/O operations to complete |

**Tags:** `hostname`, `os`, `arch`

**Example Value:** `1234.56` (seconds since boot)

**Use Cases:**
- **Primary indicator of storage bottlenecks**
- Identify slow disk I/O
- Detect network file system issues
- Correlate with disk/network metrics

**High I/O Wait Causes:**
- Slow or failing disk drives
- Insufficient disk throughput (IOPS)
- Network storage latency (NFS, CIFS)
- Excessive swapping

**Troubleshooting:**
```bash
# Check I/O wait percentage
iostat -x 1

# Identify processes causing I/O
iotop

# View disk queue depths
iostat -dx 1
```

### CPU Steal Time

| Metric Name | Display Name | Type | Unit | Description |
|------------|--------------|------|------|-------------|
| `cpu_steal` | CPU Steal | Gauge | `s` | Time hypervisor stole CPU from VM (virtualization overhead) |

**Tags:** `hostname`, `os`, `arch`

**Example Value:** `23.45` (seconds since boot)

**Use Cases:**
- **Monitor virtualized environment performance**
- Detect hypervisor CPU oversubscription
- Identify noisy neighbor issues
- Validate VM resource allocation

**High Steal Time Indicators:**
- Physical host CPU oversubscribed
- Other VMs consuming excessive CPU
- Inadequate CPU allocation to VM

**Typical Values:**
- `< 5%`: Normal, acceptable overhead
- `5-10%`: Moderate contention
- `> 10%`: Significant virtualization overhead

**Only Available On:** Virtualized Linux systems (AWS, Azure, VMware, KVM, etc.)

### Load Average Metrics

| Metric Name | Display Name | Type | Unit | Description |
|------------|--------------|------|------|-------------|
| `cpu_load1` | CPU Load Average 1min | Gauge | `#` | Average system load over 1 minute |
| `cpu_load5` | CPU Load Average 5min | Gauge | `#` | Average system load over 5 minutes |
| `cpu_load15` | CPU Load Average 15min | Gauge | `#` | Average system load over 15 minutes |

**Tags:** `hostname`, `os`, `arch`

**Example Values:**
- `cpu_load1`: `1.23` (1.23 runnable processes on average)
- `cpu_load5`: `1.45`
- `cpu_load15`: `1.67`

**Use Cases:**
- **Primary Linux/Unix load indicator**
- Identify CPU saturation
- Trend analysis (short vs. long term)
- Capacity planning

**Interpretation:**
```
Load Value = Number of processes in runnable state (running or waiting)

For a 4-core system:
- Load < 4.0: System has capacity
- Load = 4.0: System fully utilized
- Load > 4.0: Processes waiting for CPU
```

**Load Trend Analysis:**
- `load1 > load5 > load15`: Load increasing (potential problem)
- `load1 < load5 < load15`: Load decreasing (recovery)
- `load1 ≈ load5 ≈ load15`: Stable load

**Warning Thresholds:**
- `load1 / cores > 0.7`: High load warning
- `load1 / cores > 1.0`: Critical load

## Windows Metrics

These metrics are specific to Windows operating systems.

### DPC Rate

| Metric Name | Display Name | Type | Unit | Description |
|------------|--------------|------|------|-------------|
| `cpu_dpc_rate` | CPU DPC Rate | Gauge | `#` | Deferred Procedure Calls per second (all cores) |

**Tags:** `hostname`, `os`, `arch`, `core=total`

**Example Value:** `1234.56` (DPCs/second)

**Use Cases:**
- Detect driver performance issues
- Monitor interrupt coalescing effectiveness
- Identify network driver problems
- Troubleshoot high-throughput I/O

**High DPC Rate Indicators:**
- Network driver issues
- Storage driver problems
- Outdated or buggy drivers
- Insufficient interrupt coalescing

**Typical Values:**
- `< 500`: Normal operation
- `500-2000`: Moderate activity (network/disk intensive)
- `> 2000`: Investigate driver or hardware issues

### DPCs Queued

| Metric Name | Display Name | Type | Unit | Description |
|------------|--------------|------|------|-------------|
| `cpu_dpc_queued` | CPU DPC Queued | Gauge | `/s` | DPCs added to queue per second |

**Tags:** `hostname`, `os`, `arch`, `core=total`

**Example Value:** `876.54` (DPCs queued/second)

**Use Cases:**
- Monitor DPC queue depth
- Identify deferred work backlog
- Correlate with DPC rate metrics
- Troubleshoot interrupt processing

**Note:** Should closely match `cpu_dpc_rate` under normal conditions. Large discrepancy indicates queue buildup.

### Interrupts Per Second

| Metric Name | Display Name | Type | Unit | Description |
|------------|--------------|------|------|-------------|
| `cpu_interrupts` | CPU Interrupts | Gauge | `/s` | Hardware interrupts per second (all cores) |

**Tags:** `hostname`, `os`, `arch`, `core=total`

**Example Value:** `5432.10` (interrupts/second)

**Use Cases:**
- Monitor interrupt load
- Detect interrupt storms
- Identify misbehaving hardware
- Validate interrupt affinity settings

**Typical Values:**
- Idle system: `500-2000` interrupts/sec
- Busy system: `5000-15000` interrupts/sec
- Network-heavy: `10000-50000` interrupts/sec

**High Interrupt Rate Causes:**
- Network interface at line rate
- High-throughput storage controller
- Misbehaving hardware (interrupt storm)
- Missing or broken interrupt coalescing

### Processor Queue Length

| Metric Name | Display Name | Type | Unit | Description |
|------------|--------------|------|------|-------------|
| `cpu_queue_length` | CPU Queue Length | Gauge | `#` | Number of threads waiting for CPU time |

**Tags:** `hostname`, `os`, `arch`, `core=total`

**Example Value:** `2` (2 threads waiting)

**Use Cases:**
- **Primary Windows CPU saturation indicator**
- Detect CPU bottlenecks
- Identify thread scheduling delays
- Capacity planning

**Interpretation:**
```
Queue Length = Threads ready to run but waiting for available CPU

Typical values:
- 0: No waiting threads (idle or light load)
- 1-2: Slight contention (acceptable)
- > 2 per core: CPU bottleneck (investigate)
```

**Warning Thresholds:**
- `queue_length > cores * 2`: High contention
- `queue_length > cores * 4`: Critical CPU saturation

**Equivalent to Unix Load Average:** Similar concept but instantaneous rather than averaged.

## Per-Core Metrics

These metrics provide per-CPU-core granularity for detailed analysis.

### CPU Core Usage

| Metric Name | Display Name | Type | Unit | Description |
|------------|--------------|------|------|-------------|
| `cpu_core_usage` | CPU Core {core} Usage | Gauge | `%` | Per-core CPU usage percentage |

**Tags:** `hostname`, `os`, `arch`, `core=<N>` (e.g., `core=0`, `core=1`, ...)

**Example Values:**
- `cpu_core_usage{core=0}`: `45.2%`
- `cpu_core_usage{core=1}`: `38.7%`
- `cpu_core_usage{core=2}`: `62.1%`
- `cpu_core_usage{core=3}`: `41.5%`

**Use Cases:**
- Identify per-core imbalances
- Detect single-threaded bottlenecks
- Validate CPU affinity settings
- Monitor thread distribution

**Analysis Examples:**

**Balanced Load (Good):**
```
Core 0: 42%
Core 1: 45%
Core 2: 43%
Core 3: 44%
```

**Imbalanced Load (Investigate):**
```
Core 0: 98%  ← Bottleneck
Core 1: 12%
Core 2: 15%
Core 3: 10%
```

**Troubleshooting Imbalances:**
- Check application thread affinity
- Review process CPU affinity settings
- Identify single-threaded bottlenecks
- Consider workload optimization

### Windows Per-Core Metrics

On Windows, additional per-core metrics are available:

| Metric Name | Display Name | Description |
|------------|--------------|-------------|
| `cpu_core_user` | CPU Core {core} User | User time percentage per core |
| `cpu_core_system` | CPU Core {core} System | System time percentage per core |
| `cpu_core_irq` | CPU Core {core} IRQ | Interrupt time percentage per core |
| `cpu_core_softirq` | CPU Core {core} SoftIRQ | DPC time percentage per core |

**Tags:** `hostname`, `os`, `arch`, `core=<N>`

**Use Cases:**
- Detailed per-core analysis
- Identify interrupt affinity issues
- Monitor core-specific driver behavior
- Optimize interrupt distribution

## Metric Tags

All CPU metrics include standard tags for filtering and aggregation.

### Standard Tags

| Tag Name | Type | Description | Example |
|----------|------|-------------|---------|
| `hostname` | string | System hostname | `web-server-01` |
| `os` | string | Operating system | `linux`, `windows`, `darwin` |
| `arch` | string | CPU architecture | `amd64`, `arm64`, `386` |
| `core` | string | CPU core identifier | `0`, `1`, `2`, ..., `total` |
| `probe_name` | string | Probe identifier | `cpu` |

### Tag Usage Examples

**Filter by OS:**
```promql
cpu_usage_total{os="linux"}
```

**Filter specific core:**
```promql
cpu_core_usage{core="0"}
```

**Aggregate across all cores:**
```promql
avg(cpu_core_usage{hostname="web-01"})
```

**Total CPU vs. per-core:**
```promql
cpu_usage_total{core="total"}  # Total
cpu_core_usage{core="0"}       # Core 0
```

## Calculation Details

### Unix/Linux CPU Percentage Calculation

CPU time metrics on Unix/Linux are cumulative seconds since boot. To calculate percentages:

```
CPU Usage % = ((metric_now - metric_prev) / (total_now - total_prev)) * 100

Where:
- total = user + nice + system + idle + iowait + irq + softirq + steal
```

**Example:**
```
Sample 1 (t=0):
- user:   10000s
- system:  2000s
- idle:   50000s
- total:  62000s

Sample 2 (t=30):
- user:   10120s  (delta: +120s)
- system:  2030s  (delta: +30s)
- idle:   50700s  (delta: +700s)
- total:  62850s  (delta: +850s)

User %   = (120 / 850) * 100 = 14.1%
System % = (30 / 850) * 100  = 3.5%
Idle %   = (700 / 850) * 100 = 82.4%
Total Usage = 100 - 82.4 = 17.6%
```

### Windows CPU Percentage

Windows PDH counters return instantaneous percentages (0-100%), no calculation needed.

### Load Average Calculation (Unix/Linux)

Load average represents the exponentially-decaying average of runnable processes:

```
load(t) = runnable_processes(t) * exp(-t/T)

Where:
- T = time constant (1, 5, or 15 minutes)
- Includes both running and waiting processes
```

## Use Cases by Metric

### Performance Monitoring

| Goal | Primary Metrics | Alert Threshold |
|------|----------------|----------------|
| High CPU usage | `cpu_usage_total` | > 80% |
| CPU bottleneck | `cpu_queue_length` (Windows)<br>`cpu_load1` (Unix) | > cores * 2 |
| Single-thread limit | `cpu_core_usage` | One core > 95%, others < 50% |
| I/O bottleneck | `cpu_iowait` | > 20% |

### VM Performance

| Goal | Primary Metrics | Alert Threshold |
|------|----------------|----------------|
| CPU steal | `cpu_steal` | > 10% |
| Oversubscription | `cpu_steal`, `cpu_load1` | Steal > 10%, Load > cores |

### Windows Troubleshooting

| Goal | Primary Metrics | Alert Threshold |
|------|----------------|----------------|
| Driver issues | `cpu_dpc_rate`, `cpu_interrupts` | DPC > 2000/s |
| CPU saturation | `cpu_queue_length` | > cores * 2 |
| Interrupt storm | `cpu_interrupts` | > 50000/s |

### Unix/Linux Troubleshooting

| Goal | Primary Metrics | Alert Threshold |
|------|----------------|----------------|
| Storage bottleneck | `cpu_iowait` | > 20% |
| CPU saturation | `cpu_load1` | > cores |
| System overhead | `cpu_system` | > 30% |

## Platform Comparison

### Equivalent Metrics Across Platforms

| Concept | Unix/Linux | Windows | Notes |
|---------|-----------|---------|-------|
| CPU Usage | `cpu_usage_total` | `cpu_usage_total` | Both are % (0-100) |
| User Time | `cpu_user` (seconds) | `cpu_user` (%) | Different units |
| System Time | `cpu_system` (seconds) | `cpu_system` (%) | Different units |
| Load/Queue | `cpu_load1` (avg) | `cpu_queue_length` (instant) | Different calculations |
| Interrupts | `cpu_irq` (seconds) | `cpu_interrupts` (/s) | Different units |
| Soft IRQ/DPC | `cpu_softirq` (seconds) | `cpu_dpc_rate` (/s) | Different units |

## Monitoring Best Practices

### Alert Configurations

**Basic Alerts:**
```yaml
# High CPU usage (all platforms)
- alert: HighCPUUsage
  expr: cpu_usage_total > 80
  for: 5m

# CPU bottleneck (Windows)
- alert: WindowsCPUBottleneck
  expr: cpu_queue_length > 2 * cpu_count
  for: 5m

# CPU bottleneck (Unix/Linux)
- alert: UnixCPUBottleneck
  expr: cpu_load1 > cpu_count
  for: 5m

# High I/O wait (Unix/Linux)
- alert: HighIOWait
  expr: rate(cpu_iowait[5m]) / rate(cpu_total[5m]) > 0.2
  for: 5m
```

### Dashboard Panels

**Essential CPU Panels:**
1. **Total CPU Usage** - Time series of `cpu_usage_total`
2. **Per-Core Heatmap** - Heatmap of `cpu_core_usage` by core
3. **Load Average** (Unix) - Time series of `cpu_load1`, `cpu_load5`, `cpu_load15`
4. **Processor Queue** (Windows) - Time series of `cpu_queue_length`
5. **CPU Breakdown** - Stacked area of user, system, iowait, irq, idle

### Collection Intervals

| Monitoring Type | Interval | Reason |
|----------------|----------|--------|
| Real-time | 10s | Catch transient spikes |
| Standard | 30s | Balance accuracy/overhead |
| Long-term | 60s | Historical trending |

## Advanced Analysis

### CPU Saturation Detection

**Unix/Linux:**
```
Saturation = (cpu_load1 / cpu_count) > 1.0
```

**Windows:**
```
Saturation = cpu_queue_length > (cpu_count * 2)
```

### Application vs. System CPU

**Unix/Linux:**
```
App CPU %    = rate(cpu_user) / rate(cpu_total) * 100
System CPU % = rate(cpu_system) / rate(cpu_total) * 100
```

**Windows:**
```
Already provided as percentages:
- cpu_user (%)
- cpu_system (%)
```

### Virtualization Overhead

```
Overhead % = rate(cpu_steal) / rate(cpu_total) * 100

Interpretation:
- < 5%: Acceptable
- 5-10%: Monitor closely
- > 10%: Resource contention issue
```

### Per-Core Imbalance Detection

```
Core Imbalance = max(cpu_core_usage) - min(cpu_core_usage)

Thresholds:
- < 20%: Well balanced
- 20-40%: Moderate imbalance
- > 40%: Significant imbalance (investigate)
```

## Related Documentation

- [CPU Probe README](./README.md) - Configuration and quick start
- [System Monitoring Guide](../../guides/system-monitoring.md) - Comprehensive system monitoring
- [Performance Tuning](../../guides/performance-tuning.md) - Optimize agent performance
- [Troubleshooting Guide](../../troubleshooting/) - Common issues and solutions
