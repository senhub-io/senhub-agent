# Memory Probe

The Memory probe monitors system memory and swap usage across all major operating systems, providing comprehensive metrics for physical memory, cache, buffers, and page file utilization.

## Quick Start

### Basic Configuration

```yaml
# probes.d/10-memory.yaml — each file under probes.d/ is a YAML array of probes
- name: memory
  params:
    interval: 30  # Collection interval in seconds (default: 30)
```

### Minimal Configuration

```yaml
# probes.d/10-memory.yaml
- name: memory
  params: {}
```

The Memory probe requires no mandatory parameters and works out-of-the-box with default settings.

## Supported Platforms

- **Windows**: Windows Server 2012+ / Windows 10+
- **Linux**: All modern distributions (Ubuntu, RHEL, CentOS, Debian, etc.)
- **macOS**: macOS 10.13+
- **BSD**: FreeBSD, OpenBSD, NetBSD

Platform-specific metrics are automatically detected and collected based on the operating system.

## Key Metrics Summary

### Cross-Platform Metrics

| Metric | Description | Available On |
|--------|-------------|--------------|
| `memory_total` | Total physical memory (bytes) | All platforms |
| `memory_available` | Available memory for applications (bytes) | All platforms |
| `memory_used_percent` | Memory usage percentage (0-100%) | All platforms |
| `swap_total` | Total swap space (bytes) | All platforms |
| `swap_used` | Used swap space (bytes) | All platforms |
| `swap_used_percent` | Swap usage percentage (0-100%) | All platforms |

### Unix/Linux/macOS Specific

| Metric | Description |
|--------|-------------|
| `memory_used` | Actively used memory (bytes) |
| `memory_free` | Completely unused memory (bytes) |
| `memory_cached` | Page cache memory (bytes) |
| `memory_buffers` | Kernel buffer memory (bytes) |
| `swap_free` | Available swap space (bytes) |

### Windows Specific

| Metric | Description |
|--------|-------------|
| `memory_committed` | Committed (virtual) memory (bytes) |
| `memory_modified_page_list` | Modified pages waiting to be written (bytes) |
| `memory_nonpaged_pool` | Non-pageable kernel memory (bytes) |
| `memory_paged_pool` | Pageable kernel memory (bytes) |
| `memory_cache` | System cache memory (bytes) |
| `memory_page_faults` | Page faults per second |
| `memory_pages_input` | Pages read from disk per second |
| `memory_pages_output` | Pages written to disk per second |
| `pagefile_usage` | Page file usage percentage |
| `pagefile_usage_peak` | Peak page file usage percentage |

For a complete metrics reference, see [METRICS.md](./METRICS.md).

## Configuration Parameters

| Parameter | Type | Default | Description |
|-----------|------|---------|-------------|
| `interval` | integer | `30` | Collection interval in seconds |

### Example Configurations

**High-frequency monitoring (every 10 seconds):**
```yaml
# probes.d/10-memory.yaml
- name: memory
  params:
    interval: 10
```

**Standard monitoring (every minute):**
```yaml
# probes.d/10-memory.yaml
- name: memory
  params:
    interval: 60
```

## Monitoring Tool Integration

### PRTG Network Monitor

Access memory metrics in PRTG JSON format:

```bash
# All memory metrics
curl http://localhost:8080/api/{agentkey}/prtg/metrics

# Configure PRTG HTTP Advanced Sensor:
# - URL: http://agent-host:8080/api/{agentkey}/prtg/metrics
# - Method: POST
# - Request body: {"probe": "memory"}
```

**PRTG Channels Available:**
- Memory Available (bytes)
- Memory Used (%)
- Memory Total (bytes)
- Swap Used (%)
- Page Faults/sec (Windows)
- Cache Memory (bytes)

### Nagios/Icinga

Access memory metrics in Nagios format:

```bash
# All memory metrics with performance data
curl http://localhost:8080/api/{agentkey}/nagios/metrics?probe=memory

# Example output:
# OK - Memory monitoring active | memory_used_percent=65.2%;80;90 swap_used_percent=12.5%;50;75
```

**Nagios Performance Data:**
- `memory_used_percent` - Memory usage with 80% warning, 90% critical
- `swap_used_percent` - Swap usage with 50% warning, 75% critical
- `memory_available` - Available memory in bytes

### Grafana/Prometheus

Access metrics in Prometheus-compatible format:

```bash
# Prometheus format
curl http://localhost:8080/api/{agentkey}/prometheus/metrics

# Example output:
# memory_used_percent{hostname="server01"} 65.2
# memory_available{hostname="server01"} 8589934592
# swap_used_percent{hostname="server01"} 12.5
```

