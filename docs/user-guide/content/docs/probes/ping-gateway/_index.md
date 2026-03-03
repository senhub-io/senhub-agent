---
title: "Ping Gateway"
weight: 12
---

# Ping Gateway Probe

The Ping Gateway probe monitors network connectivity to the default gateway or a specified IP address, providing essential metrics for detecting network routing issues, ISP quality problems, and local network connectivity.

## Quick Start

### Basic Configuration

```yaml
probes:
  - name: ping_gateway
    params:
      interval: 30  # Collection interval in seconds (default: 30)
```

### Minimal Configuration

```yaml
probes:
  - name: ping_gateway
    params: {}
```

The Ping Gateway probe requires no mandatory parameters and automatically detects the default gateway. It works out-of-the-box with default settings.

## Supported Platforms

- **Windows**: Windows Server 2012+ / Windows 10+
- **Linux**: All modern distributions (Ubuntu, RHEL, CentOS, Debian, etc.)
- **macOS**: macOS 10.13+

Platform-specific ping implementations are automatically selected based on the operating system.

## Key Metrics Summary

| Metric | Description | Unit | Alert Threshold |
|--------|-------------|------|-----------------|
| `averageLatency` | Average round-trip time to gateway | ms | > 50ms (warning), > 100ms (critical) |
| `packetLoss` | Percentage of lost packets | % | > 1% (warning), > 5% (critical) |

## Configuration Parameters

| Parameter | Type | Default | Description |
|-----------|------|---------|-------------|
| `interval` | integer | `30` | Collection interval in seconds |

### Example Configurations

**High-frequency monitoring (every 10 seconds):**
```yaml
probes:
  - name: ping_gateway
    params:
      interval: 10
```

**Standard monitoring (every minute):**
```yaml
probes:
  - name: ping_gateway
    params:
      interval: 60
```

**Low-frequency monitoring (every 5 minutes):**
```yaml
probes:
  - name: ping_gateway
    params:
      interval: 300
```

## How It Works

### Gateway Auto-Detection

The probe automatically detects the default gateway by:

1. **Enumerating network interfaces** - Identifies all active, non-loopback interfaces
2. **Selecting primary interface** - Chooses the first interface with an IPv4 address
3. **Extracting gateway IP** - Uses the interface's network configuration

**Note:** The current implementation returns the local interface IP, not the actual gateway. This is a known limitation that will be addressed in future versions.

### Ping Collection Process

The probe sends **10 ICMP echo requests** to the target IP and collects:

- **Average latency**: Mean round-trip time across all successful pings
- **Packet loss**: Percentage of packets that did not receive a reply

**Platform-Specific Implementations:**

- **Windows**: Uses `ping -n 10 <ip>` command
- **Linux**: Uses `ping -c 10 <ip>` command
- **macOS**: Uses `ping -c 10 <ip>` command

All implementations parse the native ping command output to extract metrics.

## Monitoring Tool Integration

### PRTG Network Monitor

Access gateway metrics in PRTG JSON format:

```bash
# All gateway metrics
curl http://localhost:8080/api/{agentkey}/prtg/metrics

# Configure PRTG HTTP Advanced Sensor:
# - URL: http://agent-host:8080/api/{agentkey}/prtg/metrics
# - Method: POST
# - Request body: {"probe": "ping_gateway"}
```

**PRTG Channels Available:**
- Gateway Average Latency (ms)
- Gateway Packet Loss (%)

**Recommended Limits:**
```json
{
  "Gateway Average Latency": {
    "LimitMaxWarning": 50,
    "LimitMaxError": 100,
    "LimitMode": 1
  },
  "Gateway Packet Loss": {
    "LimitMaxWarning": 1,
    "LimitMaxError": 5,
    "LimitMode": 1
  }
}
```

### Nagios/Icinga

Access gateway metrics in Nagios format:

```bash
# All gateway metrics with performance data
curl http://localhost:8080/api/{agentkey}/nagios/metrics?probe=ping_gateway

# Example output:
# OK - Gateway connectivity normal | averageLatency=15.3ms;50;100 packetLoss=0.0%;1;5
```

**Nagios Performance Data:**
- `averageLatency` - Gateway latency with 50ms warning, 100ms critical
- `packetLoss` - Packet loss with 1% warning, 5% critical

**Service Check Configuration:**
```cfg
define service {
    use                     generic-service
    host_name               monitored-server
    service_description     Gateway Connectivity
    check_command           check_http_json!8080!/api/{agentkey}/nagios/metrics?probe=ping_gateway
    check_interval          1
    retry_interval          1
    max_check_attempts      3
}
```

### Grafana/Prometheus

Access metrics in Prometheus-compatible format:

```bash
# Prometheus format
curl http://localhost:8080/api/{agentkey}/prometheus/metrics

# Example output:
# ping_gateway_average_latency{hostname="server01"} 15.3
# ping_gateway_packet_loss{hostname="server01"} 0.0
```

