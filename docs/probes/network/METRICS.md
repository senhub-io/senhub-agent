# Gateway Ping Probe - Metrics Reference

## Introduction

The Gateway Ping probe collects network connectivity metrics by sending ICMP ping packets to the default gateway. It measures round-trip latency and packet loss using platform-native ping commands (10 packets per collection).

## Collected Metrics

### Overview

| Metric Name | Display Name | Type | Unit | Description |
|-------------|--------------|------|------|-------------|
| `averageLatency` | Gateway Latency | gauge | ms | Average round-trip time over 10 ping packets |
| `packetLoss` | Gateway Packet Loss | gauge | % | Percentage of lost packets (0-100%) |

## Detailed Metric Descriptions

### 1. averageLatency

**Technical Name**: `averageLatency`  
**Display Name**: Gateway Latency  
**Type**: Gauge  
**Unit**: Milliseconds (ms)  
**Range**: 0-1000+ ms

**Description**:
Average round-trip time (RTT) for ICMP echo request/reply packets to the default gateway. Calculated from 10 ping packets sent per collection interval.

**Calculation**:
```
averageLatency = sum(rtt_1, rtt_2, ..., rtt_10) / 10
```

**Platform Implementation**:
- **Windows**: Parsed from `ping -n 10 <gateway>` output ("Average = XXXms")
- **Linux**: Parsed from `ping -c 10 <gateway>` output (rtt min/avg/max/mdev)
- **macOS**: Parsed from `ping -c 10 <gateway>` output (round-trip min/avg/max/stddev)

**Typical Values**:
- **LAN (Wired)**: 1-5 ms
- **LAN (WiFi)**: 3-10 ms
- **Quality Issue**: >10 ms
- **Problem**: >50 ms

**Tags**:
- `probe`: "ping_gateway"
- `hostname`: System hostname
- `os`: Operating system
- `arch`: CPU architecture

**Example**:
```json
{
  "name": "averageLatency",
  "value": 3.5,
  "unit": "ms",
  "tags": {
    "probe": "ping_gateway",
    "hostname": "server01"
  }
}
```

**Use Cases**:
- Detect network latency issues
- Monitor WiFi quality
- Identify router performance problems
- Track network degradation trends

**Troubleshooting**:
- **High values (>10ms on LAN)**: Network congestion, WiFi interference, router overload
- **Unstable values**: Intermittent network issues, switching problems
- **Timeout**: Gateway unreachable, routing issues

### 2. packetLoss

**Technical Name**: `packetLoss`  
**Display Name**: Gateway Packet Loss  
**Type**: Gauge  
**Unit**: Percentage (%)  
**Range**: 0-100%

**Description**:
Percentage of ICMP echo packets that did not receive a reply within the timeout period. Indicates network reliability and connectivity quality.

**Calculation**:
```
packetLoss = (packets_lost / packets_sent) * 100
             = (packets_lost / 10) * 100
```

**Platform Implementation**:
- **Windows**: Parsed from "Lost = X (...%)" in ping output
- **Linux**: Parsed from "X% packet loss" in ping output
- **macOS**: Parsed from "X% packet loss" in ping output

**Typical Values**:
- **Healthy**: 0%
- **Minor Issues**: 1-5%
- **Significant Problems**: 5-10%
- **Severe**: >10%

**Tags**:
- `probe`: "ping_gateway"
- `hostname`: System hostname
- `os`: Operating system
- `arch`: CPU architecture

**Example**:
```json
{
  "name": "packetLoss",
  "value": 0.0,
  "unit": "%",
  "tags": {
    "probe": "ping_gateway",
    "hostname": "server01"
  }
}
```

**Use Cases**:
- Detect connectivity issues
- Monitor network reliability
- Identify hardware problems
- Track SLA compliance

**Troubleshooting**:
- **1-5% loss**: Minor network congestion, WiFi issues
- **5-10% loss**: Cable problems, switch issues, heavy congestion
- **>10% loss**: Severe hardware failure, routing problems
- **100% loss**: Gateway unreachable, network disconnected

## Metric Tags

### Standard Tags

All metrics include these tags by default:

| Tag | Example | Description |
|-----|---------|-------------|
| `probe` | `ping_gateway` | Probe name identifier |
| `hostname` | `server01` | System hostname |
| `os` | `linux` | Operating system |
| `arch` | `amd64` | CPU architecture |

## Calculation Details

### Gateway Auto-Detection

The probe automatically detects the default gateway using platform-specific methods:

**Windows**:
```go
// Finds first non-loopback UP interface with IPv4 address
for _, iface := range net.Interfaces() {
    if iface.Flags&net.FlagUp != 0 && iface.Flags&net.FlagLoopback == 0 {
        // Get first IPv4 address
    }
}
```

**Linux/macOS**:
Same logic - scans network interfaces for first active non-loopback IPv4 interface.

### Ping Execution

**Command executed**:
- Windows: `ping -n 10 <gateway_ip>`
- Linux: `ping -c 10 <gateway_ip>`
- macOS: `ping -c 10 <gateway_ip>`

