# Syslog Probe

The Syslog probe collects system logs and events by running a Syslog server that receives messages from network devices, servers, applications, and infrastructure components via UDP or TCP.

## Quick Start

### Basic Configuration

```yaml
# probes.d/10-syslog.yaml — each file under probes.d/ is a YAML array of probes
- name: syslog
  type: syslog
  params:
    port: 514          # Syslog port (default: 514)
    protocol: udp      # Protocol: udp or tcp (default: udp)
```

### Minimal Configuration

```yaml
# probes.d/10-syslog.yaml
- name: syslog
  type: syslog
  params: {}
```

The Syslog probe requires no mandatory parameters and works out-of-the-box with default settings (UDP port 514).

## Supported Platforms

- **Windows**: Windows Server 2012+ / Windows 10+
- **Linux**: All modern distributions (Ubuntu, RHEL, CentOS, Debian, etc.)
- **macOS**: macOS 10.13+
- **BSD**: FreeBSD, OpenBSD, NetBSD

The Syslog probe is platform-independent and listens on all network interfaces (0.0.0.0).

## Key Features Summary

### Event Collection

| Feature | Description | Details |
|---------|-------------|---------|
| **RFC Compliance** | Automatic format detection | RFC 3164 (BSD) and RFC 5424 (IETF) |
| **Transport Protocols** | UDP and TCP support | Configurable per probe instance |
| **Event Metadata** | Complete message parsing | Facility, severity, hostname, timestamp, content |
| **Real-time Processing** | Event-driven architecture | Zero-latency event forwarding |
| **Multi-source** | Centralized log aggregation | Network devices, servers, applications |

### Collected Event Fields

| Field | Description | Example |
|-------|-------------|---------|
| `facility` | Syslog facility code (0-23) | `1` (user-level) |
| `severity` | Syslog severity level (0-7) | `3` (error) |
| `hostname` | Source system hostname | `firewall-01.example.com` |
| `content` | Log message content | `Connection denied from 192.168.1.100` |
| `tag` | Application/service identifier | `sshd`, `kernel`, `nginx` |
| `client` | Client IP address | `192.168.1.50` |
| `priority` | Combined facility/severity (PRI) | `11` ((1*8)+3) |
| `timestamp` | Message timestamp | `2025-10-13T14:23:45Z` |

For complete event field reference, see [METRICS.md](./METRICS.md).

## Configuration Parameters

| Parameter | Type | Default | Valid Values | Description |
|-----------|------|---------|--------------|-------------|
| `port` | integer | `514` | `1-65535` | UDP/TCP port to listen on |
| `protocol` | string | `udp` | `udp`, `tcp` | Transport protocol |

### Example Configurations

**Standard UDP Syslog (default):**
```yaml
# probes.d/10-syslog.yaml
- name: syslog
  type: syslog
  params:
    port: 514
    protocol: udp
```

**TCP Syslog with custom port:**
```yaml
# probes.d/10-syslog.yaml
- name: syslog_tcp
  type: syslog
  params:
    port: 1514
    protocol: tcp
```

**High-security environments (TCP + custom port):**
```yaml
# probes.d/10-syslog.yaml
- name: syslog_secure
  type: syslog
  params:
    port: 6514
    protocol: tcp
```

**Multiple syslog listeners:**
```yaml
# probes.d/10-syslog.yaml
- name: syslog_standard
  type: syslog
  params:
    port: 514
    protocol: udp

- name: syslog_applications
  type: syslog
  params:
    port: 1514
    protocol: tcp
```

## Monitoring Tool Integration

### PRTG Network Monitor

Access syslog events in PRTG JSON format:

```bash
# All syslog events
curl http://localhost:8080/api/{agentkey}/prtg/metrics

# Configure PRTG HTTP Advanced Sensor:
# - URL: http://agent-host:8080/api/{agentkey}/prtg/metrics
# - Method: POST
# - Request body: {"probe": "syslog"}
```

**PRTG Channels Available:**
- Syslog Events Received (count)
- Events by Severity (Emergency, Alert, Critical, Error, Warning)
- Events by Facility (Kernel, User, System, Security)
- Event Rate (events/second)

### Nagios/Icinga

Access syslog metrics in Nagios format:

```bash
# Syslog event statistics
curl http://localhost:8080/api/{agentkey}/nagios/metrics?probe=syslog

# Example output:
# OK - Syslog events: 1234 received, 56 errors | events_total=1234c events_errors=56c rate=2.3/s
```

**Nagios Performance Data:**
- `syslog_events_total` - Total events received (counter)
- `syslog_events_by_severity` - Events by severity level
- `syslog_event_rate` - Events per second