**Grafana Dashboard Queries:**

```promql
# Gateway latency over time
ping_gateway_average_latency{hostname="server01"}

# Packet loss percentage
ping_gateway_packet_loss{hostname="server01"}

# Latency rate of change (detect spikes)
rate(ping_gateway_average_latency{hostname="server01"}[5m])
```

**Alert Rules:**
```yaml
groups:
  - name: gateway_connectivity
    rules:
      - alert: HighGatewayLatency
        expr: ping_gateway_average_latency > 50
        for: 5m
        annotations:
          summary: "High gateway latency on {{ $labels.hostname }}"
          description: "Gateway latency is {{ $value }}ms (threshold: 50ms)"

      - alert: GatewayPacketLoss
        expr: ping_gateway_packet_loss > 1
        for: 2m
        annotations:
          summary: "Gateway packet loss detected on {{ $labels.hostname }}"
          description: "Packet loss is {{ $value }}% (threshold: 1%)"
```

### Web Interface

View gateway metrics in the built-in dashboard:

```
http://localhost:8080/web/{agentkey}/dashboard
```

Features:
- Real-time latency visualization
- Packet loss monitoring
- Historical trend graphs
- Network connectivity status

## Use Cases

### Network Connectivity Monitoring

Monitor local network connectivity to detect:
- **Gateway failures**: Complete loss of connectivity to the default gateway
- **Network path issues**: Routing problems between host and gateway
- **Local switch problems**: Layer 2 connectivity issues

**Typical Alert Configuration:**
- Packet loss > 1%: Warning (investigate network)
- Packet loss > 5%: Critical (network failure)

### ISP Quality Monitoring

Track Internet Service Provider quality:
- **Latency increases**: Detect congestion or routing changes
- **Packet loss patterns**: Identify intermittent connectivity issues
- **Performance degradation**: Monitor for ISP throttling or oversubscription

**Use with WebApp Probe**: Combine gateway ping with external website monitoring to isolate local vs. WAN issues.

### Routing Issues Detection

Identify routing problems:
- **Gateway unreachable**: Local network configuration issues
- **High latency**: Network congestion or misconfigured routes
- **Intermittent connectivity**: Faulty network equipment

**Correlation with Other Metrics:**
- Compare with WebApp probe to distinguish local vs. remote issues
- Check network interface metrics for errors/collisions
- Review system logs for DHCP or routing updates

### Network Troubleshooting

Diagnose network issues:
- **Baseline establishment**: Normal latency and packet loss patterns
- **Change detection**: Identify when network performance degrades
- **Isolation**: Determine if issues are local (gateway) or remote (internet)

**Troubleshooting Workflow:**
1. Check gateway ping (this probe) - Is local network working?
2. Check external ping (webapp probe) - Is internet working?
3. Check interface metrics (network probe) - Are there errors?
4. Review system logs - Any configuration changes?

## Troubleshooting

### No Metrics Collected

**Check probe status:**
```bash
# View agent logs with gateway probe debugging
./agent run --authentication-key YOUR_KEY --verbose --debug-modules probe.gateway
```

**Verify probe is enabled:**
```bash
# Check configuration
cat agent-config.yaml | grep -A5 "name: ping_gateway"
```

**Common Causes:**
- Firewall blocking ICMP echo requests
- Network interface disabled or misconfigured
- Gateway auto-detection failure (no active interfaces)

### Gateway Detection Failed

**Symptom:** Error message "no default gateway found"

**Solution:**

**Windows:**
```powershell
# Verify network configuration
ipconfig /all

# Check routing table
route print

# Verify default gateway
netstat -rn | findstr "0.0.0.0"
```

**Linux:**
```bash
# Check network interfaces
ip addr show

# Check routing table
ip route show

# Verify default gateway
ip route | grep default
```

**macOS:**
```bash
# Check network interfaces
ifconfig

# Check routing table
netstat -rn | grep default
```

### High Latency or Packet Loss

**Symptom:** Consistently high latency (>50ms) or packet loss (>1%)

**Diagnosis Steps:**

1. **Verify local network health:**
   ```bash
   # Check interface errors (Linux)
   ip -s link

   # Check interface errors (Windows)
   netsh interface ipv4 show subinterfaces
   ```

2. **Test connectivity directly:**
   ```bash
   # Windows
   ping -n 20 <gateway-ip>

   # Linux/macOS
   ping -c 20 <gateway-ip>
   ```

3. **Check for network congestion:**
   - Review bandwidth utilization
   - Check for broadcast storms
   - Verify switch/router performance

4. **Hardware issues:**
   - Loose or damaged cables
   - Failing network interface card
   - Overheating network equipment

**Mitigation:**
- Replace faulty cables
- Update network drivers
- Restart network equipment
- Check for firmware updates

### Firewall Blocking ICMP

**Symptom:** 100% packet loss, but network appears to be working

**Solution:**