**Parsing**:
- Regex extraction of average latency and packet loss percentage
- Platform-specific output format handling
- Error handling for timeouts and permission issues

## Use Cases by Metric

### Network Connectivity Monitoring

**Metrics**: `averageLatency`, `packetLoss`  
**Alert**: `packetLoss > 0%`

Detect when gateway becomes unreachable or partially reachable.

### ISP Quality Tracking

**Metrics**: `averageLatency`, `packetLoss`  
**Alert**: `averageLatency > 10ms OR packetLoss > 1%`

Monitor internet connection quality and identify ISP-related issues.

### WiFi Performance

**Metrics**: `averageLatency`  
**Alert**: `averageLatency > 10ms`

Track WiFi signal quality by monitoring latency to local gateway.

### Hardware Failure Detection

**Metrics**: `packetLoss`  
**Alert**: `packetLoss > 5%`

Identify failing network hardware (cables, switches, NICs, router).

## Monitoring Best Practices

### Alert Configuration

**Basic Alerts**:
```yaml
alerts:
  - name: Gateway Unreachable
    condition: packetLoss > 50%
    severity: critical
    
  - name: High Latency
    condition: averageLatency > 50ms
    severity: warning
    
  - name: Packet Loss
    condition: packetLoss > 5%
    severity: warning
```

**Advanced Alerts**:
```yaml
alerts:
  - name: Intermittent Connectivity
    condition: packetLoss > 0% AND packetLoss < 50%
    duration: 5m
    severity: warning
    
  - name: Network Degradation
    condition: averageLatency > 10ms
    duration: 10m
    severity: info
```

### Dashboard Panels

**Essential Panels**:
1. **Gateway Latency Timeline** - Line chart of averageLatency over time
2. **Packet Loss Timeline** - Line chart of packetLoss over time
3. **Current Status** - Single stat showing current latency and loss
4. **Availability %** - Calculated from packet loss: `100 - packetLoss`

### Collection Intervals

| Interval | Use Case | Impact |
|----------|----------|--------|
| 10s | Real-time monitoring | Higher network usage |
| 30s | Standard monitoring (default) | Balanced |
| 60s | Low-frequency monitoring | Minimal impact |

## Advanced Analysis

### Network Health Score

Calculate overall network health from metrics:

```
health_score = 100 - (packetLoss * 10) - (averageLatency / 10)

Examples:
- latency=5ms, loss=0% → score = 99.5 (excellent)
- latency=15ms, loss=0% → score = 98.5 (good)
- latency=5ms, loss=5% → score = 49.5 (poor)
- latency=100ms, loss=10% → score = -10 (critical)
```

### Availability Calculation

```
availability = 100 - packetLoss

SLA Mapping:
- 99.9% (three nines) = 0.1% packet loss
- 99.0% (two nines) = 1% packet loss
- 95.0% = 5% packet loss
```

## Troubleshooting

### High Latency (>10ms on LAN)

**Symptoms**:
- `averageLatency` consistently >10ms on local network
- No packet loss

**Possible Causes**:
1. WiFi interference or weak signal
2. Router CPU/memory overload
3. Network congestion
4. Distance (if WAN/VPN)

**Diagnosis**:
```bash
# Test with continuous ping
ping -t <gateway>  # Windows
ping <gateway>     # Linux/macOS

# Check WiFi signal (macOS)
/System/Library/PrivateFrameworks/Apple80211.framework/Versions/Current/Resources/airport -I

# Check router status
# Access router admin interface
```

**Solutions**:
- Move closer to WiFi access point
- Switch to wired connection
- Upgrade router firmware
- Reduce network load

### Packet Loss

**Symptoms**:
- `packetLoss` > 0%
- Intermittent or consistent

**Possible Causes**:
1. Failing network cable
2. NIC hardware issues
3. Switch/router problems
4. Firewall dropping ICMP
5. Network congestion

**Diagnosis**:
```bash
# Continuous ping test
ping <gateway>

# Check cable connections
# Inspect physical cables for damage

# Check interface errors
netstat -i  # Linux/macOS
```

**Solutions**:
- Replace network cables
- Update NIC drivers
- Check switch port status
- Verify firewall rules allow ICMP
- Test different network ports

### Gateway Not Found

**Symptoms**:
- Error: "no default gateway found"
- Probe fails to collect metrics

**Possible Causes**:
1. No active network interfaces
2. Network disconnected
3. DHCP failure
4. Manual IP without gateway configured

**Diagnosis**:
```bash
# Windows
ipconfig /all
route print

# Linux
ip route show
ip addr show

# macOS
netstat -rn
ifconfig
```

**Solutions**:
- Connect to network
- Enable network interface
- Renew DHCP lease
- Configure static gateway

## Related Documentation

- [Network Probe](../system/network/) - Network interface metrics
- [WebApp Ping Probe](../webapp/PING-README.md) - HTTP/HTTPS monitoring
- [Load WebApp Probe](../webapp/LOAD-README.md) - Web performance monitoring