### Grafana/Prometheus

Access syslog event data in Prometheus-compatible format:

```bash
# Prometheus format
curl http://localhost:8080/api/{agentkey}/prometheus/metrics

# Example output:
# syslog_events_total{hostname="server01",facility="1",severity="3"} 1234
# syslog_event_rate{hostname="server01"} 2.3
```

### Web Interface

View syslog events in the built-in dashboard:

```
http://localhost:8080/web/{agentkey}/dashboard
```

Features:
- Real-time syslog event stream
- Event filtering by severity, facility, hostname
- Event search and correlation
- Historical event trends

## Use Cases

### Centralized Log Aggregation

Collect logs from multiple sources into a single location:
- Network devices (routers, switches, firewalls)
- Linux/Unix servers
- Windows systems (with syslog forwarders)
- Applications and services
- Security appliances

**Configuration Example:**
```yaml
# probes.d/10-syslog.yaml
# Central syslog collector
- name: syslog_collector
  type: syslog
  params:
    port: 514
    protocol: udp
```

```yaml
# strategies.d/30-event.yaml
event:
  targets: ["senhub", "local_storage", "siem"]
```

### Security Monitoring (SIEM Integration)

Forward security-critical logs to SIEM systems:
- Authentication failures
- Firewall denials
- Intrusion detection alerts
- Security policy violations

**Filter by Security Facilities:**
- Facility 4: Security/authorization messages
- Facility 10: Security/authorization messages (private)
- Facility 13: Log audit

### Compliance and Audit Logging

Maintain compliance with regulatory requirements:
- PCI-DSS: Network and system logs
- HIPAA: Access and security logs
- SOX: Administrative activity logs
- GDPR: Data access and processing logs

**Retention Requirements:**
- PCI-DSS: 1 year minimum
- HIPAA: 6 years minimum
- SOX: 7 years minimum

### Network Device Monitoring

Collect logs from network infrastructure:
- Router and switch syslogs
- Firewall policy logs
- Load balancer events
- VPN connection logs

**Common Network Severities:**
- **0 (Emergency)**: System unusable
- **1 (Alert)**: Immediate action required
- **2 (Critical)**: Critical conditions
- **3 (Error)**: Error conditions

### Application Log Aggregation

Centralize application logs:
- Web server access/error logs (nginx, Apache)
- Database logs (MySQL, PostgreSQL)
- Application server logs (Tomcat, JBoss)
- Custom application logs

**Common Application Facilities:**
- Facility 16-23: Local use (local0-local7)

## Troubleshooting

### No Events Received

**Check network connectivity:**
```bash
# Test UDP syslog
echo "<13>Oct 13 14:23:45 test-host test-app: Test message" | nc -u -w1 localhost 514

# Test TCP syslog
echo "<13>Oct 13 14:23:45 test-host test-app: Test message" | nc -w1 localhost 514

# Verify port is listening
netstat -an | grep 514
# or
ss -tulpn | grep 514
```

**Check agent logs:**
```bash
# View syslog probe debugging
./agent run --verbose --debug-modules probe.syslog
```

**Verify probe configuration:**
```bash
# Check configuration (multi-file layout)
grep -rA5 "type: syslog" /etc/senhub/probes.d/
```

### Permission Denied (Port < 1024)

**Symptom:** Error binding to port 514 on Unix/Linux systems

**Solution:**
Ports below 1024 require elevated privileges on Unix/Linux:

```bash
# Option 1: Run agent as root (not recommended)
sudo ./agent run

# Option 2: Grant port binding capability (Linux)
sudo setcap cap_net_bind_service=+ep ./agent

# Option 3: Use alternate port (>1024) and configure syslog sources
# probes.d/10-syslog.yaml:
- name: syslog
  type: syslog
  params:
    port: 1514  # Non-privileged port
```

**Configure syslog sources to use alternate port:**
```bash
# rsyslog configuration (/etc/rsyslog.conf or /etc/rsyslog.d/*.conf)
*.* @syslog-server:1514  # UDP
*.* @@syslog-server:1514 # TCP
```

### Windows: Firewall Blocking

**Symptom:** Events not received from remote sources

**Solution:**
Add Windows Firewall rule:

```powershell
# Allow UDP Syslog
New-NetFirewallRule -DisplayName "SenHub Agent Syslog (UDP)" `
  -Direction Inbound -Protocol UDP -LocalPort 514 -Action Allow

# Allow TCP Syslog
New-NetFirewallRule -DisplayName "SenHub Agent Syslog (TCP)" `
  -Direction Inbound -Protocol TCP -LocalPort 514 -Action Allow

# Verify rules
Get-NetFirewallRule | Where-Object {$_.DisplayName -like "*Syslog*"}
```

