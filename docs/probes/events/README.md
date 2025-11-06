# Syslog Probe - README

## Overview

The Syslog probe collects log messages by acting as a Syslog server (RFC 3164/5424). It receives syslog messages from network devices, servers, and applications over UDP or TCP, forwarding them to configured data storage strategies.

## Quick Start

### Basic Configuration
```yaml
probes:
  - name: syslog
    params:
      port: 514
      protocol: udp
```

### TCP Configuration
```yaml
probes:
  - name: syslog
    params:
      port: 514
      protocol: tcp
```

## Key Features

- **Event-driven**: Real-time log collection
- **RFC compliant**: Supports RFC 3164 and RFC 5424
- **Dual protocol**: UDP (fast) or TCP (reliable)
- **Auto-parsing**: Automatic facility, severity, and message extraction
- **Network-wide**: Collect logs from multiple sources

## Configuration Parameters

| Parameter | Type | Default | Description |
|-----------|------|---------|-------------|
| `port` | integer | 514 | Listening port (1-65535) |
| `protocol` | string | udp | Protocol: "udp" or "tcp" |

## Event Structure

Each syslog message generates a DataPoint with:

| Field | Description | Example |
|-------|-------------|---------|
| `facility` | Syslog facility (0-23) | 1 (user) |
| `severity` | Syslog severity (0-7) | 6 (info) |
| `hostname` | Source hostname | "server01" |
| `message` | Log message content | "User logged in" |
| `tag` | Application tag | "sshd" |
| `client` | Source IP address | "192.168.1.100" |
| `priority` | Calculated priority | 14 |
| `timestamp` | Event timestamp | ISO 8601 |

## Syslog Severity Levels

| Level | Name | Description |
|-------|------|-------------|
| 0 | Emergency | System unusable |
| 1 | Alert | Action must be taken immediately |
| 2 | Critical | Critical conditions |
| 3 | Error | Error conditions |
| 4 | Warning | Warning conditions |
| 5 | Notice | Normal but significant |
| 6 | Informational | Informational messages |
| 7 | Debug | Debug-level messages |

## Syslog Facilities

| Code | Facility | Description |
|------|----------|-------------|
| 0 | kern | Kernel messages |
| 1 | user | User-level messages |
| 2 | mail | Mail system |
| 3 | daemon | System daemons |
| 4 | auth | Security/authorization |
| 5 | syslog | Syslog internal |
| 6 | lpr | Line printer subsystem |
| 16 | local0 | Local use 0 |
| 17-23 | local1-7 | Local use 1-7 |

## Monitoring Integration

### Event Strategy
```yaml
storage:
  - name: event
    params:
      endpoint: "https://siem.company.com/events"
```

### HTTP Strategy
```yaml
storage:
  - name: http
    params:
      port: 8080
```

Query events via API:
```bash
curl http://localhost:8080/api/{key}/events?probe=syslog&severity=0-3
```

## Use Cases

### 1. Log Aggregation
Centralize logs from multiple servers and devices.

**Configuration**:
- Configure devices to send syslog to agent IP:514
- Agent forwards to SIEM or log management platform

### 2. Security Monitoring
Collect security-related logs for threat detection.

**Filters**:
- Facility 4 (auth) - Authentication events
- Severity 0-3 - Critical security alerts
- Failed login attempts, privilege escalations

### 3. Compliance & Auditing
Maintain audit logs for compliance requirements.

**Requirements**:
- Retain all logs (severity 0-7)
- Timestamp accuracy
- Source attribution
- Tamper-proof storage

### 4. Application Monitoring
Monitor application logs via syslog.

**Configuration**:
```bash
# Application sends logs via syslog
logger -p local0.info "Application started"
```

## Platform Configuration

### Linux Syslog Clients

**rsyslog configuration** (`/etc/rsyslog.conf`):
```bash
# Send all logs to SenHub Agent
*.* @192.168.1.100:514     # UDP
*.* @@192.168.1.100:514    # TCP
```

**syslog-ng configuration**:
```bash
destination senhub {
    syslog("192.168.1.100" port(514) transport("udp"));
};
log { source(src); destination(senhub); };
```

### Network Devices

**Cisco IOS**:
```
logging host 192.168.1.100
logging trap informational
```

**Juniper JunOS**:
```
set system syslog host 192.168.1.100 any info
```

### Windows Event Forwarding

Use tools like NXLog to forward Windows Events as Syslog:
```xml
<Output syslog>
    Module om_udp
    Host 192.168.1.100
    Port 514
</Output>
```

## Troubleshooting

### No Events Received

**Check firewall**:
```bash
# Linux
sudo ufw allow 514/udp
sudo firewall-cmd --add-port=514/udp --permanent

# Check if port is listening
sudo netstat -ulnp | grep 514  # UDP
sudo netstat -tlnp | grep 514  # TCP
```

### Permission Denied (Port <1024)

**Symptom**: "Permission denied" on ports below 1024

**Solutions**:
```bash
# Option 1: Use unprivileged port
port: 5514  # >1024, no root required

# Option 2: Grant capabilities (Linux)
sudo setcap 'cap_net_bind_service=+ep' /path/to/agent

# Option 3: Run as root (not recommended)
sudo ./agent run
```

### Messages Not Parsed

**Symptom**: Raw messages without proper field extraction

**Cause**: Non-standard syslog format

**Solution**:
- Verify client sends RFC-compliant syslog
- Check for custom formats
- Enable debug logging: `--verbose --debug-modules probe.syslog`

### High Event Volume

**Symptom**: Agent performance degradation with many events

**Solutions**:
- Filter at source (don't send debug logs)
- Use TCP for flow control
- Increase buffer sizes
- Scale horizontally (multiple agents)

## Security Considerations

### Network Security

- **Firewall**: Only allow trusted sources
- **Encryption**: Use VPN or TLS for sensitive logs
- **Authentication**: No built-in auth in syslog (use network segmentation)

### Log Integrity

- **Timestamps**: Sync time with NTP
- **Source validation**: Verify `client` IP matches expected sources
- **Tamper detection**: Forward to write-once storage (SIEM, S3)

## Performance

- **CPU**: Varies with event rate (~1% per 1000 events/sec)
- **Memory**: ~10MB base + buffers
- **Network**: Depends on log volume
- **UDP**: Lower overhead, possible loss
- **TCP**: Higher overhead, guaranteed delivery

## Alert Examples

### Critical Events
```yaml
alerts:
  - name: Critical Syslog Event
    condition: severity <= 2
    action: notify_oncall
```

### Authentication Failures
```yaml
alerts:
  - name: Failed Login
    condition: facility == 4 AND message contains "failed"
    threshold: 5 events in 5 minutes
    action: security_alert
```

### Service Failures
```yaml
alerts:
  - name: Service Down
    condition: message contains "stopped" OR message contains "failed"
    severity: <= 3
    action: notify_ops
```

## Related Documentation

- [METRICS.md](./METRICS.md) - Event structure reference
- [Event Probe](./EVENT-README.md) - Custom HTTP events
- [Windows Events](./WINEVENTS-README.md) - Windows Event Log
