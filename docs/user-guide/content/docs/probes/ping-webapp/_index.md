---
title: "Ping WebApp"
weight: 10
---

# WebApp Ping Probe

The WebApp Ping probe monitors the network reachability and latency of web applications by resolving hostnames and performing ICMP ping tests against the target IP addresses.

## Quick Start

### Basic Configuration

```yaml
probes:
  - name: webapp_ping
    type: ping_webapp
    params:
      url: "https://example.com"
      interval: 30  # Collection interval in seconds (default: 30)
```

### Monitoring Multiple Web Applications

```yaml
probes:
  - name: webapp_ping_production
    type: ping_webapp
    params:
      url: "https://app.example.com"
      interval: 30

  - name: webapp_ping_api
    type: ping_webapp
    params:
      url: "https://api.example.com"
      interval: 60
```

The WebApp Ping probe requires only a URL parameter and performs DNS resolution followed by ICMP ping tests.

## Supported Platforms

- **Windows**: Windows Server 2012+ / Windows 10+
- **Linux**: All modern distributions (Ubuntu, RHEL, CentOS, Debian, etc.)
- **macOS**: macOS 10.13+
- **BSD**: FreeBSD, OpenBSD, NetBSD

Platform-specific ping commands are automatically used based on the operating system.

## Key Metrics Summary

| Metric | Description | Unit | Use Case |
|--------|-------------|------|----------|
| `averageLatency` | Average ICMP round-trip time | `ms` | Network latency monitoring |
| `packetLoss` | Percentage of lost ICMP packets | `%` | Network reliability monitoring |

## Configuration Parameters

| Parameter | Type | Required | Default | Description |
|-----------|------|----------|---------|-------------|
| `url` | string | **Yes** | - | Target web application URL (HTTP/HTTPS) |
| `interval` | integer | No | `30` | Collection interval in seconds |

### URL Parameter Format

The probe accepts any valid HTTP or HTTPS URL:
- `https://example.com` - Basic HTTPS URL
- `http://app.example.com` - HTTP URL
- `https://api.example.com:8443` - Custom port (port is ignored, only hostname used)
- `https://192.168.1.100` - Direct IP address

**Note:** The probe extracts the hostname from the URL and resolves it to an IP address for ping tests. The protocol (http/https) and port are not used in the actual ping operation.

### Example Configurations

**High-frequency monitoring (every 10 seconds):**
```yaml
probes:
  - name: webapp_ping_critical
    type: ping_webapp
    params:
      url: "https://critical-app.example.com"
      interval: 10
```

**Standard monitoring (every minute):**
```yaml
probes:
  - name: webapp_ping_standard
    type: ping_webapp
    params:
      url: "https://app.example.com"
      interval: 60
```

**Low-frequency monitoring (every 5 minutes):**
```yaml
probes:
  - name: webapp_ping_background
    type: ping_webapp
    params:
      url: "https://backup.example.com"
      interval: 300
```

## Monitoring Tool Integration

### PRTG Network Monitor

Access WebApp Ping metrics in PRTG JSON format:

```bash
# All WebApp Ping metrics
curl -X POST http://localhost:8080/api/{agentkey}/prtg/metrics \
  -H "Content-Type: application/json" \
  -d '{"probe": "ping_webapp"}'

# Configure PRTG HTTP Advanced Sensor:
# - URL: http://agent-host:8080/api/{agentkey}/prtg/metrics
# - Method: POST
# - Request body: {"probe": "ping_webapp"}
```

**PRTG Channels Available:**
- Average Latency (ms) - Tagged with URL
- Packet Loss (%) - Tagged with URL

### Nagios/Icinga

Access WebApp Ping metrics in Nagios format:

```bash
# All WebApp Ping metrics with performance data
curl http://localhost:8080/api/{agentkey}/nagios/metrics?probe=ping_webapp

# Example output:
# OK - WebApp Ping monitoring active | averageLatency=45.2ms;100;200 packetLoss=0%;5;10
```

