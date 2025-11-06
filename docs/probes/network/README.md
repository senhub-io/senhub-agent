# Gateway Ping Probe - README

## Overview

The Gateway Ping probe monitors network connectivity to your default gateway by sending ICMP ping packets. It measures latency and packet loss to detect network issues, ISP problems, and routing failures.

## Quick Start

### Basic Configuration
```yaml
probes:
  - name: ping_gateway
    params:
      interval: 30
```

### Minimal Configuration
```yaml
probes:
  - name: ping_gateway
```

## Key Metrics

| Metric | Description | Unit | Typical Value |
|--------|-------------|------|---------------|
| `averageLatency` | Round-trip time to gateway | ms | 1-10 ms |
| `packetLoss` | Lost packets percentage | % | 0% |

## Platform Support

- ✅ Windows (ping -n 10)
- ✅ Linux (ping -c 10)
- ✅ macOS (ping -c 10)

Auto-detects default gateway on all platforms.

## Monitoring Integration

### PRTG
```yaml
storage:
  - name: http
    params:
      endpoints: ["prtg"]
```
Access: `http://localhost:8080/api/{key}/prtg/metrics`

### Nagios
```yaml
storage:
  - name: http
    params:
      endpoints: ["nagios"]
```
Returns: `OK - Gateway reachable | latency=5.2ms loss=0%`

### Grafana
Query: `ping_gateway_averageLatency{probe="ping_gateway"}`

## Use Cases

1. **Network Connectivity** - Detect local network failures
2. **ISP Quality** - Track internet connection stability  
3. **Routing Issues** - Identify gateway problems
4. **SLA Tracking** - Monitor network availability

## Troubleshooting

### No Gateway Found
Check network interfaces:
```bash
# Windows
ipconfig

# Linux/macOS
ip route show default
```

### High Latency
- Check WiFi signal strength
- Test with wired connection
- Verify router isn't overloaded

### Packet Loss
- Replace network cables
- Check for interference (WiFi)
- Verify firewall isn't blocking ICMP

### Linux Permission Error
```bash
sudo setcap cap_net_raw+ep /usr/bin/ping
```

## Alert Thresholds

| Metric | Warning | Critical |
|--------|---------|----------|
| Latency | >10ms | >50ms |
| Packet Loss | >1% | >5% |

## Performance

- CPU: <0.1%
- Network: ~600 bytes/30s
- Overhead: Negligible

## Related Documentation

- [METRICS.md](./METRICS.md) - Complete metrics reference
- [Network Probe](../system/network/) - Interface metrics
- [WebApp Ping](../webapp/PING-README.md) - HTTP monitoring