### UDP Packet Loss

**Symptom:** Missing events during high-volume periods

**Solution:**
UDP is connectionless and can drop packets under load:

1. **Switch to TCP for reliability:**
   ```yaml
   - name: syslog
     type: syslog
     params:
       protocol: tcp  # Use TCP instead of UDP
   ```

2. **Increase UDP receive buffer:**
   ```bash
   # Linux: Increase UDP buffer size
   sudo sysctl -w net.core.rmem_max=26214400
   sudo sysctl -w net.core.rmem_default=26214400

   # Make permanent
   echo "net.core.rmem_max=26214400" | sudo tee -a /etc/sysctl.conf
   echo "net.core.rmem_default=26214400" | sudo tee -a /etc/sysctl.conf
   ```

3. **Rate limit syslog sources:**
   ```bash
   # rsyslog rate limiting (/etc/rsyslog.conf)
   $SystemLogRateLimitInterval 10
   $SystemLogRateLimitBurst 500
   ```

### Malformed Messages

**Symptom:** Events not parsed correctly or missing fields

**Solution:**
The probe uses automatic format detection (RFC 3164/5424):

1. **Verify syslog source format:**
   ```bash
   # Capture raw syslog packets
   sudo tcpdump -i any -n port 514 -A
   ```

2. **Check for proper RFC 3164 format:**
   ```
   <PRI>TIMESTAMP HOSTNAME TAG: MESSAGE
   Example: <13>Oct 13 14:23:45 server01 sshd[1234]: Connection from 192.168.1.100
   ```

3. **Check for proper RFC 5424 format:**
   ```
   <PRI>VERSION TIMESTAMP HOSTNAME APP-NAME PROCID MSGID STRUCTURED-DATA MESSAGE
   Example: <13>1 2025-10-13T14:23:45.000Z server01 sshd 1234 - - Connection from 192.168.1.100
   ```

### High Memory Usage

**Symptom:** Agent consuming excessive memory during high event rates

**Solution:**

1. **Implement event filtering at source:**
   ```bash
   # rsyslog: Filter by severity (only errors and above)
   if $syslogseverity-text == 'err' or $syslogseverity-text == 'crit' or $syslogseverity-text == 'alert' or $syslogseverity-text == 'emerg' then @@syslog-server:514
   ```

2. **Use rate limiting:**
   ```bash
   # Limit events per source
   $SystemLogRateLimitInterval 60
   $SystemLogRateLimitBurst 1000
   ```

3. **Increase storage flush interval (if using local storage)**

## Performance Considerations

### Collection Overhead

The Syslog probe is event-driven with minimal overhead:
- **UDP**: Near-zero overhead, fire-and-forget
- **TCP**: ~1-2ms per connection establishment
- **Message parsing**: ~0.1ms per message
- **Memory**: ~1 KB per message in memory

### UDP vs TCP Performance

| Feature | UDP | TCP |
|---------|-----|-----|
| **Throughput** | Very high (50,000+ msg/sec) | High (10,000-20,000 msg/sec) |
| **Reliability** | No delivery guarantee | Guaranteed delivery |
| **Latency** | Lowest (~0.1ms) | Low (~1-5ms) |
| **Connection overhead** | None | Connection establishment required |
| **Ordering** | No guarantee | Ordered delivery |
| **Use case** | High-volume, non-critical logs | Critical logs, compliance |

### Recommended Configurations

| Use Case | Protocol | Rationale |
|----------|----------|-----------|
| Network device logs | UDP | High volume, some loss acceptable |
| Security events | TCP | Reliability critical |
| Application logs | UDP | High throughput required |
| Compliance logs | TCP | No data loss permitted |
| Mixed environment | Both | Run two probe instances |

## Advanced Configuration

### Multiple Syslog Listeners

Run multiple syslog servers for different purposes:

```yaml
# probes.d/10-syslog.yaml
# Standard UDP syslog (network devices)
- name: syslog_devices
  type: syslog
  params:
    port: 514
    protocol: udp

# TCP syslog for applications
- name: syslog_apps
  type: syslog
  params:
    port: 1514
    protocol: tcp

# Secure syslog for critical systems
- name: syslog_critical
  type: syslog
  params:
    port: 6514
    protocol: tcp
```

### Integration with Other Probes

Correlate syslog events with system metrics:

```yaml
# probes.d/00-host.yaml
# Syslog events
- name: syslog
  type: syslog
  params:
    port: 514
    protocol: udp

# System metrics for correlation
- name: cpu
  type: cpu
  params:
    interval: 30

- name: memory
  type: memory
  params:
    interval: 30

- name: network
  type: network
  params:
    interval: 30
```

