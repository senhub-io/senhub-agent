<img src="https://api.iconify.design/mdi/cpu-64-bit.svg?color=%23666" alt="" class="probe-page-logo probe-page-logo-mdi">

!!! info
    **License: Free** - No license required. Available in all tiers.

# CPU Probe

The CPU probe monitors processor performance across all major operating systems, providing comprehensive metrics for usage, load, and system-level CPU statistics.

## Quick Start

### Basic Configuration

```yaml
# probes.d/10-cpu.yaml — each file under probes.d/ is a YAML array of probes
- name: cpu
  type: cpu
  params:
    interval: 30  # Collection interval in seconds (default: 30)
```

### Minimal Configuration

```yaml
# probes.d/10-cpu.yaml
- name: cpu
  type: cpu
  params: {}
```

The CPU probe requires no mandatory parameters and works out-of-the-box with default settings.

## Supported Platforms

- **Windows**: Windows Server 2012+ / Windows 10+
- **Linux**: All modern distributions (Ubuntu, RHEL, CentOS, Debian, etc.)
- **macOS**: macOS 10.13+ (with graceful degradation)
- **BSD**: FreeBSD, OpenBSD, NetBSD

Platform-specific metrics are automatically detected and collected based on the operating system.

### macOS Platform Notes

On macOS, the `gopsutil` library has limited support for detailed CPU time metrics (`cpu.Times()`). The CPU probe implements graceful degradation:

- **If detailed CPU times unavailable**: Probe continues with load average metrics (cpu_load1, cpu_load5, cpu_load15)
- **Always available**: CPU usage percentage (cpu_usage_total, cpu_core_usage)
- **Behavior**: Logs warnings for unavailable metrics but remains active

This ensures the probe stays functional even when platform limitations exist, providing at minimum load average and usage percentage metrics.

## Key Metrics Summary

### Cross-Platform Metrics

| Metric | Description | Available On |
|--------|-------------|--------------|
| `cpu_usage_total` | Total CPU usage percentage (0-100%) | All platforms |
| `cpu_core_usage` | Per-core CPU usage percentage | All platforms |
| `cpu_user` | User-mode CPU time | All platforms |
| `cpu_system` | System-mode CPU time | All platforms |
| `cpu_irq` | Hardware interrupt time | All platforms |
| `cpu_softirq` | Software interrupt time | All platforms |

### Unix/Linux/macOS Specific

| Metric | Description |
|--------|-------------|
| `cpu_idle` | CPU idle time (seconds) |
| `cpu_nice` | CPU nice priority time (seconds) |
| `cpu_iowait` | CPU I/O wait time (seconds) |
| `cpu_steal` | CPU steal time for VMs (seconds) |
| `cpu_load1` | Load average (1 minute) |
| `cpu_load5` | Load average (5 minutes) |
| `cpu_load15` | Load average (15 minutes) |

### Windows Specific

| Metric | Description |
|--------|-------------|
| `cpu_dpc_rate` | Deferred Procedure Calls per second |
| `cpu_dpc_queued` | DPCs queued per second |
| `cpu_interrupts` | Hardware interrupts per second |
| `cpu_queue_length` | Processor queue length |

## Configuration Parameters

| Parameter | Type | Default | Description |
|-----------|------|---------|-------------|
| `interval` | integer | `30` | Collection interval in seconds |

### Example Configurations

**High-frequency monitoring (every 10 seconds):**
```yaml
# probes.d/10-cpu.yaml
- name: cpu
  type: cpu
  params:
    interval: 10
```

**Standard monitoring (every minute):**
```yaml
# probes.d/10-cpu.yaml
- name: cpu
  type: cpu
  params:
    interval: 60
```

## Monitoring Tool Integration

### PRTG Network Monitor

Access CPU metrics in PRTG JSON format:

```bash
# All CPU metrics
curl http://localhost:8080/api/{agentkey}/prtg/metrics

# Configure PRTG HTTP Advanced Sensor:
# - URL: http://agent-host:8080/api/{agentkey}/prtg/metrics
# - Method: POST
# - Request body: {"probe": "cpu"}
```

**PRTG Channels Available:**
- CPU Total Usage (%)
- CPU Core 0-N Usage (%)
- CPU User Time (% or seconds)
- CPU System Time (% or seconds)
- CPU Load Average (Linux/Unix)
- DPC Rate & Interrupts (Windows)

### Nagios

Access CPU metrics in Nagios format:

```bash
# All CPU metrics with performance data
curl http://localhost:8080/api/{agentkey}/nagios/metrics?probe=cpu

# Example output:
# OK - CPU monitoring active | cpu_usage_total=42.5%;80;90 cpu_load1=1.23;;;
```

**Nagios Performance Data:**
- `cpu_usage_total` - Total CPU usage with 80% warning, 90% critical
- `cpu_load1`, `cpu_load5`, `cpu_load15` - Load averages (Unix)
- `cpu_queue_length` - Processor queue (Windows)


### Web Interface

View CPU metrics in the built-in dashboard:

```
http://localhost:8080/web/{agentkey}/dashboard
```

Features:
- Real-time CPU usage visualization
- Per-core CPU usage breakdown
- Load average trends (Unix/Linux)
- System-wide CPU statistics

## Use Cases

### Performance Monitoring
Monitor CPU usage to identify:
- High CPU consumers
- CPU bottlenecks
- Per-core imbalances
- System vs. user time distribution

### Capacity Planning
Track CPU trends over time:
- Peak usage patterns
- Average load levels
- Core utilization distribution
- Growth trends

### VM Performance Analysis
Monitor virtualized environments:
- CPU steal time (hypervisor overhead)
- Queue length (scheduling delays)
- Per-core allocation effectiveness

### Troubleshooting
Diagnose system issues:
- High I/O wait (storage bottleneck)
- Excessive interrupts (hardware issues)
- High DPC rate (Windows driver issues)
- Load average spikes (Unix/Linux)

## Troubleshooting

### No Metrics Collected

**Check probe status:**
```bash
# View agent logs with CPU probe debugging
./agent run --verbose --debug-modules probe.cpu
```

**Verify probe is enabled:**
```bash
# Check configuration (multi-file layout)
grep -rA5 "name: cpu" /etc/senhub/probes.d/
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

**Symptom:** Cannot read `/proc/stat` or system files

**Solution:**
Run the agent with appropriate permissions:
```bash
# Option 1: Run as root
sudo ./agent run

# Option 2: Grant capabilities (Linux)
sudo setcap cap_sys_ptrace=eip ./agent
```

### High CPU Usage from Agent

**Symptom:** Agent itself consuming significant CPU

**Solution:**
1. Increase collection interval:
   ```yaml
   - name: cpu
     type: cpu
     params:
       interval: 60  # Collect every minute instead of 30 seconds
   ```

2. Check for other resource-intensive probes

3. Review system load and available resources

### Per-Core Metrics Missing

**Windows:** Ensure all CPU cores are enabled in BIOS/firmware

**Unix/Linux:** Verify `/proc/cpuinfo` shows all cores:
```bash
cat /proc/cpuinfo | grep processor
```

## Performance Considerations

### Collection Overhead

The CPU probe has minimal overhead:
- **Windows**: ~10ms per collection (PDH counters)
- **Unix/Linux**: ~50ms per collection (gopsutil library)
- **macOS**: ~30ms per collection (system calls)

### Memory Usage

Typical memory footprint per collection:
- Base probe: ~500 KB
- Per-core metrics: ~50 KB per core
- Example: 16-core system = ~1.3 MB total

### Recommended Intervals

| Use Case | Interval | Reason |
|----------|----------|--------|
| Real-time monitoring | 10-15s | Catch short-lived spikes |
| Standard monitoring | 30-60s | Balance accuracy and overhead |
| Long-term trending | 120-300s | Reduce storage and overhead |

## Advanced Configuration

### Multi-Instance Monitoring

Monitor multiple systems with individual configurations:

```yaml
# probes.d/10-cpu.yaml
- name: cpu_realtime
  type: cpu
  params:
    interval: 10

- name: cpu_trending
  type: cpu
  params:
    interval: 300
```

**Note:** This will create duplicate metrics. Use unique probe names for different collection intervals.

### Integration with Other Probes

Correlate CPU metrics with other system metrics:

```yaml
# probes.d/00-host.yaml — group host probes in one fragment
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
    interval: 60
```

This provides comprehensive system monitoring with aligned collection intervals.

## Authentication

The CPU probe requires no authentication as it collects local system metrics only.

## Requirements

### Windows
- Windows Server 2012+ or Windows 10+
- Performance Counter service enabled
- No special permissions required (runs as service account)

### Linux/Unix/macOS
- Read access to `/proc/stat` (Linux)
- Read access to `/proc/loadavg` (Linux)
- System information APIs (macOS, BSD)

### Network
- No network access required (local metrics only)
- HTTP strategy required for remote access to metrics
