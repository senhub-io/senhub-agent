# Network Metrics Complete Reference

This document provides a comprehensive reference for all network metrics collected by the SenHub Agent Network probe across different operating systems.

## Table of Contents

- [Introduction](#introduction)
- [Cross-Platform Metrics](#cross-platform-metrics)
- [Metric Tags](#metric-tags)
- [Calculation Details](#calculation-details)
- [Use Cases by Metric](#use-cases-by-metric)
- [Platform-Specific Behavior](#platform-specific-behavior)
- [Monitoring Best Practices](#monitoring-best-practices)

## Introduction

The Network probe collects network interface performance metrics using platform-specific APIs:

- **Windows**: Performance Data Helper (PDH) counters for `Network Interface` object
- **Linux/Unix/macOS**: gopsutil library wrapping `/proc/net/dev`, `netstat`, and system APIs

All metrics are collected at the configured interval (default: 30 seconds) and include tags for interface identification, IP addresses, hostname, OS, and architecture.

## Cross-Platform Metrics

These metrics are available on all supported platforms (Windows, Linux, macOS, BSD) for each monitored network interface.

### Bytes Sent

| Metric Name | Display Name | Type | Unit | Description |
|------------|--------------|------|------|-------------|
| `bytes_sent` | Network {Interface} - Bytes Sent | Gauge | `BytesPerSecond` | Rate of bytes transmitted per second |

**Tags:** `hostname`, `os`, `arch`, `interface`, `ip`, `probe_name=network`

**Example Value:** `1234567.89` (1.23 MB/s)

**Use Cases:**
- Monitor outbound bandwidth utilization
- Detect bandwidth saturation
- Track upload-heavy workloads (backups, video streaming)
- Identify unexpected data exfiltration

**Platform-Specific Details:**
- **Windows**: `\Network Interface\Bytes Sent/sec` PDH counter (instantaneous rate)
- **Unix/Linux**: Calculated from `/proc/net/dev` cumulative bytes (delta / time)
- **macOS**: System API with delta calculation

**Typical Values:**
- Idle interface: `< 1 KB/s`
- Normal web server: `1-10 MB/s`
- Saturated 1 Gbps link: `~125 MB/s`
- Saturated 10 Gbps link: `~1250 MB/s`

### Bytes Received

| Metric Name | Display Name | Type | Unit | Description |
|------------|--------------|------|------|-------------|
| `bytes_received` | Network {Interface} - Bytes Received | Gauge | `BytesPerSecond` | Rate of bytes received per second |

**Tags:** `hostname`, `os`, `arch`, `interface`, `ip`, `probe_name=network`

**Example Value:** `9876543.21` (9.87 MB/s)

**Use Cases:**
- Monitor inbound bandwidth utilization
- Detect bandwidth saturation
- Track download-heavy workloads (content delivery, database replication)
- Identify DDoS attacks or unexpected traffic

**Platform-Specific Details:**
- **Windows**: `\Network Interface\Bytes Received/sec` PDH counter
- **Unix/Linux**: Calculated from `/proc/net/dev` cumulative bytes
- **macOS**: System API with delta calculation

**Typical Values:**
- Idle interface: `< 1 KB/s`
- Normal application server: `1-50 MB/s`
- Saturated 1 Gbps link: `~125 MB/s`
- Saturated 10 Gbps link: `~1250 MB/s`

### Packets Sent

| Metric Name | Display Name | Type | Unit | Description |
|------------|--------------|------|------|-------------|
| `packets_sent` | Network {Interface} - Packets Sent | Gauge | `/s` | Rate of packets transmitted per second |

**Tags:** `hostname`, `os`, `arch`, `interface`, `ip`, `probe_name=network`

**Example Value:** `8765.43` (8765 packets/s)

**Use Cases:**
- Monitor packet rate independent of size
- Detect small packet floods (DDoS, network scans)
- Track packet-per-second (PPS) limits of network hardware
- Analyze application network behavior

**Platform-Specific Details:**
- **Windows**: `\Network Interface\Packets Sent/sec` PDH counter
- **Unix/Linux**: Calculated from `/proc/net/dev` cumulative packets
- **macOS**: System API with delta calculation

**Typical Values:**
- Idle interface: `< 100 pps`
- Normal workload: `1,000-10,000 pps`
- High throughput: `50,000-100,000 pps`
- Line rate (1 Gbps, small packets): `~1,488,000 pps` (64-byte frames)

**Note:** Packet rate limits often occur before bandwidth limits with small packets.

### Packets Received

| Metric Name | Display Name | Type | Unit | Description |
|------------|--------------|------|------|-------------|
| `packets_received` | Network {Interface} - Packets Received | Gauge | `/s` | Rate of packets received per second |

**Tags:** `hostname`, `os`, `arch`, `interface`, `ip`, `probe_name=network`

**Example Value:** `12345.67` (12345 packets/s)

**Use Cases:**
- Monitor inbound packet rate
- Detect DDoS attacks (SYN floods, UDP floods)
- Identify network scans
- Track packet-per-second performance

**Platform-Specific Details:**
- **Windows**: `\Network Interface\Packets Received/sec` PDH counter
- **Unix/Linux**: Calculated from `/proc/net/dev` cumulative packets
- **macOS**: System API with delta calculation

**Typical Values:**
- Idle interface: `< 100 pps`
- Normal workload: `1,000-10,000 pps`
- High throughput: `50,000-100,000 pps`
- DDoS attack: `> 500,000 pps`

### Errors Sent (Transmission Errors)

| Metric Name | Display Name | Type | Unit | Description |
|------------|--------------|------|------|-------------|
| `errors_sent` | Network {Interface} - Errors Sent | Gauge | `/s` | Rate of outbound transmission errors per second |

**Tags:** `hostname`, `os`, `arch`, `interface`, `ip`, `probe_name=network`

**Example Value:** `0.0` (normal) or `5.3` (problematic)

**Use Cases:**
- **Primary indicator of network hardware or cable problems**
- Detect faulty network interface cards (NICs)
- Identify duplex mismatch issues
- Monitor transmission quality

**Common Causes:**
- Faulty network cable or connector
- Duplex mismatch (auto-negotiate failure)
- NIC hardware failure
- Driver bugs or compatibility issues
- Overheating network hardware

**Platform-Specific Details:**
- **Windows**: `\Network Interface\Packets Outbound Errors` PDH counter
- **Unix/Linux**: `errin` field from `/proc/net/dev`
- **macOS**: System API error counters

**Expected Value:** `0` (any non-zero value indicates a problem)

**Troubleshooting High Errors:**
```bash
# Linux: Check error details
ethtool -S eth0 | grep -i error

# Check duplex settings
ethtool eth0 | grep -i duplex

# Windows: Check adapter diagnostics
Get-NetAdapterStatistics | Where-Object {$_.OutboundErrors -gt 0}
```

### Errors Received (Reception Errors)

| Metric Name | Display Name | Type | Unit | Description |
|------------|--------------|------|------|-------------|
| `errors_received` | Network {Interface} - Errors Received | Gauge | `/s` | Rate of inbound reception errors per second |

**Tags:** `hostname`, `os`, `arch`, `interface`, `ip`, `probe_name=network`

**Example Value:** `0.0` (normal) or `3.7` (problematic)

**Use Cases:**
- **Primary indicator of network hardware or cable problems**
- Detect faulty cabling or connectors
- Identify physical layer issues (bit errors, CRC errors)
- Monitor reception quality

**Common Causes:**
- Damaged or low-quality network cable
- Electromagnetic interference (EMI)
- Faulty switch port or NIC
- Incorrect cable type (Cat5 vs. Cat6)
- Cable length exceeds specifications

**Platform-Specific Details:**
- **Windows**: `\Network Interface\Packets Received Errors` PDH counter
- **Unix/Linux**: `errout` field from `/proc/net/dev`
- **macOS**: System API error counters

**Expected Value:** `0` (any non-zero value indicates a problem)

**Error Types (Linux):**
- CRC errors: Physical layer corruption
- Frame errors: Framing protocol issues
- Overrun errors: Buffer overflow (see discards)

**Troubleshooting:**
```bash
# Linux: Detailed error breakdown
ethtool -S eth0 | grep -E 'crc|frame|align'

# Test cable quality
ethtool --test eth0

# Check for EMI sources near cabling
```

### Discards Sent (Outbound Discards)

| Metric Name | Display Name | Type | Unit | Description |
|------------|--------------|------|------|-------------|
| `discards_sent` | Network {Interface} - Discards Sent | Gauge | `/s` | Rate of outbound packets discarded per second |

**Tags:** `hostname`, `os`, `arch`, `interface`, `ip`, `probe_name=network`

**Example Value:** `0.0` (normal) or `42.5` (problematic)

**Use Cases:**
- **Primary indicator of interface congestion**
- Detect transmit buffer overruns
- Identify insufficient bandwidth
- Monitor QoS policy effectiveness

**Common Causes:**
- Interface congestion (traffic exceeds link capacity)
- Insufficient transmit buffer size
- CPU overload preventing packet processing
- QoS policies dropping low-priority traffic
- Bandwidth policing or rate limiting

**Platform-Specific Details:**
- **Windows**: `\Network Interface\Packets Outbound Discarded` PDH counter
- **Unix/Linux**: `dropout` field from `/proc/net/dev`
- **macOS**: System API discard counters

**Expected Value:** `0` or very low (< 0.01% of packets sent)

**Troubleshooting:**
```bash
# Linux: Increase transmit ring buffer
ethtool -g eth0  # Show current settings
ethtool -G eth0 tx 4096  # Increase to 4096

# Check interface congestion
ip -s link show eth0

# Monitor CPU usage (high CPU can cause discards)
mpstat 1
```

### Discards Received (Inbound Discards)

| Metric Name | Display Name | Type | Unit | Description |
|------------|--------------|------|------|-------------|
| `discards_received` | Network {Interface} - Discards Received | Gauge | `/s` | Rate of inbound packets discarded per second |

**Tags:** `hostname`, `os`, `arch`, `interface`, `ip`, `probe_name=network`

**Example Value:** `0.0` (normal) or `67.8` (problematic)

**Use Cases:**
- **Primary indicator of receive buffer overruns**
- Detect high packet rate overwhelming interface
- Identify insufficient receive buffers
- Monitor system resource exhaustion

**Common Causes:**
- High packet rate (DDoS, network flood)
- Insufficient receive buffer size
- CPU overload (cannot process packets fast enough)
- Memory pressure preventing buffer allocation
- Network storms (broadcast, multicast floods)

**Platform-Specific Details:**
- **Windows**: `\Network Interface\Packets Received Discarded` PDH counter
- **Unix/Linux**: `dropin` field from `/proc/net/dev`
- **macOS**: System API discard counters

**Expected Value:** `0` or very low (< 0.01% of packets received)

**Troubleshooting:**
```bash
# Linux: Increase receive ring buffer
ethtool -g eth0  # Show current settings
ethtool -G eth0 rx 4096  # Increase to 4096

# Check for packet drops in kernel
netstat -s | grep -i drop

# Monitor interrupt processing
watch -n1 'cat /proc/interrupts | grep eth0'

# Check CPU IRQ time
mpstat -P ALL 1
```

## Metric Tags

All network metrics include comprehensive tags for filtering, grouping, and correlation.

### Standard Tags (All Platforms)

| Tag Name | Type | Description | Example |
|----------|------|-------------|---------|
| `hostname` | string | System hostname | `web-server-01` |
| `os` | string | Operating system | `linux`, `windows`, `darwin` |
| `arch` | string | CPU architecture | `amd64`, `arm64`, `386` |
| `interface` | string | Network interface identifier | `eth0`, `ens192`, `Intel[R] PRO_1000 MT` |
| `ip` | string | Primary IP address (IPv4 or IPv6) | `192.168.1.10`, `2001:db8::1` |
| `probe_name` | string | Probe identifier | `network` |

### Windows-Specific Tags

| Tag Name | Type | Description | Example |
|----------|------|-------------|---------|
| `adapter` | string | Physical adapter name from WMI | `Intel(R) PRO/1000 MT Desktop Adapter` |
| `connection_name` | string | Network connection name | `Ethernet`, `Wi-Fi`, `Local Area Connection` |
| `ip_1`, `ip_2`, ... | string | Additional IP addresses | `192.168.1.11`, `10.0.0.5` |

### Unix/Linux/macOS Tags

| Tag Name | Type | Description | Example |
|----------|------|-------------|---------|
| `ip_1`, `ip_2`, ... | string | Additional IP addresses (beyond primary) | `192.168.1.11`, `fe80::1` |

### Tag Usage Examples

**Filter specific interface:**
```promql
bytes_sent{interface="eth0"}
```

**Filter by IP address:**
```promql
packets_received{ip="192.168.1.10"}
```

**Aggregate all interfaces on a host:**
```promql
sum(bytes_sent{hostname="web-01"})
```

**Filter by operating system:**
```promql
errors_received{os="linux"}
```

**Windows: Filter by connection name:**
```promql
bytes_received{connection_name="Ethernet"}
```

## Calculation Details

### Windows Rate Calculation

Windows PDH counters return instantaneous rates (per second) directly:
- `Bytes Sent/sec`: Already calculated by Windows Performance Counters
- `Packets Sent/sec`: Already calculated by Windows Performance Counters

No additional calculation needed by the agent.

### Unix/Linux/macOS Rate Calculation

Unix-like systems provide cumulative counters since boot. The agent calculates rates:

```
Rate = (counter_now - counter_previous) / (timestamp_now - timestamp_previous)

Example:
Sample 1 (t=0):
- bytes_sent: 1000000000

Sample 2 (t=30):
- bytes_sent: 1037500000

Bytes Sent Rate = (1037500000 - 1000000000) / 30 = 1250000 bytes/second
```

**Important Notes:**
1. **First collection**: No rate data available (requires 2 samples)
2. **Counter wraparound**: Handled automatically (counters are 64-bit on modern systems)
3. **Interface restart**: Counter resets detected and handled

### Bandwidth Percentage Calculation

To calculate interface utilization percentage:

```
Utilization % = (bytes_sent + bytes_received) / link_speed * 100

Example (1 Gbps interface):
- bytes_sent: 50,000,000 bytes/sec (50 MB/s = 400 Mbps)
- bytes_received: 25,000,000 bytes/sec (25 MB/s = 200 Mbps)
- link_speed: 125,000,000 bytes/sec (1 Gbps = 1000 Mbps)

Utilization = (50,000,000 + 25,000,000) / 125,000,000 * 100 = 60%
```

**Note:** Link speed must be obtained separately (via `ethtool`, WMI, or system APIs).

## Use Cases by Metric

### Bandwidth Monitoring

| Goal | Primary Metrics | Alert Threshold |
|------|----------------|----------------|
| Track bandwidth usage | `bytes_sent`, `bytes_received` | Depends on link speed |
| Detect saturation | `bytes_sent + bytes_received` | > 80% of link capacity |
| Identify spikes | `bytes_sent`, `bytes_received` | > 2x baseline |
| Capacity planning | Historical `bytes_sent/received` | Trending toward limit |

### Error Detection

| Goal | Primary Metrics | Alert Threshold |
|------|----------------|----------------|
| Cable problems | `errors_sent`, `errors_received` | > 0 |
| Duplex mismatch | `errors_sent`, `errors_received` | > 0.01% of packets |
| Hardware failure | `errors_sent`, `errors_received` | Increasing over time |

### Performance Analysis

| Goal | Primary Metrics | Alert Threshold |
|------|----------------|----------------|
| Interface congestion | `discards_sent`, `discards_received` | > 0.1% of packets |
| Buffer tuning | `discards_received` | > 0 |
| QoS effectiveness | `discards_sent` | Depends on policy |
| Packet rate limits | `packets_sent`, `packets_received` | Near hardware PPS limit |

### Security Monitoring

| Goal | Primary Metrics | Alert Threshold |
|------|----------------|----------------|
| DDoS detection | `packets_received` | > 10x baseline |
| Network scans | `packets_received` | Sustained high rate |
| Data exfiltration | `bytes_sent` | > 5x baseline |

## Platform-Specific Behavior

### Windows

**Interface Detection:**
- Uses WMI `Win32_NetworkAdapter` query with `PhysicalAdapter=True`
- Filters to `NetEnabled=True` adapters only
- Maps PDH instance names to connection names via adapter name normalization

**Name Normalization:**
- Removes trademarks: `(R)`, `(TM)`, `®`, `™`
- Removes special characters: `()`, `[]`, `-`, `_`
- Converts to lowercase for comparison

**Example Mapping:**
```
WMI Adapter: Intel(R) PRO/1000 MT Desktop Adapter
PDH Instance: intel pro 1000 mt desktop adapter
Connection:   Ethernet
```

**Tags:**
- `interface`: PDH instance name (e.g., `Intel[R] PRO_1000 MT`)
- `adapter`: WMI adapter name
- `connection_name`: Network connection name (e.g., `Ethernet`)
- `ip`: Primary IPv4 or IPv6 address
- `ip_1`, `ip_2`, ...: Additional IP addresses

### Unix/Linux

**Interface Detection:**
- Uses `net.Interfaces()` from Go standard library
- Filters to interfaces with `UP` and `RUNNING` flags
- Excludes loopback interfaces (`lo`)
- Requires at least one valid IP address (non link-local)

**Typical Interface Names:**
- **Old naming**: `eth0`, `eth1`, `wlan0`
- **Predictable naming**: `ens192`, `enp0s3`, `wlp3s0`
- **Docker**: `docker0` (excluded if no valid IP)
- **Virtual**: `tun0`, `tap0` (excluded if not UP/RUNNING)

**Tags:**
- `interface`: Kernel interface name (e.g., `eth0`)
- `ip`: Primary IPv4 or IPv6 address (non link-local)
- `ip_1`, `ip_2`, ...: Additional IP addresses

### macOS

**Interface Detection:**
- Uses `net.Interfaces()` from Go standard library
- Filters to interfaces with `UP` and `RUNNING` flags
- Excludes loopback interface (`lo0`)
- Requires at least one valid IP address

**Typical Interface Names:**
- Ethernet: `en0`, `en1`
- Wi-Fi: `en1` (often)
- Thunderbolt: `en2`, `en3`
- Bridge: `bridge0` (excluded if not UP/RUNNING)

**Tags:**
- `interface`: BSD interface name (e.g., `en0`)
- `ip`: Primary IPv4 or IPv6 address
- `ip_1`, `ip_2`, ...: Additional IP addresses

### BSD (FreeBSD, OpenBSD, NetBSD)

**Interface Detection:**
- Similar to Linux/macOS using `net.Interfaces()`
- Filters to UP/RUNNING interfaces with valid IPs
- Excludes loopback (`lo0`)

**Typical Interface Names:**
- Ethernet: `em0`, `re0`, `igb0`
- Wi-Fi: `wlan0`, `iwn0`

## Monitoring Best Practices

### Alert Configurations

**Bandwidth Alerts:**
```yaml
# High bandwidth usage (requires link speed)
- alert: HighBandwidthUsage
  expr: (bytes_sent + bytes_received) / link_speed > 0.8
  for: 5m

# Sustained high inbound traffic
- alert: HighInboundTraffic
  expr: bytes_received > 100000000  # 100 MB/s
  for: 5m
```

**Error Alerts:**
```yaml
# Any transmission errors (critical)
- alert: NetworkTransmissionErrors
  expr: errors_sent > 0
  for: 1m

# Any reception errors (critical)
- alert: NetworkReceptionErrors
  expr: errors_received > 0
  for: 1m
```

**Discard Alerts:**
```yaml
# High outbound discards (congestion)
- alert: NetworkOutboundDiscards
  expr: discards_sent / packets_sent > 0.001  # 0.1%
  for: 5m

# High inbound discards (buffer overruns)
- alert: NetworkInboundDiscards
  expr: discards_received / packets_received > 0.001  # 0.1%
  for: 5m
```

**Packet Rate Alerts:**
```yaml
# Possible DDoS attack
- alert: HighPacketRate
  expr: packets_received > 100000  # 100k pps
  for: 1m

# Suspicious packet rate spike
- alert: PacketRateSpike
  expr: rate(packets_received[5m]) > 2 * avg_over_time(packets_received[1h])
  for: 5m
```

### Dashboard Panels

**Essential Network Panels:**

1. **Bandwidth Usage (Time Series)**
   - Metrics: `bytes_sent`, `bytes_received`
   - Display: Stacked area chart
   - Unit: `BytesPerSecond` or `bps` (bits/second)
   - Y-axis: Auto-scale or fixed to link speed

2. **Packet Rate (Time Series)**
   - Metrics: `packets_sent`, `packets_received`
   - Display: Line chart
   - Unit: Packets per second

3. **Errors and Discards (Time Series)**
   - Metrics: `errors_sent`, `errors_received`, `discards_sent`, `discards_received`
   - Display: Stacked area chart (log scale if needed)
   - Unit: Per second
   - Alert: Any non-zero value

4. **Interface Utilization (Gauge)**
   - Calculation: `(bytes_sent + bytes_received) / link_speed * 100`
   - Display: Gauge or bar chart
   - Unit: Percentage
   - Thresholds: 0-70% green, 70-90% yellow, >90% red

5. **Per-Interface Heatmap**
   - Metrics: `bytes_sent`, `bytes_received` by `interface` tag
   - Display: Heatmap or stacked bar chart
   - Identifies busiest interfaces

### Collection Intervals

| Use Case | Interval | Reason |
|----------|----------|--------|
| Real-time monitoring | 10s | Detect short-lived bandwidth spikes |
| Standard monitoring | 30s | Balance accuracy and overhead |
| Long-term trending | 60-120s | Reduce storage, still catch issues |

### Bandwidth Thresholds by Link Speed

| Link Speed | Warning Threshold | Critical Threshold |
|------------|------------------|-------------------|
| 100 Mbps | 8 MB/s (64%) | 10 MB/s (80%) |
| 1 Gbps | 100 MB/s (80%) | 120 MB/s (96%) |
| 10 Gbps | 1000 MB/s (80%) | 1200 MB/s (96%) |

**Note:** Leave ~10-20% headroom for bursts and overhead.

## Advanced Analysis

### Bandwidth Utilization Calculation

```
# Bits per second conversion
bits_sent = bytes_sent * 8

# Percentage of link capacity
utilization = (bytes_sent + bytes_received) * 8 / link_speed_bps * 100

# Example: 1 Gbps link
link_speed = 1000000000 bps
bytes_sent = 50000000 bytes/sec
bytes_received = 30000000 bytes/sec

utilization = (50000000 + 30000000) * 8 / 1000000000 * 100 = 64%
```

### Error Rate Calculation

```
# Percentage of packets with errors
error_rate_sent = errors_sent / packets_sent * 100
error_rate_received = errors_received / packets_received * 100

# Example
packets_sent = 10000 pps
errors_sent = 5 errors/sec
error_rate = 5 / 10000 * 100 = 0.05%

Interpretation:
- < 0.01%: Normal
- 0.01-0.1%: Investigate
- > 0.1%: Critical hardware/cable issue
```

### Discard Rate Calculation

```
# Percentage of packets discarded
discard_rate_sent = discards_sent / packets_sent * 100
discard_rate_received = discards_received / packets_received * 100

# Example
packets_sent = 50000 pps
discards_sent = 250 discards/sec
discard_rate = 250 / 50000 * 100 = 0.5%

Interpretation:
- 0%: No congestion
- < 0.1%: Acceptable
- 0.1-1%: Moderate congestion
- > 1%: Severe congestion, increase buffers or bandwidth
```

### Average Packet Size Calculation

```
# Average transmitted packet size
avg_packet_size_sent = bytes_sent / packets_sent

# Average received packet size
avg_packet_size_received = bytes_received / packets_received

# Example
bytes_sent = 1250000 bytes/sec
packets_sent = 1000 packets/sec
avg_size = 1250000 / 1000 = 1250 bytes

Typical sizes:
- 64 bytes: Minimum Ethernet frame (ACKs, small control packets)
- 1500 bytes: Standard MTU (full-sized packets)
- 9000 bytes: Jumbo frames (if enabled)
```

### Packet Rate vs. Bandwidth Analysis

```
Small packet scenario (DDoS, SYN flood):
- packets_received: 500,000 pps
- bytes_received: 32,000,000 bytes/sec (32 MB/s = 256 Mbps)
- avg_packet_size: 64 bytes (minimum Ethernet frame)

Result: High packet rate, low bandwidth (hardware PPS limit reached)

Large packet scenario (bulk transfer):
- packets_received: 10,000 pps
- bytes_received: 15,000,000 bytes/sec (15 MB/s = 120 Mbps)
- avg_packet_size: 1500 bytes (full MTU)

Result: Moderate packet rate, moderate bandwidth (normal operation)
```

## Troubleshooting by Symptom

### High Bandwidth, No Performance Issues
- **Normal**: High throughput is expected (backups, data transfers)
- **Action**: Monitor for sustained saturation (>80% for extended periods)

### High Bandwidth, Performance Degradation
- **Cause**: Interface saturation, bandwidth limit reached
- **Action**: Upgrade link speed or implement QoS

### Low Bandwidth, High Packet Rate
- **Cause**: Small packet flood (DDoS, network scan, SYN flood)
- **Action**: Implement rate limiting, investigate packet source

### Errors > 0
- **Cause**: Hardware/cable issue, duplex mismatch
- **Action**: Check cables, connectors, NIC health, duplex settings

### Discards Sent > 0
- **Cause**: Interface congestion, insufficient TX buffers
- **Action**: Increase buffers, reduce traffic, upgrade bandwidth

### Discards Received > 0
- **Cause**: Buffer overruns, high packet rate, CPU overload
- **Action**: Increase RX buffers, reduce CPU load, filter traffic

## Related Documentation

- [Network Probe README](./README.md) - Configuration and quick start
- [System Monitoring Guide](../../guides/system-monitoring.md) - Comprehensive system monitoring
- [Performance Tuning](../../guides/performance-tuning.md) - Optimize agent performance
- [Troubleshooting Guide](../../troubleshooting/) - Common issues and solutions
