# WebApp Ping Metrics Complete Reference

This document provides a comprehensive reference for all metrics collected by the SenHub Agent WebApp Ping probe across different operating systems.

## Table of Contents

- [Introduction](#introduction)
- [Metrics Overview](#metrics-overview)
- [Detailed Metric Specifications](#detailed-metric-specifications)
- [Metric Tags](#metric-tags)
- [Calculation Details](#calculation-details)
- [Use Cases by Metric](#use-cases-by-metric)
- [Platform Differences](#platform-differences)
- [Monitoring Best Practices](#monitoring-best-practices)

## Introduction

The WebApp Ping probe collects network reachability and latency metrics for web applications using ICMP (Internet Control Message Protocol) ping tests. The probe:

1. **Resolves DNS**: Extracts hostname from URL and resolves to IP address
2. **Executes Ping**: Sends 10 ICMP Echo Request packets
3. **Collects Metrics**: Parses ping output for latency and packet loss
4. **Reports**: Sends metrics with URL tags to configured storage strategies

All metrics are collected at the configured interval (default: 30 seconds) and include tags for the target URL.

## Metrics Overview

The WebApp Ping probe collects **2 primary metrics** for each monitored URL:

| Metric Name | Display Name | Type | Unit | Description |
|------------|--------------|------|------|-------------|
| `averageLatency` | Average Latency | Gauge | `ms` | Average ICMP round-trip time over 10 packets |
| `packetLoss` | Packet Loss | Gauge | `%` | Percentage of packets lost (0-100%) |

**Key Characteristics:**
- **Cross-platform**: Available on Windows, Linux, macOS, and BSD
- **Multi-instance**: Each URL monitored generates separate metrics
- **Real-time**: Reflects current network conditions
- **Standard protocol**: Uses ICMP Echo Request/Reply (ping)

## Detailed Metric Specifications

### Average Latency

| Metric Name | `averageLatency` |
|------------|------------------|
| **Display Name** | Average Latency ({url}) |
| **Type** | Gauge |
| **Unit** | Milliseconds (ms) |
| **Value Range** | 0.0 - 10000.0+ |
| **Sample Count** | 10 ICMP packets per collection |

**Description:**
Average round-trip time for ICMP Echo Request/Reply packets. Calculated as the arithmetic mean of all successful ping responses within the collection window.

**Tags:** `hostname`, `os`, `arch`, `url`, `probe_name=ping_webapp`

**Example Values:**
- `12.5` ms - Excellent (local network or nearby server)
- `45.2` ms - Good (regional connection)
- `150.0` ms - Moderate (long-distance or congested)
- `500.0` ms - Poor (severe latency issues)

**Use Cases:**
- **Network performance monitoring**: Track latency trends over time
- **SLA compliance**: Ensure latency meets service level agreements
- **Geographic performance**: Compare latency across regions
- **Capacity planning**: Identify when additional CDN or edge locations needed
- **Troubleshooting**: Detect network path issues or congestion

**Platform-Specific Details:**

**Windows:**
- Command: `ping -n 10 {ip}`
- Output parsing: `Average = {value}ms`
- Precision: Whole milliseconds (e.g., `42ms`)

**Linux:**
- Command: `ping -c 10 {ip}`
- Output parsing: `rtt min/avg/max/mdev = {min}/{avg}/{max}/{mdev}`
- Precision: Three decimal places (e.g., `42.123ms`)

**macOS:**
- Command: `ping -c 10 {ip}`
- Output parsing: `round-trip min/avg/max/stddev = {min}/{avg}/{max}/{stddev}`
- Precision: Three decimal places (e.g., `42.123ms`)

**Calculation:**
```
averageLatency = sum(successful_ping_times) / count(successful_pings)

Example (10 packets):
Packet 1: 40.2ms
Packet 2: 42.1ms
Packet 3: 41.8ms
Packet 4: 43.5ms
Packet 5: 41.9ms
Packet 6: 42.7ms
Packet 7: 40.5ms
Packet 8: 43.2ms
Packet 9: 41.6ms
Packet 10: 42.4ms

Average = (40.2 + 42.1 + 41.8 + 43.5 + 41.9 + 42.7 + 40.5 + 43.2 + 41.6 + 42.4) / 10
        = 419.9 / 10
        = 41.99ms
```

**Alert Thresholds:**

| Threshold | Value | Severity | Action |
|-----------|-------|----------|--------|
| Normal | < 50ms | Info | No action |
| Warning | 50-100ms | Warning | Monitor closely |
| High | 100-200ms | Warning | Investigate routing |
| Critical | > 200ms | Critical | Immediate investigation |

**Performance Baseline Examples:**

| Scenario | Expected Latency | Notes |
|----------|------------------|-------|
| Local network (LAN) | 0.1-5ms | Same subnet |
| Campus network | 1-10ms | Same building/campus |
| Same city | 5-20ms | Metropolitan area |
| Regional (< 500km) | 10-50ms | Same region |
| Cross-country | 50-150ms | Continent-wide |
| Intercontinental | 150-300ms | International |
| Satellite | 500-700ms | Geostationary satellite |

### Packet Loss

| Metric Name | `packetLoss` |
|------------|--------------|
| **Display Name** | Packet Loss ({url}) |
| **Type** | Gauge |
| **Unit** | Percent (%) |
| **Value Range** | 0.0 - 100.0 |
| **Sample Count** | 10 ICMP packets per collection |

**Description:**
Percentage of ICMP Echo Request packets that did not receive a reply within the timeout period. Indicates network reliability and packet delivery success rate.

**Tags:** `hostname`, `os`, `arch`, `url`, `probe_name=ping_webapp`

**Example Values:**
- `0.0%` - Perfect (no packet loss)
- `1.0%` - Excellent (1 packet lost out of 100)
- `5.0%` - Acceptable (5 packets lost out of 100)
- `10.0%` - Poor (10 packets lost out of 100)
- `100.0%` - Complete failure (all packets lost)

**Use Cases:**
- **Availability monitoring**: Zero packet loss indicates full reachability
- **Network reliability**: Track packet loss trends over time
- **SLA validation**: Ensure packet loss meets availability SLAs
- **Fault detection**: Immediate alert on packet loss spikes
- **Path quality**: Identify unstable network paths

**Platform-Specific Details:**

**Windows:**
- Command: `ping -n 10 {ip}`
- Output parsing: `Lost = {count} ({percentage}% loss)`
- Example: `Lost = 1 (10% loss)` → packetLoss = 10.0

**Linux:**
- Command: `ping -c 10 {ip}`
- Output parsing: `{percentage}% packet loss`
- Example: `10.0% packet loss` → packetLoss = 10.0

**macOS:**
- Command: `ping -c 10 {ip}`
- Output parsing: `{percentage}% packet loss`
- Example: `10.0% packet loss` → packetLoss = 10.0

**Calculation:**
```
packetLoss = (packets_lost / packets_sent) * 100

Example (10 packets sent):
- 10 sent, 10 received: 0% loss
- 10 sent, 9 received:  10% loss
- 10 sent, 5 received:  50% loss
- 10 sent, 0 received:  100% loss
```

**Alert Thresholds:**

| Threshold | Value | Severity | Action |
|-----------|-------|----------|--------|
| Normal | 0% | Info | No action |
| Minor | 1-5% | Warning | Monitor for trends |
| Moderate | 5-10% | Warning | Investigate network |
| High | 10-25% | Critical | Immediate investigation |
| Severe | > 25% | Critical | Outage likely |
| Complete | 100% | Critical | Service down |

**Packet Loss Causes:**

| Loss Rate | Likely Cause | Troubleshooting |
|-----------|--------------|-----------------|
| 0-1% | Normal operation | No action needed |
| 1-5% | Minor congestion or ICMP deprioritization | Monitor trends |
| 5-10% | Network congestion or routing issues | Check network path (traceroute) |
| 10-25% | Significant network problems | Investigate routers, switches |
| 25-50% | Severe network degradation | Check physical connections |
| 50-100% | Network outage or ICMP blocked | Verify connectivity, firewall rules |
| 100% | Complete failure | DNS issue, firewall block, or service down |

**Availability Calculation:**
```
Availability = 100% - packetLoss

Examples:
- 0% packet loss    = 100.00% availability (five nines: 99.999%)
- 0.1% packet loss  = 99.90% availability
- 1% packet loss    = 99.00% availability
- 5% packet loss    = 95.00% availability
- 10% packet loss   = 90.00% availability
```

**SLA Mapping:**

| SLA Tier | Required Availability | Max Packet Loss | Downtime/Month |
|----------|----------------------|-----------------|----------------|
| Five Nines | 99.999% | < 0.001% | 26 seconds |
| Four Nines | 99.99% | < 0.01% | 4.3 minutes |
| Three Nines | 99.9% | < 0.1% | 43 minutes |
| Two Nines | 99% | < 1% | 7.2 hours |

## Metric Tags

All WebApp Ping metrics include standard tags for filtering, grouping, and aggregation.

### Standard Tags

| Tag Name | Type | Description | Example |
|----------|------|-------------|---------|
| `hostname` | string | Agent hostname | `web-monitor-01` |
| `os` | string | Operating system | `linux`, `windows`, `darwin` |
| `arch` | string | CPU architecture | `amd64`, `arm64`, `386` |
| `url` | string | Target URL monitored | `https://example.com` |
| `probe_name` | string | Probe identifier | `ping_webapp` |

### Tag Usage Examples

**Filter by specific URL:**
```promql
averageLatency{url="https://example.com"}
packetLoss{url="https://api.example.com"}
```

**Filter by OS:**
```promql
averageLatency{os="linux"}
packetLoss{os="windows"}
```

**Aggregate across all URLs:**
```promql
avg(averageLatency{probe_name="ping_webapp"})
max(packetLoss{probe_name="ping_webapp"})
```

**Multi-URL dashboard queries:**
```promql
# Show latency for all monitored URLs
averageLatency{probe_name="ping_webapp"}

# Show packet loss for production URLs only
packetLoss{url=~"https://prod-.*"}

# Alert on any URL with high packet loss
packetLoss{probe_name="ping_webapp"} > 5
```

## Calculation Details

### DNS Resolution

Before each collection, the probe resolves the hostname from the URL:

```
URL: https://app.example.com:8443/api/v1
  ↓
Hostname: app.example.com
  ↓
DNS Lookup: app.example.com → 192.168.1.100
  ↓
Ping Target: 192.168.1.100
```

**Resolution Details:**
- **Cache**: Go DNS resolver handles caching (typically 30-300 seconds)
- **IPv4/IPv6**: First resolved IP is used (may be IPv4 or IPv6)
- **Failures**: DNS resolution failures result in probe error (no metrics)

### ICMP Ping Process

The probe executes platform-specific ping commands:

```
1. Resolve hostname to IP address
2. Execute: ping -n 10 {ip} (Windows) or ping -c 10 {ip} (Unix)
3. Wait for command completion (~1-3 seconds)
4. Parse output using regex patterns
5. Extract averageLatency and packetLoss
6. Create data points with tags
7. Send to storage strategies
```

**Ping Parameters:**
- **Packet count**: 10 packets per collection
- **Timeout**: System default (typically 1-4 seconds per packet)
- **Packet size**: System default (typically 32-64 bytes)
- **TTL**: System default (typically 64-128)

### Platform-Specific Parsing

**Windows Output:**
```
Pinging 192.168.1.100 with 32 bytes of data:
Reply from 192.168.1.100: bytes=32 time=42ms TTL=64
Reply from 192.168.1.100: bytes=32 time=43ms TTL=64
...

Ping statistics for 192.168.1.100:
    Packets: Sent = 10, Received = 9, Lost = 1 (10% loss),
Approximate round trip times in milli-seconds:
    Minimum = 40ms, Maximum = 45ms, Average = 42ms
```

Regex extraction:
- Packet Loss: `Lost = \d+ \((\d+)%` → `10%`
- Average Latency: `Average = (\d+)ms` → `42ms`

**Linux Output:**
```
PING 192.168.1.100 (192.168.1.100) 56(84) bytes of data.
64 bytes from 192.168.1.100: icmp_seq=1 ttl=64 time=42.1 ms
64 bytes from 192.168.1.100: icmp_seq=2 ttl=64 time=43.2 ms
...

--- 192.168.1.100 ping statistics ---
10 packets transmitted, 9 received, 10.0% packet loss, time 9001ms
rtt min/avg/max/mdev = 40.2/42.1/45.3/1.2 ms
```

Regex extraction:
- Packet Loss: `(\d+\.\d+|\d+)% packet loss` → `10.0%`
- Average Latency: `rtt min/avg/max/mdev = \d+\.\d+/(\d+\.\d+)/` → `42.1ms`

**macOS Output:**
```
PING 192.168.1.100 (192.168.1.100): 56 data bytes
64 bytes from 192.168.1.100: icmp_seq=0 ttl=64 time=42.123 ms
64 bytes from 192.168.1.100: icmp_seq=1 ttl=64 time=43.234 ms
...

--- 192.168.1.100 ping statistics ---
10 packets transmitted, 9 packets received, 10.0% packet loss
round-trip min/avg/max/stddev = 40.234/42.123/45.345/1.234 ms
```

Regex extraction:
- Packet Loss: `(\d+\.\d+|\d+)% packet loss` → `10.0%`
- Average Latency: `round-trip min/avg/max/stddev = \d+\.\d+/(\d+\.\d+)/` → `42.123ms`

### Error Handling

**DNS Resolution Failure:**
```
Error: error retrieving web app IP address: failed to resolve hostname: no such host
Result: No metrics collected, error logged
```

**Ping Command Failure:**
```
Error: error collecting ping data: ping command failed: exit status 1
Result: No metrics collected, error logged
```

**100% Packet Loss:**
```
Result: averageLatency = 0.0, packetLoss = 100.0
Note: Average latency is 0 when all packets lost (no successful responses)
```

## Use Cases by Metric

### Network Performance Monitoring

**Primary Metrics:**
- `averageLatency` - Track latency trends over time
- `packetLoss` - Monitor network reliability

**Alert Configuration:**
```yaml
# High latency alert
- alert: HighWebAppLatency
  expr: averageLatency{probe_name="ping_webapp"} > 100
  for: 5m
  annotations:
    summary: "High latency to {{ $labels.url }}"
    description: "Average latency is {{ $value }}ms (threshold: 100ms)"

# Packet loss alert
- alert: WebAppPacketLoss
  expr: packetLoss{probe_name="ping_webapp"} > 5
  for: 2m
  annotations:
    summary: "Packet loss detected to {{ $labels.url }}"
    description: "Packet loss is {{ $value }}% (threshold: 5%)"
```

### SLA Compliance Tracking

**Metrics:**
- `packetLoss` - Calculate availability percentage
- `averageLatency` - Verify latency SLAs

**SLA Dashboard Example:**
```promql
# Current availability (over last hour)
100 - avg_over_time(packetLoss{url="https://app.example.com"}[1h])

# 99.9% SLA compliance check
(100 - avg_over_time(packetLoss{url="https://app.example.com"}[30d])) > 99.9

# Average latency vs. SLA
avg_over_time(averageLatency{url="https://app.example.com"}[30d]) < 100
```

### Multi-Site Monitoring

**Metrics:**
- `averageLatency` per URL - Compare performance across sites
- `packetLoss` per URL - Identify site-specific issues

**Dashboard Queries:**
```promql
# Latency comparison across all sites
averageLatency{url=~"https://.*example.com"}

# Worst performing site (highest latency)
topk(1, averageLatency{probe_name="ping_webapp"})

# Sites with packet loss
packetLoss{probe_name="ping_webapp"} > 0
```

### Capacity Planning

**Metrics:**
- `averageLatency` trends - Identify when additional capacity needed
- `packetLoss` patterns - Detect network saturation

**Analysis Queries:**
```promql
# Latency growth rate (week over week)
(avg_over_time(averageLatency[7d]) - avg_over_time(averageLatency[7d] offset 7d))
/ avg_over_time(averageLatency[7d] offset 7d) * 100

# Peak latency hours
max_over_time(averageLatency[1h])
```

### Troubleshooting

**Metrics:**
- `averageLatency` - Detect latency spikes
- `packetLoss` - Identify connectivity issues

**Diagnostic Queries:**
```promql
# Latency spike detection (current vs. baseline)
averageLatency / avg_over_time(averageLatency[24h]) > 2

# Intermittent packet loss
rate(packetLoss[5m]) > 0 and rate(packetLoss[5m]) < 100

# Complete outage detection
packetLoss == 100
```

## Platform Differences

### Metric Precision

| Platform | Latency Precision | Notes |
|----------|------------------|-------|
| Windows | Whole milliseconds (e.g., `42ms`) | Less precise |
| Linux | Three decimals (e.g., `42.123ms`) | High precision |
| macOS | Three decimals (e.g., `42.123ms`) | High precision |

**Impact:** Historical trending and analysis may show slight variations between platforms due to precision differences.

### Command Variations

| Platform | Command | Count Flag | Output Format |
|----------|---------|------------|---------------|
| Windows | `ping` | `-n 10` | Text (English) |
| Linux | `ping` | `-c 10` | Text (English) |
| macOS | `ping` | `-c 10` | Text (English) |

**Note:** The probe assumes English output. Non-English locale systems may require locale override.

### Timeout Behavior

| Platform | Default Timeout | Customizable |
|----------|----------------|--------------|
| Windows | 4 seconds per packet | No |
| Linux | System default (varies) | No |
| macOS | System default (varies) | No |

**Impact:** Total collection time varies by platform (typically 1-3 seconds for 10 packets).

### IPv6 Support

All platforms support IPv6 if DNS resolves to IPv6 address:
- **Automatic**: Probe uses first resolved IP (IPv4 or IPv6)
- **Transparent**: No configuration needed
- **Compatibility**: IPv6 ping works identically to IPv4

## Monitoring Best Practices

### Alert Configurations

**Basic Alerts:**
```yaml
# High latency (warning)
- alert: WebAppLatencyWarning
  expr: averageLatency{probe_name="ping_webapp"} > 100
  for: 5m
  labels:
    severity: warning
  annotations:
    summary: "Increased latency to {{ $labels.url }}"

# High latency (critical)
- alert: WebAppLatencyCritical
  expr: averageLatency{probe_name="ping_webapp"} > 200
  for: 5m
  labels:
    severity: critical
  annotations:
    summary: "Severe latency to {{ $labels.url }}"

# Packet loss (warning)
- alert: WebAppPacketLossWarning
  expr: packetLoss{probe_name="ping_webapp"} > 5
  for: 2m
  labels:
    severity: warning
  annotations:
    summary: "Packet loss detected to {{ $labels.url }}"

# Packet loss (critical)
- alert: WebAppPacketLossCritical
  expr: packetLoss{probe_name="ping_webapp"} > 10
  for: 2m
  labels:
    severity: critical
  annotations:
    summary: "Severe packet loss to {{ $labels.url }}"

# Complete outage
- alert: WebAppUnreachable
  expr: packetLoss{probe_name="ping_webapp"} == 100
  for: 1m
  labels:
    severity: critical
  annotations:
    summary: "{{ $labels.url }} is unreachable"
```

### Dashboard Panels

**Essential Panels:**

1. **Latency Time Series**
   ```promql
   averageLatency{probe_name="ping_webapp"}
   ```
   - Chart Type: Line graph
   - Unit: Milliseconds
   - Legend: `{{ url }}`

2. **Packet Loss Time Series**
   ```promql
   packetLoss{probe_name="ping_webapp"}
   ```
   - Chart Type: Line graph
   - Unit: Percent
   - Legend: `{{ url }}`

3. **Current Availability**
   ```promql
   100 - packetLoss{probe_name="ping_webapp"}
   ```
   - Chart Type: Gauge
   - Unit: Percent
   - Thresholds: >99.9% (green), 99-99.9% (yellow), <99% (red)

4. **Latency Distribution**
   ```promql
   histogram_quantile(0.95, rate(averageLatency_bucket[5m]))
   histogram_quantile(0.99, rate(averageLatency_bucket[5m]))
   ```
   - Chart Type: Heatmap or line graph
   - Shows p95 and p99 latency

5. **Multi-URL Comparison**
   ```promql
   averageLatency{probe_name="ping_webapp"}
   ```
   - Chart Type: Bar gauge
   - Shows current latency for all monitored URLs

### Collection Intervals

| Use Case | Interval | Reason |
|----------|----------|--------|
| Critical services | 10-30s | Quick detection of issues |
| Standard monitoring | 30-60s | Balance accuracy and overhead |
| Background monitoring | 60-300s | Low-priority services |

### Metric Retention

Recommended retention policies:

| Resolution | Retention | Use Case |
|------------|-----------|----------|
| Raw (30s samples) | 7-30 days | Recent troubleshooting |
| 5-minute aggregates | 90 days | Medium-term analysis |
| 1-hour aggregates | 1 year | Long-term trending |

## Related Documentation

- [WebApp Ping Probe README](./README.md) - Configuration and quick start
- [Load WebApp Probe](../load_webapp/README.md) - HTTP/HTTPS monitoring
- [Network Probe](../network/README.md) - Network interface monitoring
- [System Monitoring Guide](../../guides/system-monitoring.md) - Comprehensive monitoring
- [Troubleshooting Guide](../../troubleshooting/) - Common issues and solutions
