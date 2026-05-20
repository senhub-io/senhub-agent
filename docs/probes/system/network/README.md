# Network Probe

The Network probe monitors network interface performance across all major operating systems, providing comprehensive metrics for bandwidth, packets, errors, and discards on a per-interface basis.

## Quick Start

### Basic Configuration

```yaml
probes:
  - name: network
    params:
      interval: 30  # Collection interval in seconds (default: 30)
```

### Minimal Configuration

```yaml
probes:
  - name: network
    params: {}
```

The Network probe requires no mandatory parameters and works out-of-the-box with default settings.

## Supported Platforms

- **Windows**: Windows Server 2012+ / Windows 10+
- **Linux**: All modern distributions (Ubuntu, RHEL, CentOS, Debian, etc.)
- **macOS**: macOS 10.13+
- **BSD**: FreeBSD, OpenBSD, NetBSD

Platform-specific metrics are automatically detected and collected based on the operating system. Only physical, enabled network interfaces with valid IP addresses are monitored.

## Key Metrics Summary

### Cross-Platform Metrics (All Interfaces)

| Metric | Description | Unit | Available On |
|--------|-------------|------|--------------|
| `bytes_sent` | Bytes transmitted per second | `BytesPerSecond` | All platforms |
| `bytes_received` | Bytes received per second | `BytesPerSecond` | All platforms |
| `packets_sent` | Packets transmitted per second | `/s` | All platforms |
| `packets_received` | Packets received per second | `/s` | All platforms |
| `errors_sent` | Transmission errors per second | `/s` | All platforms |
| `errors_received` | Reception errors per second | `/s` | All platforms |
| `discards_sent` | Transmitted packets discarded | `/s` | All platforms |
| `discards_received` | Received packets discarded | `/s` | All platforms |

All metrics include the `interface` tag to identify the specific network interface.

For a complete metrics reference, see [METRICS.md](./METRICS.md).

## Configuration Parameters

| Parameter | Type | Default | Description |
|-----------|------|---------|-------------|
| `interval` | integer | `30` | Collection interval in seconds |

### Example Configurations

**High-frequency monitoring (every 10 seconds):**
```yaml
probes:
  - name: network
    params:
      interval: 10
```

**Standard monitoring (every minute):**
```yaml
probes:
  - name: network
    params:
      interval: 60
```

## Interface Detection

The probe automatically detects and monitors valid network interfaces based on platform-specific criteria:

### Windows
- **Physical adapters only** (excludes virtual adapters)
- **Enabled interfaces** (NetEnabled = True)
- **Interfaces with IP addresses** (IPv4 or IPv6)
- **Excludes**: Virtual adapters, disabled interfaces, loopback

### Unix/Linux/macOS
- **Non-loopback interfaces** (excludes `lo`, `lo0`)
- **Interfaces that are UP and RUNNING**
- **Interfaces with valid IP addresses** (excludes link-local)
- **Excludes**: Loopback, down interfaces, interfaces without IPs

## Monitoring Tool Integration

### PRTG Network Monitor

Access Network metrics in PRTG JSON format:

```bash
# All Network metrics (all interfaces)
curl http://localhost:8080/api/{agentkey}/prtg/metrics

# Configure PRTG HTTP Advanced Sensor:
# - URL: http://agent-host:8080/api/{agentkey}/prtg/metrics
# - Method: POST
# - Request body: {"probe": "network"}
```