### Web Interface

View memory metrics in the built-in dashboard:

```
http://localhost:8080/web/{agentkey}/dashboard
```

Features:
- Real-time memory usage visualization
- Memory breakdown (used, cached, buffers, free)
- Swap usage monitoring
- Historical memory trends

## Use Cases

### Memory Leak Detection
Monitor memory usage over time to identify:
- Gradual memory consumption increase
- Applications not releasing memory
- Kernel memory leaks
- Cache growth patterns

### Capacity Planning
Track memory trends to plan:
- When to add more RAM
- Application memory requirements
- Virtual machine sizing
- Container memory limits

### Performance Optimization
Analyze memory usage to:
- Identify memory-constrained applications
- Optimize cache utilization
- Reduce swap usage
- Balance memory allocation

### System Health Monitoring
Monitor memory health:
- Available memory for applications
- Swap activity (performance impact)
- Page fault rates (Windows)
- Memory pressure indicators

## Troubleshooting

### No Metrics Collected

**Check probe status:**
```bash
# View agent logs with memory probe debugging
./agent run --verbose --debug-modules probe.memory
```

**Verify probe is enabled:**
```bash
# Check configuration (multi-file layout)
grep -rA5 "name: memory" /etc/senhub/probes.d/
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

**Symptom:** Cannot read `/proc/meminfo` or system files

**Solution:**
Run the agent with appropriate permissions:
```bash
# Option 1: Run as root
sudo ./agent run

# Option 2: Grant capabilities (Linux)
sudo setcap cap_sys_admin=eip ./agent
```

### High Memory Usage from Agent

**Symptom:** Agent itself consuming significant memory

**Solution:**
1. Increase collection interval:
   ```yaml
   - name: memory
     params:
       interval: 60  # Collect every minute instead of 30 seconds
   ```

2. Check for memory leaks in other probes

3. Review system available memory

### Missing Swap Metrics

**Unix/Linux:** Verify swap is configured:
```bash
# Check swap status
swapon --show

# View swap usage
free -h
```

**Windows:** Verify page file is configured:
```powershell
# Check page file settings
Get-CimInstance -ClassName Win32_PageFileUsage
```

## Performance Considerations

### Collection Overhead

The Memory probe has minimal overhead:
- **Windows**: ~5ms per collection (PDH counters)
- **Unix/Linux**: ~20ms per collection (gopsutil library)
- **macOS**: ~15ms per collection (system calls)

### Memory Usage

Typical memory footprint per collection:
- Base probe: ~200 KB
- Metrics storage: ~10 KB per collection
- Example: 30-second interval = ~500 KB total

### Recommended Intervals

| Use Case | Interval | Reason |
|----------|----------|--------|
| Real-time monitoring | 10-15s | Detect rapid memory changes |
| Standard monitoring | 30-60s | Balance accuracy and overhead |
| Long-term trending | 120-300s | Reduce storage and overhead |

## Advanced Configuration

### Multi-Instance Monitoring

Monitor memory with different collection frequencies:

```yaml
# probes.d/10-memory.yaml
- name: memory_realtime
  params:
    interval: 10

- name: memory_trending
  params:
    interval: 300
```

**Note:** This will create duplicate metrics. Use unique probe names for different collection intervals.

### Integration with Other Probes

Correlate memory metrics with other system metrics:

```yaml
# probes.d/00-host.yaml
- name: cpu
  params:
    interval: 30

- name: memory
  params:
    interval: 30

- name: logicaldisk
  params:
    interval: 60
```

This provides comprehensive system monitoring with aligned collection intervals.

## Authentication

The Memory probe requires no authentication as it collects local system metrics only.

## Requirements

### Windows
- Windows Server 2012+ or Windows 10+
- Performance Counter service enabled
- No special permissions required (runs as service account)

### Linux/Unix/macOS
- Read access to `/proc/meminfo` (Linux)
- Read access to `/proc/vmstat` (Linux)
- System information APIs (macOS, BSD)

### Network
- No network access required (local metrics only)
- HTTP strategy required for remote access to metrics

## Support

For issues or questions:

1. **Enable debug logging:**
   ```bash
   ./agent run --verbose --debug-modules probe.memory
   ```

2. **Check probe health:**
   ```bash
   curl http://localhost:8080/api/{agentkey}/debug/health
   ```

3. **Review documentation:**
   - [Complete Metrics Reference](./METRICS.md)
   - [Troubleshooting Guide](../../troubleshooting/)
   - [Agent Configuration Guide](../../../configuration/)

4. **Report issues:**
   - Include platform (OS, version)
   - Include relevant log excerpts
   - Include configuration YAML