This enables correlation like:
- High CPU usage → Syslog errors
- Memory exhaustion → OOM killer logs
- Network saturation → Connection timeouts

### Syslog Source Configuration

#### Linux rsyslog

```bash
# /etc/rsyslog.d/senhub.conf

# Send all logs via UDP
*.* @syslog-server:514

# Send all logs via TCP
*.* @@syslog-server:514

# Send only errors via TCP
*.err @@syslog-server:514

# Send security logs only
authpriv.* @@syslog-server:514

# Apply and restart
sudo systemctl restart rsyslog
```

#### Cisco IOS/IOS-XE

```
configure terminal
logging host 192.168.1.100 transport udp port 514
logging trap informational
logging facility local6
logging source-interface Loopback0
end
write memory
```

#### pfSense/OPNsense Firewall

```
System > Settings > Logging
  Remote Logging Options:
    - Enable Remote Logging: ✓
    - Source Address: [Interface IP]
    - Remote log servers: 192.168.1.100:514
    - Remote Syslog Contents: Everything
```

#### Windows Event Log Forwarding

Windows doesn't natively support syslog. Use a forwarder:

**Option 1: NXLog Community Edition**
```
# nxlog.conf
<Input eventlog>
    Module im_msvinevents
</Input>

<Output syslog>
    Module om_udp
    Host 192.168.1.100
    Port 514
    Exec to_syslog_bsd();
</Output>

<Route 1>
    Path eventlog => syslog
</Route>
```

**Option 2: Windows Syslog Agent**
- Snare for Windows
- Kiwi Syslog Agent
- EventLog-to-Syslog

## Syslog Format Reference

### RFC 3164 (BSD Syslog)

```
<PRI>TIMESTAMP HOSTNAME TAG: MESSAGE

Example:
<13>Oct 13 14:23:45 server01 sshd[1234]: Connection from 192.168.1.100
```

### RFC 5424 (IETF Syslog)

```
<PRI>VERSION TIMESTAMP HOSTNAME APP-NAME PROCID MSGID [STRUCTURED-DATA] MESSAGE

Example:
<13>1 2025-10-13T14:23:45.000Z server01 sshd 1234 ID47 [exampleSDID@32473 eventID="1"] Connection from 192.168.1.100
```

### Priority (PRI) Calculation

```
PRI = (Facility * 8) + Severity

Examples:
- Facility 1 (user), Severity 3 (error): PRI = (1*8)+3 = 11
- Facility 4 (security), Severity 2 (critical): PRI = (4*8)+2 = 34
```

## Authentication

The Syslog probe requires no authentication for incoming syslog messages. Access control should be implemented at the network level (firewall rules, VLANs).

**Security Recommendations:**
- Restrict syslog traffic to trusted networks
- Use TCP for sensitive logs
- Consider TLS syslog for encryption (future enhancement)
- Implement network segmentation
- Monitor for unauthorized syslog sources

## Requirements

### Operating System
- **Windows**: Windows Server 2012+ or Windows 10+
- **Linux**: Any modern distribution with network stack
- **macOS**: macOS 10.13+
- **BSD**: FreeBSD, OpenBSD, NetBSD

### Network
- Inbound UDP/TCP access on configured port
- Firewall rules allowing syslog traffic
- Network connectivity to syslog sources

### Permissions
- **Privileged ports (<1024)**: Root/Administrator or CAP_NET_BIND_SERVICE capability
- **Non-privileged ports (≥1024)**: Standard user account sufficient

### Syslog Sources
- RFC 3164 (BSD Syslog) or RFC 5424 (IETF Syslog) format
- UDP or TCP transport support

## Support

For issues or questions:

1. **Enable debug logging:**
   ```bash
   ./agent run --verbose --debug-modules probe.syslog
   ```

2. **Check probe health:**
   ```bash
   curl http://localhost:8080/api/{agentkey}/debug/health
   ```

3. **Test syslog connectivity:**
   ```bash
   # Send test UDP syslog message
   logger -n localhost -P 514 "Test syslog message"

   # Send test TCP syslog message
   logger -n localhost -P 514 -T "Test syslog message"
   ```

4. **Review documentation:**
   - [Complete Event Field Reference](./METRICS.md)
   - [Troubleshooting Guide](../../troubleshooting/)
   - [Agent Configuration Guide](../../../configuration/)

5. **Report issues:**
   - Include platform (OS, version)
   - Include syslog source configuration
   - Include network topology
   - Include relevant log excerpts
   - Include sample syslog messages (tcpdump)