**PRTG Channels Available:**
- Network {Interface} Bytes Sent (bytes)
- Network {Interface} Bytes Received (bytes)
- Network {Interface} Packets Sent (#)
- Network {Interface} Packets Received (#)
- Network {Interface} Send Errors (#)
- Network {Interface} Receive Errors (#)
- Network {Interface} Send Discards (#)
- Network {Interface} Receive Discards (#)

Each interface creates a separate set of channels with the interface name embedded (e.g., "Network eth0 Bytes Sent").

### Nagios/Icinga

Access Network metrics in Nagios format:

```bash
# All Network metrics with performance data
curl http://localhost:8080/api/{agentkey}/nagios/metrics?probe=network

# Example output:
# OK - Network monitoring active | bytes_sent=1234567;; bytes_received=9876543;;
```

**Nagios Performance Data:**
- `bytes_sent` - Bytes transmitted per second per interface
- `bytes_received` - Bytes received per second per interface
- `packets_sent` - Packets transmitted per second
- `packets_received` - Packets received per second
- `errors_sent` - Transmission errors (alert on non-zero)
- `errors_received` - Reception errors (alert on non-zero)

### Grafana/Prometheus

Access metrics in Prometheus-compatible format:

```bash
# Prometheus format
curl http://localhost:8080/api/{agentkey}/prometheus/metrics

# Example output:
# bytes_sent{hostname="server01",interface="eth0",ip="192.168.1.10"} 1234567
# bytes_received{hostname="server01",interface="eth0",ip="192.168.1.10"} 9876543
# packets_sent{hostname="server01",interface="eth0",ip="192.168.1.10"} 8765
```

### Web Interface

View Network metrics in the built-in dashboard:

```
http://localhost:8080/web/{agentkey}/dashboard
```

Features:
- Real-time bandwidth monitoring per interface
- Packet rate visualization
- Error and discard detection
- Interface status and IP addresses

## Use Cases

### Bandwidth Monitoring
Monitor network throughput to:
- Track bandwidth utilization per interface
- Identify bandwidth saturation
- Detect unexpected traffic spikes
- Plan capacity upgrades

### Error Detection
Track network errors to identify:
- Faulty network cables or connectors
- Duplex mismatch issues
- Hardware problems (NIC failures)
- Network configuration issues

### Performance Analysis
Analyze network performance:
- Correlate network metrics with application performance
- Identify bottlenecks in multi-tier applications
- Monitor cloud connectivity (VPN, DirectConnect, ExpressRoute)
- Troubleshoot slow database queries

### Security Monitoring
Detect anomalies:
- Unusual traffic patterns
- DDoS attacks (packet rate spikes)
- Network scans (connection attempts)
- Data exfiltration (unexpected bandwidth usage)

## Troubleshooting

### No Metrics Collected

**Check probe status:**
```bash
# View agent logs with Network probe debugging
./agent run --verbose --debug-modules probe.network
```

**Verify probe is enabled:**
```bash
# Check configuration
cat agent-config.yaml | grep -A5 "name: network"
```

**Check interface detection:**
```bash
# Unix/Linux: List network interfaces
ip addr show

# Windows: List network adapters
Get-NetAdapter | Where-Object {$_.Status -eq "Up"}

# macOS: List interfaces
ifconfig
```

### Windows: No Interfaces Detected

**Symptom:** Network probe collects no metrics on Windows

**Solution:**

1. **Verify physical adapters are enabled:**
   ```powershell
   Get-NetAdapter | Where-Object {$_.PhysicalAdapter -eq $true}
   ```

2. **Check PDH Network Interface counters:**
   ```powershell
   Get-Counter "\Network Interface(*)\Bytes Total/sec"
   ```

3. **Rebuild Performance Counters if needed:**
   ```powershell
   lodctr /R
   ```

4. **Enable debug logging:**
   ```bash
   ./agent run --verbose --debug-modules probe.host
   ```

### Unix/Linux: Missing Interfaces

**Symptom:** Some interfaces not monitored

**Solution:**

1. **Check interface status:**
   ```bash
   # Interface must be UP and RUNNING
   ip link show
   ```

2. **Verify IP addresses:**
   ```bash
   # Interface must have a valid IP (not link-local)
   ip addr show
   ```

3. **Check interface flags:**
   ```bash
   # Must not be loopback
   ifconfig -a
   ```

### High Error Rates

**Symptom:** `errors_sent` or `errors_received` > 0

**Common Causes:**
- Faulty network cable or connector
- Duplex mismatch (auto vs. manual)
- NIC hardware failure
- Driver issues

**Troubleshooting:**
```bash
# Linux: Check interface errors
ethtool -S eth0

# Windows: Check adapter errors
Get-NetAdapterStatistics

# Check for duplex mismatch
ethtool eth0  # Should match switch port settings
```

### High Discard Rates

**Symptom:** `discards_sent` or `discards_received` > 0

**Common Causes:**
- Buffer overflow (high traffic, insufficient buffers)
- QoS policies dropping packets
- Interface congestion
- Resource exhaustion (CPU, memory)

**Troubleshooting:**
```bash
# Linux: Increase ring buffer size
ethtool -G eth0 rx 4096 tx 4096

# Check CPU usage (high CPU can cause discards)
top

# Monitor buffer overruns
netstat -s | grep -i overrun
```

### Incorrect Bandwidth Metrics

**Symptom:** Bandwidth metrics seem incorrect or missing

**Solution:**

1. **Windows**: Ensure Performance Counter service is running:
   ```powershell
   Get-Service PerfHost
   Restart-Service PerfHost
   ```

2. **Unix/Linux**: Verify statistics are updating:
   ```bash
   # Check interface statistics
   cat /proc/net/dev
   ```

3. **Check collection interval**: First collection after agent start may be incomplete (requires 2 samples for rate calculation)

### Interface Name Mismatch

**Symptom:** Interface names in metrics don't match expected names

**Platform-Specific Naming:**
- **Windows**: Uses PDH instance names (e.g., "Intel[R] PRO_1000 MT Desktop Adapter")
- **Linux**: Uses kernel names (e.g., "eth0", "ens192", "enp0s3")
- **macOS**: Uses BSD names (e.g., "en0", "en1")

**Solution:** Use the `interface` tag to filter by actual detected name:
```bash
# List all interfaces detected by the probe
curl http://localhost:8080/api/{agentkey}/senhub/metrics?probe=network | jq '.[] | select(.name=="bytes_sent") | .tags[] | select(.key=="interface")'
```

## Performance Considerations

### Collection Overhead

The Network probe has minimal overhead:
- **Windows**: ~15ms per collection (PDH counters)
- **Unix/Linux**: ~10ms per collection (gopsutil library)
- **macOS**: ~10ms per collection (system calls)

Overhead scales linearly with the number of monitored interfaces.

### Memory Usage

Typical memory footprint per collection:
- Base probe: ~500 KB
- Per-interface metrics: ~100 KB per interface
- Example: 4 interfaces = ~900 KB total

### Recommended Intervals

| Use Case | Interval | Reason |
|----------|----------|--------|
| Real-time monitoring | 10-15s | Detect bandwidth spikes |
| Standard monitoring | 30-60s | Balance accuracy and overhead |
| Long-term trending | 120-300s | Reduce storage and overhead |

## Advanced Configuration

### Multi-Instance Monitoring

Monitor network with different collection intervals:

```yaml
probes:
  - name: network_realtime
    params:
      interval: 10

  - name: network_trending
    params:
      interval: 300
```

**Note:** This will create duplicate metrics. Use unique probe names for different collection intervals.

### Integration with Other Probes

Correlate network metrics with other system metrics:

```yaml
probes:
  - name: network
    params:
      interval: 30

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

This provides comprehensive system monitoring with aligned collection intervals for correlation analysis.

## Metric Tags

All network metrics include detailed tags for identification and filtering:

| Tag | Description | Example |
|-----|-------------|---------|
| `interface` | Interface identifier | `eth0`, `ens192`, `Intel[R] PRO_1000 MT` |
| `ip` | Primary IP address | `192.168.1.10`, `2001:db8::1` |
| `hostname` | System hostname | `web-server-01` |
| `os` | Operating system | `linux`, `windows`, `darwin` |
| `arch` | System architecture | `amd64`, `arm64` |

**Windows-Specific Tags:**
| Tag | Description | Example |
|-----|-------------|---------|
| `adapter` | Physical adapter name | `Intel(R) PRO/1000 MT Desktop Adapter` |
| `connection_name` | Connection name | `Ethernet`, `Wi-Fi` |
| `ip_1`, `ip_2`, ... | Additional IPs | `192.168.1.11`, `10.0.0.5` |

**Unix/Linux/macOS Tags:**
| Tag | Description | Example |
|-----|-------------|---------|
| `ip_1`, `ip_2`, ... | Additional IPs | `192.168.1.11`, `fe80::1` |

### Example Queries

**Filter by interface:**
```promql
bytes_sent{interface="eth0"}
```

**Filter by IP address:**
```promql
packets_received{ip="192.168.1.10"}
```

**Aggregate all interfaces:**
```promql
sum(bytes_sent{hostname="web-01"})
```

## Authentication

The Network probe requires no authentication as it collects local system metrics only.

## Requirements

### Windows
- Windows Server 2012+ or Windows 10+
- Performance Counter service enabled
- WMI access for adapter enumeration
- No special permissions required (runs as service account)

### Linux/Unix/macOS
- Read access to `/proc/net/dev` (Linux)
- Read access to `/sys/class/net/` (Linux)
- System information APIs (macOS, BSD)
- gopsutil library v3 (bundled with agent)

### Network
- No network access required (local metrics only)
- HTTP strategy required for remote access to metrics

## Known Limitations

### Windows
- Virtual network adapters are excluded (Hyper-V, VirtualBox, VMware)
- PDH instance names may contain special characters
- Adapter name mapping requires WMI access

### Unix/Linux/macOS
- Link-local IPv6 addresses are excluded
- Virtual interfaces (bridges, tun/tap) are excluded if not UP/RUNNING
- First collection provides no rate data (requires 2 samples)

## Support

For issues or questions:

1. **Enable debug logging:**
   ```bash
   # Windows: probe.host module handles network on Windows
   ./agent run --verbose --debug-modules probe.host

   # Unix/Linux/macOS: probe.network module
   ./agent run --verbose --debug-modules probe.network
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
   - Include network interface details (`ip addr`, `Get-NetAdapter`)
   - Include relevant log excerpts
   - Include configuration YAML
