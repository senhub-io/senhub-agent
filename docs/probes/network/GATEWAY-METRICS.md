# Ping Gateway Metrics Complete Reference

This document provides a comprehensive reference for all metrics collected by the SenHub Agent Ping Gateway probe across different operating systems.

## Table of Contents

- [Introduction](#introduction)
- [Metrics Overview](#metrics-overview)
- [Average Latency](#average-latency)
- [Packet Loss](#packet-loss)
- [Metric Tags](#metric-tags)
- [Calculation Details](#calculation-details)
- [Use Cases by Metric](#use-cases-by-metric)
- [Alert Thresholds](#alert-thresholds)
- [Platform-Specific Behavior](#platform-specific-behavior)

## Introduction

The Ping Gateway probe monitors network connectivity to the default gateway by sending ICMP echo requests (ping) and measuring response times and packet loss. The probe collects metrics using platform-native ping commands:

- **Windows**: `ping -n 10 <ip>` command with Windows-specific output parsing
- **Linux**: `ping -c 10 <ip>` command with Linux-specific output parsing
- **macOS**: `ping -c 10 <ip>` command with macOS-specific output parsing

All metrics are collected at the configured interval (default: 30 seconds) and include standard tags for hostname, OS, and architecture.

## Metrics Overview

The Ping Gateway probe collects two primary metrics:

| Metric Name | Display Name | Type | Unit | Description |
|------------|--------------|------|------|-------------|
| `averageLatency` | Gateway Average Latency | Gauge | `ms` | Average round-trip time to gateway over 10 pings |
| `packetLoss` | Gateway Packet Loss | Gauge | `%` | Percentage of ICMP packets that did not receive a reply |

Both metrics are cross-platform and available on Windows, Linux, and macOS.

## Average Latency

### Overview

| Attribute | Value |
|-----------|-------|
| **Metric Name** | `averageLatency` |
| **Display Name** | Gateway Average Latency |
| **Channel Name** | `gateway_average_latency` |
| **Type** | Gauge |
| **Unit** | `ms` (milliseconds) |

**Tags:** `hostname`, `os`, `arch`, `probe_name=ping_gateway`

### Description

Average round-trip time (RTT) for ICMP echo request/reply packets sent to the default gateway. This metric is calculated from 10 consecutive ping attempts and represents the mean latency across all successful responses.

### Typical Values

| Network Condition | Latency Range | Interpretation |
|------------------|---------------|----------------|
| Excellent | < 1 ms | Local network, high-quality equipment |
| Good | 1-5 ms | Typical LAN performance |
| Acceptable | 5-20 ms | Normal for larger networks or longer cables |
| Poor | 20-50 ms | Network congestion or routing issues |
| Critical | > 50 ms | Serious network problems (investigate immediately) |

### Example Values

```json
{
  "name": "averageLatency",
  "value": 2.5,
  "unit": "ms",
  "timestamp": "2025-10-13T10:30:00Z"
}
```

**Interpretation:** 2.5 ms average latency indicates excellent local network performance with minimal delay.

### Use Cases

#### Network Performance Monitoring
Track baseline network performance and detect degradation:

```promql
# Prometheus query: Gateway latency over time
ping_gateway_average_latency{hostname="server01"}

# Alert on high latency
ping_gateway_average_latency > 50
```

**Baseline Establishment:**
- Monitor for 1-2 weeks to establish normal latency range
- Set warning threshold at baseline + 30ms
- Set critical threshold at baseline + 80ms

#### Detecting Network Congestion

High latency often indicates:
- **Switch/router overload**: Check network equipment CPU/memory
- **Bandwidth saturation**: Review bandwidth utilization metrics
- **QoS misconfiguration**: Verify Quality of Service settings
- **Broadcast storms**: Check for excessive broadcast traffic

**Correlation with Other Metrics:**
```promql
# Check if network bandwidth is saturated
rate(network_bytes_sent_total[5m]) + rate(network_bytes_received_total[5m])

# Correlate gateway latency with interface errors
ping_gateway_average_latency > 20 AND rate(network_errors_total[5m]) > 0
```

#### Troubleshooting Routing Issues

Latency patterns that indicate routing problems:

| Pattern | Likely Cause | Investigation |
|---------|--------------|---------------|
| Sudden spike | Route change | Check routing table updates |
| Gradual increase | Network congestion | Monitor bandwidth trends |
| Intermittent spikes | Faulty equipment | Check switch/router logs |
| Consistently high | Misconfiguration | Verify network topology |

#### Capacity Planning

Use historical latency data for capacity planning:

```promql
# Average latency over last 30 days
avg_over_time(ping_gateway_average_latency[30d])

# 95th percentile latency (service level target)
quantile_over_time(0.95, ping_gateway_average_latency[30d])

# Detect upward trends
predict_linear(ping_gateway_average_latency[30d], 86400 * 30)
```

### Platform-Specific Details

#### Windows
- **Command**: `ping -n 10 <gateway_ip>`
- **Parsing**: Extracts "Average = Xms" from statistics summary
- **Regex Pattern**: `Average = (\d+)ms`
- **Typical Range**: 0-100 ms

**Example Output:**
```
Minimum = 1ms, Maximum = 5ms, Average = 2ms
```

#### Linux
- **Command**: `ping -c 10 <gateway_ip>`
- **Parsing**: Extracts average from "rtt min/avg/max/mdev" line
- **Regex Pattern**: `rtt min/avg/max/mdev = (\d+\.\d+)/(\d+\.\d+)/(\d+\.\d+)/(\d+\.\d+)`
- **Typical Range**: 0.1-100 ms (higher precision)

**Example Output:**
```
rtt min/avg/max/mdev = 1.234/2.567/3.890/0.456 ms
```

#### macOS
- **Command**: `ping -c 10 <gateway_ip>`
- **Parsing**: Extracts average from "round-trip min/avg/max/stddev" line
- **Regex Pattern**: `round-trip min/avg/max/stddev = (\d+\.\d+)/(\d+\.\d+)/(\d+\.\d+)/(\d+\.\d+)`
- **Typical Range**: 0.1-100 ms (higher precision)

**Example Output:**
```
round-trip min/avg/max/stddev = 1.234/2.567/3.890/0.456 ms
```

### Alert Configuration

**PRTG Limits:**
```json
{
  "channel": "Gateway Average Latency",
  "unit": "ms",
  "float": 1,
  "LimitMaxWarning": 50,
  "LimitMaxError": 100,
  "LimitMode": 1
}
```

**Nagios Thresholds:**
```bash
# Performance data format
averageLatency=2.5ms;50;100;0;
```

**Prometheus Alerts:**
```yaml
- alert: HighGatewayLatency
  expr: ping_gateway_average_latency > 50
  for: 5m
  labels:
    severity: warning
  annotations:
    summary: "High gateway latency on {{ $labels.hostname }}"
    description: "Gateway latency is {{ $value }}ms (threshold: 50ms)"

- alert: CriticalGatewayLatency
  expr: ping_gateway_average_latency > 100
  for: 2m
  labels:
    severity: critical
  annotations:
    summary: "Critical gateway latency on {{ $labels.hostname }}"
    description: "Gateway latency is {{ $value }}ms (threshold: 100ms)"
```

## Packet Loss

### Overview

| Attribute | Value |
|-----------|-------|
| **Metric Name** | `packetLoss` |
| **Display Name** | Gateway Packet Loss |
| **Channel Name** | `gateway_packet_loss` |
| **Type** | Gauge |
| **Unit** | `%` (percentage) |

**Tags:** `hostname`, `os`, `arch`, `probe_name=ping_gateway`

### Description

Percentage of ICMP echo request packets that did not receive a reply within the timeout period. This metric is calculated from 10 consecutive ping attempts and represents the failure rate.

**Calculation:**
```
Packet Loss % = (Failed Pings / Total Pings) × 100
Packet Loss % = (Lost Packets / 10) × 100
```

### Typical Values

| Packet Loss | Network Condition | Action Required |
|------------|-------------------|-----------------|
| 0% | Excellent | None - Normal operation |
| 1-2% | Minor issues | Monitor - May be transient |
| 3-5% | Degraded | Investigate - Network problems |
| 6-10% | Severe issues | Urgent - Significant connectivity problems |
| > 10% | Critical | Immediate action - Network failure |
| 100% | Complete failure | Emergency - No connectivity |

### Example Values

```json
{
  "name": "packetLoss",
  "value": 0.0,
  "unit": "%",
  "timestamp": "2025-10-13T10:30:00Z"
}
```

**Interpretation:** 0% packet loss indicates perfect connectivity with no dropped packets.

### Use Cases

#### Network Reliability Monitoring

Track network reliability and detect connectivity issues:

```promql
# Prometheus query: Packet loss over time
ping_gateway_packet_loss{hostname="server01"}

# Alert on any packet loss
ping_gateway_packet_loss > 0
```

**Service Level Objectives (SLO):**
- **Gold tier**: < 0.1% packet loss (99.9% reliability)
- **Silver tier**: < 1% packet loss (99% reliability)
- **Bronze tier**: < 5% packet loss (95% reliability)

#### Detecting Hardware Failures

Packet loss patterns that indicate hardware problems:

| Pattern | Likely Cause | Investigation |
|---------|--------------|---------------|
| Intermittent 10-20% | Cable issues | Check/replace network cables |
| Consistent 50% | Duplex mismatch | Verify auto-negotiation settings |
| Random bursts | Switch/NIC failure | Check hardware logs and statistics |
| Gradual increase | Equipment degradation | Monitor temperature, replace hardware |

#### Network Quality Assessment

Assess overall network quality:

```promql
# Calculate network uptime percentage
100 - avg_over_time(ping_gateway_packet_loss[24h])

# Detect repeated packet loss events
count_over_time(ping_gateway_packet_loss{job="gateway"} > 1[1h])

# Network availability SLA
(count_over_time(ping_gateway_packet_loss[30d] == 0) / count_over_time(ping_gateway_packet_loss[30d])) * 100
```

#### Troubleshooting Network Issues

**Diagnostic Steps for Packet Loss:**

1. **Verify Physical Layer:**
   ```bash
   # Linux: Check interface errors
   ip -s link show

   # Windows: Check interface statistics
   netsh interface ipv4 show subinterfaces level=verbose
   ```

2. **Check for Duplex Mismatch:**
   ```bash
   # Linux: Check duplex settings
   ethtool eth0 | grep Duplex

   # Windows: Check adapter settings
   Get-NetAdapter | Select Name, LinkSpeed, DuplexMode
   ```

3. **Monitor for Broadcast Storms:**
   ```bash
   # Linux: Monitor broadcast packets
   tcpdump -i eth0 broadcast

   # Windows: Use Performance Monitor
   # Counter: Network Interface\Packets Received Broadcast/sec
   ```

4. **Test Cable Quality:**
   - Replace suspect cables
   - Test with known-good cables
   - Use cable tester for validation

#### Capacity Planning

Use packet loss trends for capacity planning:

```promql
# Packet loss trend over time
rate(ping_gateway_packet_loss[7d])

# Identify peak packet loss times
max_over_time(ping_gateway_packet_loss[1h])

# Correlate with bandwidth utilization
(ping_gateway_packet_loss > 1) AND (rate(network_bytes_total[5m]) > 100000000)
```

### Platform-Specific Details

#### Windows
- **Command**: `ping -n 10 <gateway_ip>`
- **Parsing**: Extracts percentage from "Lost = X (Y%)" in statistics
- **Regex Pattern**: `Lost = (\d+) \((\d+)%\)`
- **Resolution**: Integer percentage (0%, 10%, 20%, etc.)

**Example Output:**
```
Packets: Sent = 10, Received = 9, Lost = 1 (10% loss)
```

#### Linux
- **Command**: `ping -c 10 <gateway_ip>`
- **Parsing**: Extracts percentage from "X% packet loss" in statistics
- **Regex Pattern**: `(\d+\.\d+|\d+)% packet loss`
- **Resolution**: Decimal percentage (0%, 10.0%, 20.5%, etc.)

**Example Output:**
```
10 packets transmitted, 9 received, 10% packet loss, time 9010ms
```

#### macOS
- **Command**: `ping -c 10 <gateway_ip>`
- **Parsing**: Extracts percentage from "X% packet loss" in statistics
- **Regex Pattern**: `(\d+\.\d+|\d+)% packet loss`
- **Resolution**: Decimal percentage (0%, 10.0%, 20.5%, etc.)

**Example Output:**
```
10 packets transmitted, 9 packets received, 10.0% packet loss
```

### Alert Configuration

**PRTG Limits:**
```json
{
  "channel": "Gateway Packet Loss",
  "unit": "%",
  "float": 1,
  "LimitMaxWarning": 1,
  "LimitMaxError": 5,
  "LimitMode": 1
}
```

**Nagios Thresholds:**
```bash
# Performance data format
packetLoss=0.0%;1;5;0;100
```

**Prometheus Alerts:**
```yaml
- alert: GatewayPacketLoss
  expr: ping_gateway_packet_loss > 1
  for: 2m
  labels:
    severity: warning
  annotations:
    summary: "Gateway packet loss detected on {{ $labels.hostname }}"
    description: "Packet loss is {{ $value }}% (threshold: 1%)"

- alert: CriticalGatewayPacketLoss
  expr: ping_gateway_packet_loss > 5
  for: 1m
  labels:
    severity: critical
  annotations:
    summary: "Critical gateway packet loss on {{ $labels.hostname }}"
    description: "Packet loss is {{ $value }}% (threshold: 5%)"

- alert: GatewayUnreachable
  expr: ping_gateway_packet_loss == 100
  for: 30s
  labels:
    severity: critical
  annotations:
    summary: "Gateway unreachable on {{ $labels.hostname }}"
    description: "100% packet loss - complete network failure"
```

## Metric Tags

All Ping Gateway metrics include standard tags for filtering and aggregation.

### Standard Tags

| Tag Name | Type | Description | Example |
|----------|------|-------------|---------|
| `hostname` | string | System hostname | `web-server-01` |
| `os` | string | Operating system | `linux`, `windows`, `darwin` |
| `arch` | string | CPU architecture | `amd64`, `arm64`, `386` |
| `probe_name` | string | Probe identifier | `ping_gateway` |

### Tag Usage Examples

**Filter by Operating System:**
```promql
ping_gateway_average_latency{os="linux"}
```

**Filter by Hostname:**
```promql
ping_gateway_packet_loss{hostname="web-01"}
```

**Aggregate Across All Hosts:**
```promql
avg(ping_gateway_average_latency)
```

**Group by OS:**
```promql
sum(ping_gateway_packet_loss) by (os)
```

## Calculation Details

### Average Latency Calculation

The probe sends **10 ICMP echo requests** to the gateway and calculates the average RTT from successful responses.

**Algorithm:**
```
1. Send 10 ICMP echo request packets
2. Wait for responses (timeout: platform default)
3. Measure RTT for each successful response
4. Calculate average:

   Average Latency = (RTT₁ + RTT₂ + ... + RTTₙ) / n

   Where:
   - RTTᵢ = Round-trip time for successful ping i
   - n = Number of successful responses
```

**Example Calculation:**

```
Ping results (ms):
Ping 1: 2.5 ms
Ping 2: 3.1 ms
Ping 3: 2.8 ms
Ping 4: 2.9 ms
Ping 5: 3.2 ms
Ping 6: 2.7 ms
Ping 7: 3.0 ms
Ping 8: 2.6 ms
Ping 9: 2.9 ms
Ping 10: 3.1 ms

Average Latency = (2.5 + 3.1 + 2.8 + 2.9 + 3.2 + 2.7 + 3.0 + 2.6 + 2.9 + 3.1) / 10
Average Latency = 28.8 / 10
Average Latency = 2.88 ms
```

**Note:** If some pings fail, the average is calculated only from successful responses.

### Packet Loss Calculation

Packet loss is calculated as the percentage of failed ICMP echo requests.

**Algorithm:**
```
1. Send 10 ICMP echo request packets
2. Count successful responses (n_success)
3. Count failed responses (n_failed = 10 - n_success)
4. Calculate packet loss:

   Packet Loss % = (n_failed / 10) × 100

   Where:
   - n_failed = Number of packets without response
   - Total packets = 10 (constant)
```

**Example Calculations:**

| Sent | Received | Lost | Packet Loss % |
|------|----------|------|---------------|
| 10 | 10 | 0 | 0% |
| 10 | 9 | 1 | 10% |
| 10 | 8 | 2 | 20% |
| 10 | 5 | 5 | 50% |
| 10 | 0 | 10 | 100% |

### Precision and Rounding

- **Average Latency**: Reported with 1-2 decimal places (platform-dependent)
  - Windows: Integer milliseconds (e.g., 3 ms)
  - Linux/macOS: Decimal milliseconds (e.g., 2.88 ms)

- **Packet Loss**: Reported as percentage with 1 decimal place
  - Range: 0.0% to 100.0%
  - Resolution: 10% increments (since only 10 packets sent)

## Use Cases by Metric

### Network Performance Monitoring

| Goal | Primary Metric | Secondary Metric | Alert Threshold |
|------|----------------|------------------|-----------------|
| Baseline establishment | `averageLatency` | `packetLoss` | N/A (observe) |
| Performance degradation | `averageLatency` | `packetLoss` | Latency > baseline + 30ms |
| Connectivity issues | `packetLoss` | `averageLatency` | Loss > 1% |

### Troubleshooting Scenarios

| Symptom | Likely Cause | Metrics to Check | Investigation Steps |
|---------|--------------|------------------|---------------------|
| High latency, no loss | Congestion | `averageLatency` | Check bandwidth utilization |
| Low latency, high loss | Hardware issue | `packetLoss` | Check cables, switch ports |
| Both high | Network failure | Both | Check gateway status, routing |
| Intermittent spikes | Faulty equipment | Both (trend) | Monitor over time, check logs |

### Service Level Management

**Define SLAs based on metrics:**

| Service Tier | Max Latency | Max Packet Loss | Uptime Target |
|-------------|-------------|-----------------|---------------|
| Gold | 10 ms | 0.1% | 99.99% |
| Silver | 30 ms | 1% | 99.9% |
| Bronze | 50 ms | 5% | 99% |

**Prometheus SLA Queries:**
```promql
# Calculate monthly uptime percentage
(count_over_time(ping_gateway_packet_loss[30d] == 0) / count_over_time(ping_gateway_packet_loss[30d])) * 100

# Verify latency SLA compliance (Gold tier: < 10ms)
(count_over_time(ping_gateway_average_latency[30d] < 10) / count_over_time(ping_gateway_average_latency[30d])) * 100
```

## Alert Thresholds

### Recommended Thresholds

#### Average Latency

| Threshold Level | Value | Duration | Severity | Action |
|----------------|-------|----------|----------|--------|
| Warning | > 50 ms | 5 minutes | Low | Monitor, investigate if sustained |
| Error | > 100 ms | 2 minutes | High | Investigate immediately |
| Critical | > 200 ms | 30 seconds | Critical | Urgent response required |

**Rationale:**
- **50 ms**: Noticeable network delay, indicates congestion or routing issues
- **100 ms**: Significant performance degradation affecting applications
- **200 ms**: Severe network problems requiring immediate attention

#### Packet Loss

| Threshold Level | Value | Duration | Severity | Action |
|----------------|-------|----------|----------|--------|
| Warning | > 1% | 2 minutes | Medium | Investigate network quality |
| Error | > 5% | 1 minute | High | Immediate investigation required |
| Critical | > 10% | 30 seconds | Critical | Network failure, urgent response |
| Emergency | 100% | 30 seconds | Emergency | Complete connectivity loss |

**Rationale:**
- **1%**: Minor packet loss may indicate early hardware or configuration issues
- **5%**: Significant packet loss affecting application performance
- **10%**: Severe network problems causing application failures
- **100%**: Complete network failure requiring immediate remediation

### Environment-Specific Adjustments

Different environments may require different thresholds:

#### High-Performance Networks (Data Centers)

```yaml
# Stricter thresholds for critical infrastructure
averageLatency:
  warning: 10 ms
  error: 30 ms
  critical: 50 ms

packetLoss:
  warning: 0.1%
  error: 1%
  critical: 3%
```

#### Standard Enterprise Networks

```yaml
# Balanced thresholds for business networks
averageLatency:
  warning: 50 ms
  error: 100 ms
  critical: 200 ms

packetLoss:
  warning: 1%
  error: 5%
  critical: 10%
```

#### Remote/Branch Offices

```yaml
# Relaxed thresholds for remote sites
averageLatency:
  warning: 100 ms
  error: 200 ms
  critical: 500 ms

packetLoss:
  warning: 3%
  error: 10%
  critical: 20%
```

## Platform-Specific Behavior

### Windows

**Command Execution:**
```cmd
ping -n 10 192.168.1.1
```

**Sample Output:**
```
Pinging 192.168.1.1 with 32 bytes of data:
Reply from 192.168.1.1: bytes=32 time=2ms TTL=64
Reply from 192.168.1.1: bytes=32 time=3ms TTL=64
Reply from 192.168.1.1: bytes=32 time=2ms TTL=64
...

Ping statistics for 192.168.1.1:
    Packets: Sent = 10, Received = 10, Lost = 0 (0% loss),
Approximate round trip times in milli-seconds:
    Minimum = 1ms, Maximum = 5ms, Average = 2ms
```

**Parsing Logic:**
- **Latency**: Extract from "Average = Xms"
- **Packet Loss**: Extract from "Lost = X (Y%)"

**Characteristics:**
- Integer millisecond precision
- Includes TTL (Time To Live) information
- Reports min/max/average statistics

### Linux

**Command Execution:**
```bash
ping -c 10 192.168.1.1
```

**Sample Output:**
```
PING 192.168.1.1 (192.168.1.1) 56(84) bytes of data.
64 bytes from 192.168.1.1: icmp_seq=1 ttl=64 time=2.45 ms
64 bytes from 192.168.1.1: icmp_seq=2 ttl=64 time=2.67 ms
64 bytes from 192.168.1.1: icmp_seq=3 ttl=64 time=2.34 ms
...

--- 192.168.1.1 ping statistics ---
10 packets transmitted, 10 received, 0% packet loss, time 9010ms
rtt min/avg/max/mdev = 2.123/2.567/3.890/0.456 ms
```

**Parsing Logic:**
- **Latency**: Extract average from "rtt min/avg/max/mdev" line
- **Packet Loss**: Extract from "X% packet loss"

**Characteristics:**
- Decimal millisecond precision
- Reports standard deviation (mdev)
- Includes total transmission time

### macOS

**Command Execution:**
```bash
ping -c 10 192.168.1.1
```

**Sample Output:**
```
PING 192.168.1.1 (192.168.1.1): 56 data bytes
64 bytes from 192.168.1.1: icmp_seq=0 ttl=64 time=2.456 ms
64 bytes from 192.168.1.1: icmp_seq=1 ttl=64 time=2.678 ms
64 bytes from 192.168.1.1: icmp_seq=2 ttl=64 time=2.345 ms
...

--- 192.168.1.1 ping statistics ---
10 packets transmitted, 10 packets received, 0.0% packet loss
round-trip min/avg/max/stddev = 2.123/2.567/3.890/0.456 ms
```

**Parsing Logic:**
- **Latency**: Extract average from "round-trip min/avg/max/stddev" line
- **Packet Loss**: Extract from "X% packet loss"

**Characteristics:**
- Decimal millisecond precision
- Uses "round-trip" terminology (vs. Linux "rtt")
- Includes standard deviation (stddev)

### Cross-Platform Considerations

| Feature | Windows | Linux | macOS |
|---------|---------|-------|-------|
| Precision | Integer ms | Decimal ms | Decimal ms |
| Statistics | min/max/avg | min/avg/max/mdev | min/avg/max/stddev |
| Packet Loss Format | "Lost = X (Y%)" | "X% packet loss" | "X% packet loss" |
| Default Packet Size | 32 bytes | 56 bytes | 56 bytes |
| ICMP Sequence | No | Yes (icmp_seq) | Yes (icmp_seq) |

## Monitoring Best Practices

### Dashboard Design

**Essential Panels:**

1. **Gateway Latency Time Series**
   ```promql
   ping_gateway_average_latency{hostname="server01"}
   ```
   - Visualization: Line graph
   - Time range: Last 24 hours
   - Y-axis: 0-100 ms

2. **Packet Loss Time Series**
   ```promql
   ping_gateway_packet_loss{hostname="server01"}
   ```
   - Visualization: Bar graph
   - Time range: Last 24 hours
   - Y-axis: 0-10%

3. **Network Health Status**
   ```promql
   (ping_gateway_average_latency < 50) AND (ping_gateway_packet_loss == 0)
   ```
   - Visualization: Stat panel (OK/FAIL)
   - Color coding: Green (OK), Red (FAIL)

4. **Latency Distribution Histogram**
   ```promql
   histogram_quantile(0.95, ping_gateway_average_latency)
   ```
   - Visualization: Heatmap
   - Shows latency distribution over time

### Collection Intervals

| Monitoring Objective | Interval | Rationale |
|---------------------|----------|-----------|
| Real-time monitoring | 10-15 s | Detect transient issues |
| Standard monitoring | 30-60 s | Balance accuracy and overhead |
| Baseline establishment | 60-120 s | Long-term trending |
| Low-priority health check | 300-600 s | Periodic verification |

### Alert Tuning

**Reduce False Positives:**

```yaml
# Wait for sustained issues before alerting
- alert: HighGatewayLatency
  expr: ping_gateway_average_latency > 50
  for: 5m  # Alert only if condition persists for 5 minutes

# Use moving averages for smoother alerts
- alert: SustainedHighLatency
  expr: avg_over_time(ping_gateway_average_latency[10m]) > 50
  for: 2m
```

**Multi-Condition Alerts:**

```yaml
# Alert only when both latency and packet loss are problematic
- alert: NetworkDegradation
  expr: (ping_gateway_average_latency > 30) AND (ping_gateway_packet_loss > 1)
  for: 3m
  labels:
    severity: warning
```

### Correlation with Other Metrics

**Network Interface Errors:**
```promql
# Gateway issues coinciding with interface errors
(ping_gateway_packet_loss > 1) AND (rate(network_errors_total[5m]) > 0)
```

**Bandwidth Utilization:**
```promql
# High latency during bandwidth saturation
(ping_gateway_average_latency > 50) AND (rate(network_bytes_total[5m]) / network_interface_speed > 0.8)
```

**System Load:**
```promql
# Network issues during high system load
(ping_gateway_average_latency > 30) AND (node_load1 > 4)
```

## Related Documentation

- [Ping Gateway Probe README](./GATEWAY-README.md) - Configuration and quick start
- [Network Probe Metrics](./NETWORK-METRICS.md) - Interface-level network metrics
- [WebApp Probe Metrics](../webapp/PING-METRICS.md) - External connectivity monitoring
- [Network Troubleshooting Guide](../../troubleshooting/network.md) - Diagnostic procedures
- [Monitoring Best Practices](../../guides/monitoring-best-practices.md) - General monitoring guidance