**Nagios Performance Data:**
- `averageLatency` - Average latency with 100ms warning, 200ms critical
- `packetLoss` - Packet loss with 5% warning, 10% critical

### Grafana/Prometheus

Access metrics in Prometheus-compatible format:

```bash
# Prometheus format
curl http://localhost:8080/api/{agentkey}/prometheus/metrics

# Example output:
# ping_webapp_average_latency{url="https://example.com"} 45.2
# ping_webapp_packet_loss{url="https://example.com"} 0.0
```

### Web Interface

View WebApp Ping metrics in the built-in dashboard:

```
http://localhost:8080/web/{agentkey}/dashboard
```

Features:
- Real-time latency visualization
- Packet loss monitoring
- Per-URL metrics breakdown
- Historical trends

## Use Cases

### Availability Monitoring

Monitor web application availability by tracking:
- Network reachability (packet loss = 0%)
- Latency trends over time
- DNS resolution failures
- Network path issues

**Alert Thresholds:**
- Packet Loss > 5%: Warning (intermittent connectivity)
- Packet Loss > 10%: Critical (significant network issues)
- Average Latency > 100ms: Warning (high latency)
- Average Latency > 200ms: Critical (severe latency)

### SLA Compliance

Track service level objectives:
- 99.9% availability (packet loss < 0.1%)
- Maximum latency thresholds
- Network performance baselines
- Monthly uptime reporting

### Multi-Site Monitoring

Monitor applications across multiple locations:
```yaml
probes:
  - name: webapp_ping_us_east
    type: ping_webapp
    params:
      url: "https://us-east.example.com"
      interval: 30

  - name: webapp_ping_us_west
    type: ping_webapp
    params:
      url: "https://us-west.example.com"
      interval: 30

  - name: webapp_ping_eu
    type: ping_webapp
    params:
      url: "https://eu.example.com"
      interval: 30
```

### Network Path Troubleshooting

Diagnose network issues:
- High latency (routing issues, congestion)
- Packet loss (network instability)
- DNS resolution failures (DNS server issues)
- Firewall blocking ICMP (security policies)

### Load Balancer Health

Monitor load balancer endpoints:
```yaml
probes:
  - name: webapp_ping_lb1
    type: ping_webapp
    params:
      url: "https://lb1.example.com"
      interval: 30

  - name: webapp_ping_lb2
    type: ping_webapp
    params:
      url: "https://lb2.example.com"
      interval: 30
```

## Troubleshooting

### No Metrics Collected

**Check probe status:**
```bash
# View agent logs with WebApp probe debugging
./agent run --authentication-key YOUR_KEY --verbose --debug-modules probe.webapp
```

**Verify probe is enabled:**
```bash
# Check configuration
cat agent-config.yaml | grep -A5 "type: ping_webapp"
```