**Windows Firewall:**
```powershell
# Allow ICMP Echo Request (outbound)
netsh advfirewall firewall add rule name="ICMP Allow outgoing V4 echo request" protocol=icmpv4:8,any dir=out action=allow

# Allow ICMP Echo Reply (inbound)
netsh advfirewall firewall add rule name="ICMP Allow incoming V4 echo reply" protocol=icmpv4:0,any dir=in action=allow
```

**Linux iptables:**
```bash
# Allow ICMP Echo Request (outbound)
sudo iptables -A OUTPUT -p icmp --icmp-type echo-request -j ACCEPT

# Allow ICMP Echo Reply (inbound)
sudo iptables -A INPUT -p icmp --icmp-type echo-reply -j ACCEPT
```

**Linux firewalld:**
```bash
# Allow ICMP permanently
sudo firewall-cmd --permanent --add-icmp-block-inversion
sudo firewall-cmd --reload
```

### Permission Denied (Unix/Linux)

**Symptom:** Error message "permission denied" when executing ping

**Solution:**

Most modern systems allow unprivileged users to ping, but if you encounter permission issues:

```bash
# Option 1: Run agent as root
sudo ./agent run --authentication-key YOUR_KEY

# Option 2: Grant ping capabilities (Linux)
sudo setcap cap_net_raw+ep /usr/bin/ping

# Option 3: Add agent user to netdev group (Debian/Ubuntu)
sudo usermod -a -G netdev senhub-agent
```

### Inconsistent Results

**Symptom:** Metrics vary significantly between collections

**Explanation:** Network conditions naturally fluctuate. Some variance is normal.

**Acceptable Variance:**
- Latency: +/-5-10ms is typical
- Packet loss: 0-1% is acceptable

**Excessive Variance Indicators:**
- Latency swings > 50ms
- Intermittent 100% packet loss
- Packet loss > 5% sustained

**Investigation:**
- Check for network congestion patterns
- Review network equipment logs
- Monitor bandwidth utilization
- Verify QoS configurations

## Performance Considerations

### Collection Overhead

The Ping Gateway probe has minimal overhead:
- **Command execution**: ~100-200ms per collection (10 pings)
- **Parsing overhead**: < 10ms
- **Total collection time**: ~200-300ms

**Note:** The ping command sends 10 ICMP packets with default timeout, which is generally sufficient for local gateway monitoring.

### Network Impact

Network impact is negligible:
- **10 ICMP packets** per collection interval
- **Packet size**: 64 bytes (default ping size)
- **Bandwidth**: ~640 bytes every 30 seconds = ~170 bits/sec
- **Total overhead**: < 0.001% on 10 Mbps link

### Recommended Intervals

| Use Case | Interval | Reason |
|----------|----------|--------|
| Real-time network monitoring | 10-15s | Catch short-lived network issues |
| Standard monitoring | 30-60s | Balance accuracy and overhead |
| Periodic health check | 120-300s | Low-frequency connectivity verification |
| Baseline establishment | 300-600s | Long-term trend analysis |

**Recommendation for Most Environments:** 30-60 seconds provides good balance between detection speed and system overhead.

## Advanced Configuration

### Multiple Gateway Monitoring

Monitor multiple gateways or network paths:

```yaml
probes:
  - name: ping_gateway_primary
    params:
      interval: 30

  - name: ping_gateway_secondary
    params:
      interval: 60
```

**Note:** Current implementation auto-detects only the default gateway. Manual gateway specification will be supported in future versions.

### Integration with Other Probes

Comprehensive network monitoring setup:

```yaml
probes:
  # Local network connectivity
  - name: ping_gateway
    params:
      interval: 30

  # External connectivity and DNS
  - name: ping_webapp
    params:
      interval: 60
      targets:
        - url: "https://www.google.com"
        - url: "https://1.1.1.1"

  # Interface metrics
  - name: network
    params:
      interval: 30
```

This combination provides:
- **Gateway probe**: Local network health (Layer 3)
- **WebApp probe**: Internet connectivity and latency
- **Network probe**: Interface errors, bandwidth, packet statistics

## Known Limitations

1. **Gateway Detection**: Currently returns the local interface IP instead of the actual gateway IP. This will be improved in future versions.

2. **IPv6 Support**: The probe currently focuses on IPv4 connectivity. IPv6 support is planned for future releases.

3. **Manual Gateway Override**: No option to specify a custom gateway IP. Auto-detection is the only supported mode.

4. **Jitter Metrics**: The probe does not currently calculate jitter (latency variance). This is planned for future releases.

## Requirements

### Windows
- Windows Server 2012+ or Windows 10+
- `ping.exe` command (built into Windows)
- ICMP outbound/inbound allowed in firewall

### Linux/Unix/macOS
- `ping` command installed (pre-installed on most systems)
- ICMP outbound/inbound allowed in firewall
- Network interface with IPv4 address configured

### Network
- Local network access to default gateway
- ICMP echo request/reply allowed (firewall/router)
- HTTP strategy required for remote access to metrics