**Common Issues:**
1. **Invalid URL format**: Ensure URL is properly formatted (http:// or https://)
2. **Missing URL parameter**: The `url` parameter is required
3. **DNS resolution failure**: Check that hostname resolves correctly
4. **Network connectivity**: Verify agent has network access to target

### DNS Resolution Failures

**Symptom:** Error messages about hostname resolution

**Causes:**
- Invalid hostname in URL
- DNS server unreachable
- DNS resolution timeout
- Network connectivity issues

**Solution:**
1. Verify hostname resolution manually:
   ```bash
   # Linux/macOS
   nslookup app.example.com
   dig app.example.com

   # Windows
   nslookup app.example.com
   ```

2. Check DNS server configuration
3. Verify network connectivity
4. Test with IP address instead of hostname

### ICMP Ping Failures

**Symptom:** Error messages about ping command failures or 100% packet loss

**Common Causes:**

**1. ICMP Blocked by Firewall**
- Target firewall blocking ICMP packets
- Intermediate firewall/router blocking ICMP
- Cloud security group rules

**Solution:**
```bash
# Test ping manually
ping -c 4 app.example.com  # Linux/macOS
ping -n 4 app.example.com  # Windows
```

**2. Permission Denied (Linux/Unix)**

**Symptom:** "Operation not permitted" or "socket: permission denied"

**Solution:**
```bash
# Option 1: Run agent as root
sudo ./agent run --authentication-key YOUR_KEY

# Option 2: Grant raw socket capabilities (Linux)
sudo setcap cap_net_raw=eip ./agent

# Option 3: Allow ICMP for agent user
sudo sysctl -w net.ipv4.ping_group_range="0 2147483647"
```

**3. Windows: Ping Service Disabled**

**Solution:**
```powershell
# Verify Windows Firewall allows ICMP
netsh advfirewall firewall show rule name="File and Printer Sharing (Echo Request - ICMPv4-In)"

# Enable ICMP if disabled
netsh advfirewall firewall add rule name="ICMP Allow incoming V4 echo request" protocol=icmpv4:8,any dir=in action=allow
```

### High Packet Loss

**Symptom:** Consistently high packet loss (> 5%)

**Causes:**
- Network congestion
- Routing issues
- Unstable network path
- ICMP rate limiting
- Target server load shedding ICMP

**Diagnosis:**
```bash
# Test with multiple ping counts
ping -c 100 app.example.com

# Check route to target
traceroute app.example.com  # Linux/macOS
tracert app.example.com     # Windows

# Monitor continuous ping
ping app.example.com        # Ctrl+C to stop
```

**Solution:**
1. Investigate network path (traceroute)
2. Check for intermediate network issues
3. Verify target server ICMP handling
4. Consider using HTTP/HTTPS monitoring instead (Load WebApp probe)

### High Latency

**Symptom:** Average latency consistently high (> 100ms)

**Causes:**
- Geographic distance
- Network congestion
- Routing inefficiencies
- Server performance issues
- ISP throttling

**Solution:**
1. Establish baseline latency:
   ```bash
   # Run 100 pings to get average
   ping -c 100 app.example.com | tail -1
   ```

2. Compare with historical data
3. Check for network path changes (traceroute)
4. Monitor during different times of day
5. Consider CDN or closer hosting

### Inconsistent Results

**Symptom:** Metrics vary significantly between collections

**Causes:**
- Network path changes (load balancing)
- DNS round-robin
- Intermittent network issues
- ICMP de-prioritization

**Solution:**
```yaml
# Increase collection frequency for better sampling
probes:
  - name: webapp_ping_frequent
    type: ping_webapp
    params:
      url: "https://app.example.com"
      interval: 10  # More frequent sampling
```

### Platform-Specific Issues

**Windows: Ping Command Not Found**

**Solution:**
```powershell
# Verify ping.exe exists
where ping

# Verify PATH includes System32
echo %PATH%
```

**Linux/macOS: IPv6 Resolution Issues**

**Symptom:** DNS resolves to IPv6 but ping fails

**Solution:**
The probe uses the first resolved IP (IPv4 or IPv6). To force IPv4:
1. Configure DNS to prefer IPv4
2. Use `/etc/gai.conf` to prioritize IPv4 (Linux)
3. Use direct IPv4 address in URL

## Performance Considerations

### Collection Overhead

The WebApp Ping probe has minimal overhead:
- **DNS Resolution**: ~10-100ms per collection (cached after first lookup)
- **Ping Execution**: ~1-3 seconds (10 packets sent)
- **Total**: ~1-3 seconds per collection

### Network Impact

Each collection sends:
- **10 ICMP packets** to target IP
- **Average packet size**: 32-64 bytes (platform dependent)
- **Total bandwidth**: ~320-640 bytes per collection

### Recommended Intervals

| Use Case | Interval | Reason |
|----------|----------|--------|
| Critical services | 10-30s | Quick detection of outages |
| Standard monitoring | 30-60s | Balance accuracy and overhead |
| Background monitoring | 60-300s | Low-priority services |
| Bandwidth-sensitive | 120-300s | Minimize network traffic |

**Note:** Very frequent pinging (< 10s) may be blocked by ICMP rate limiting on some networks.

### Memory Usage

Typical memory footprint per probe instance:
- Base probe: ~200 KB
- DNS cache: ~50 KB
- Per collection: ~10 KB temporary

### Multiple URL Monitoring

When monitoring many URLs, consider resource usage:

```yaml
# Example: 10 URLs with 30s interval = 10 pings every 30s
probes:
  - name: webapp_ping_app1
    type: ping_webapp
    params: {url: "https://app1.example.com", interval: 30}

  - name: webapp_ping_app2
    type: ping_webapp
    params: {url: "https://app2.example.com", interval: 30}

  # ... up to 10-20 URLs recommended per agent
```

**Guidelines:**
- **1-10 URLs**: No performance concerns
- **10-50 URLs**: Monitor agent CPU and memory
- **50+ URLs**: Consider multiple agents or increased intervals

## Advanced Configuration

### Monitoring Behind Load Balancers

When monitoring load-balanced applications, the probe will ping the resolved IP:

```yaml
# Load balancer DNS may resolve to multiple IPs
probes:
  - name: webapp_ping_lb
    type: ping_webapp
    params:
      url: "https://lb.example.com"
      interval: 30
```

**Note:** The probe pings the first resolved IP address. For comprehensive monitoring of all backend servers, configure separate probes for each backend IP.

### CDN Monitoring

Monitor CDN edge locations:

```yaml
probes:
  - name: webapp_ping_cdn
    type: ping_webapp
    params:
      url: "https://cdn.example.com"
      interval: 60
```

**Note:** CDN DNS may return different IPs based on agent location (GeoDNS). This allows you to monitor the CDN edge closest to your agent.

### Integration with Other Probes

Combine with Load WebApp probe for comprehensive monitoring:

```yaml
probes:
  # Network layer monitoring (ICMP)
  - name: webapp_ping
    type: ping_webapp
    params:
      url: "https://app.example.com"
      interval: 30

  # Application layer monitoring (HTTP/HTTPS)
  - name: webapp_load
    type: load_webapp
    params:
      url: "https://app.example.com"
      interval: 60
      method: "GET"
```

**Benefits:**
- Network latency (ping) vs. application response time (HTTP)
- Detect network issues vs. application issues
- Comprehensive availability monitoring

## Security Considerations

### ICMP Security

- **Firewall Rules**: Many firewalls block ICMP by default
- **ICMP Flooding**: Configure reasonable intervals to avoid being flagged
- **ICMP Rate Limiting**: Some networks limit ICMP packet rates
- **Security Groups**: Cloud environments often block ICMP by default

### DNS Security

- **DNS Poisoning**: Use trusted DNS servers
- **DNS Hijacking**: Verify resolved IPs match expectations
- **DNSSEC**: Enable if available in your environment

### Agent Permissions

**Linux/Unix:**
- Raw socket access required for ICMP
- Run as root OR grant `cap_net_raw` capability
- Alternatively, configure ping_group_range sysctl

**Windows:**
- Standard user account sufficient
- No elevation required for ping.exe

## Authentication

The WebApp Ping probe requires no authentication for the target URL. It only performs network-layer ICMP ping tests after DNS resolution.

For application-layer monitoring with authentication, use the Load WebApp probe instead.

## Requirements

### Network
- DNS resolution access
- ICMP outbound traffic allowed
- Target must respond to ICMP Echo Request
- No intermediate firewalls blocking ICMP

### Operating System

**Windows**
- Windows Server 2012+ or Windows 10+
- ping.exe available (standard)
- No special permissions required

**Linux/Unix/macOS**
- ping command available (standard)
- Raw socket access (root or capabilities)
- OR ping_group_range configured

### Target Requirements

- **DNS resolvable**: Hostname must resolve to IP address
- **ICMP enabled**: Target must respond to ICMP Echo Request
- **Network accessible**: No firewall blocking between agent and target
