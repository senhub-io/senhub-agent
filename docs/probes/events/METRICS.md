# Syslog Probe - Event Reference

## Introduction

The Syslog probe is **event-driven** and does not collect traditional numerical metrics. Instead, it receives syslog messages over the network and generates event DataPoints with structured fields extracted from the syslog protocol.

## Event DataPoint Structure

### Overview

Each received syslog message creates one DataPoint with the following structure:

| Field | Type | Description | Example |
|-------|------|-------------|---------|
| `name` | string | Always "syslog_event" | "syslog_event" |
| `timestamp` | datetime | Event timestamp (from message or receipt time) | "2025-01-13T10:30:45Z" |
| `value` | float32 | Severity level (0-7) | 6.0 |
| `tags` | Tag[] | Event metadata (see below) | [...] |

### Event Tags

Each syslog event includes these tags:

| Tag Key | Description | Example Value | Range/Format |
|---------|-------------|---------------|--------------|
| `facility` | Syslog facility code | "1" | "0"-"23" |
| `severity` | Syslog severity level | "6" | "0"-"7" |
| `host` | Source hostname from message | "server01" | hostname |
| `message` | Complete log message | "User admin logged in" | text |
| `tag` | Application/process tag | "sshd" | text |
| `client` | Source IP address | "192.168.1.100" | IP address |
| `priority` | Calculated priority | "14" | "0"-"191" |

## Severity Levels (RFC 3164)

### Level Definitions

| Code | Name | Description | Use Case |
|------|------|-------------|----------|
| 0 | Emergency | System is unusable | Complete system failure |
| 1 | Alert | Action must be taken immediately | Database corruption, security breach |
| 2 | Critical | Critical conditions | Hardware failure, application crash |
| 3 | Error | Error conditions | Failed operations, missing files |
| 4 | Warning | Warning conditions | Deprecated API, low disk space |
| 5 | Notice | Normal but significant condition | System start/stop, config change |
| 6 | Informational | Informational messages | User login, normal operations |
| 7 | Debug | Debug-level messages | Detailed diagnostics |

### Severity Filtering

**Critical Events Only** (Security/Operations):
```
severity: 0-2
```

**Operational Alerts** (Errors and above):
```
severity: 0-3
```

**All Important Events** (Warnings and above):
```
severity: 0-4
```

**All Events** (Including info/debug):
```
severity: 0-7
```

## Facility Codes (RFC 3164)

### Standard Facilities

| Code | Name | Description | Typical Sources |
|------|------|-------------|-----------------|
| 0 | kern | Kernel messages | Linux kernel |
| 1 | user | User-level messages | Applications, scripts |
| 2 | mail | Mail system | Postfix, Sendmail |
| 3 | daemon | System daemons | cron, systemd |
| 4 | auth | Security/authorization | SSH, su, sudo |
| 5 | syslog | Syslog daemon | rsyslog, syslog-ng |
| 6 | lpr | Line printer | CUPS, lpd |
| 7 | news | Network news | NNTP servers |
| 8 | uucp | UUCP subsystem | Legacy |
| 9 | cron | Cron daemon | cron, at |
| 10 | authpriv | Security/auth (private) | PAM, login |
| 11 | ftp | FTP daemon | vsftpd, proftpd |
| 12-15 | ntp, audit, alert, clock | Reserved | - |
| 16 | local0 | Local use 0 | Custom applications |
| 17 | local1 | Local use 1 | Custom applications |
| 18 | local2 | Local use 2 | Custom applications |
| 19 | local3 | Local use 3 | Custom applications |
| 20 | local4 | Local use 4 | Custom applications |
| 21 | local5 | Local use 5 | Custom applications |
| 22 | local6 | Local use 6 | Custom applications |
| 23 | local7 | Local use 7 | Custom applications |

### Facility Filtering

**Security Events**:
```
facility: 4, 10  (auth, authpriv)
```

**System Events**:
```
facility: 0, 3, 5  (kern, daemon, syslog)
```

**Application Events**:
```
facility: 16-23  (local0-local7)
```

## Priority Calculation

Priority is calculated from facility and severity:

```
priority = (facility * 8) + severity
```

**Examples**:
- user.info (facility=1, severity=6): priority = 1*8 + 6 = 14
- auth.error (facility=4, severity=3): priority = 4*8 + 3 = 35
- kern.emergency (facility=0, severity=0): priority = 0*8 + 0 = 0

## Event Processing

### Message Flow

```
Syslog Client → Network (UDP/TCP) → SenHub Agent Syslog Probe
              → Parse Message → Extract Fields → Create DataPoint
              → Forward to Storage Strategies → SIEM/Database/API
```

### Parsing

The probe automatically parses standard RFC 3164/5424 formats:

**RFC 3164 Format**:
```
<priority>timestamp hostname tag: message
<14>Jan 13 10:30:45 server01 sshd[1234]: User admin logged in
```

**RFC 5424 Format**:
```
<priority>version timestamp hostname app-name proc-id msg-id [structured-data] message
```

**Extracted Fields**:
- Priority → facility + severity calculation
- Timestamp → event timestamp
- Hostname → source system
- Tag/app-name → application identifier
- Message → log content

## Use Cases

### 1. Security Monitoring

**Event Filter**:
```
facility: 4,10 (auth, authpriv)
severity: 0-4 (emergency to warning)
```

**Alert Rules**:
- Multiple failed logins (auth.err)
- Privilege escalation (authpriv.notice)
- Account lockouts (auth.warning)
- Unauthorized access attempts

**Example Events**:
```json
{
  "name": "syslog_event",
  "value": 3,
  "tags": {
    "facility": "4",
    "severity": "3",
    "host": "server01",
    "message": "Failed password for admin from 10.0.0.5",
    "tag": "sshd",
    "client": "10.0.0.1"
  }
}
```

### 2. Application Monitoring

**Event Filter**:
```
facility: 16-23 (local0-local7)
severity: 0-3 (critical to error)
```

**Alert Rules**:
- Application crashes (local0.crit)
- Database errors (local1.err)
- API failures (local2.err)

### 3. System Health Monitoring

**Event Filter**:
```
facility: 0,3,5 (kern, daemon, syslog)
severity: 0-4
```

**Alert Rules**:
- Kernel panics (kern.emerg)
- Service failures (daemon.err)
- Out of memory (kern.crit)
- Disk errors (kern.err)

### 4. Compliance & Auditing

**Event Filter**:
```
severity: 0-7 (all events)
retention: permanent
```

**Requirements**:
- Retain all security events (facility 4,10)
- Timestamp accuracy (NTP sync)
- Source attribution (hostname + client IP)
- Tamper-proof storage

## Monitoring Best Practices

### Event Volume Metrics

While the probe doesn't generate traditional metrics, you can track:

- **Events per second** - Monitor event rate
- **Events by severity** - Count critical/error events
- **Events by facility** - Track sources
- **Events by host** - Identify chatty systems

### Alert Configuration

**Critical System Events**:
```yaml
alerts:
  - name: System Emergency
    condition: severity == 0
    action: page_oncall
    
  - name: Critical Error
    condition: severity <= 2
    action: notify_ops
```

**Security Events**:
```yaml
alerts:
  - name: Authentication Failure
    condition: facility == 4 AND severity == 3
    threshold: 5 in 5 minutes
    action: security_alert
    
  - name: Privilege Escalation
    condition: facility == 10 AND message contains "su:"
    action: security_alert
```

**Application Events**:
```yaml
alerts:
  - name: Application Error
    condition: facility >= 16 AND severity <= 3
    action: notify_dev_team
```

### Storage Strategy

**SIEM Integration**:
```yaml
storage:
  - name: event
    params:
      endpoint: "https://siem.company.com/api/events"
      auth_token: "${SIEM_TOKEN}"
      batch_size: 100
```

**Database Storage**:
```yaml
storage:
  - name: http
    params:
      endpoint: "https://logdb.company.com/ingest"
      buffer_size: 1000
```

### Performance Tuning

**High Volume (>1000 events/sec)**:
- Use TCP for flow control
- Increase buffer sizes
- Use batch forwarding
- Scale horizontally

**Low Volume (<100 events/sec)**:
- UDP is fine
- Default settings work well
- Single agent sufficient

## Troubleshooting

### No Events Received

**Check Server Status**:
```bash
# Verify probe is listening
sudo netstat -ulnp | grep 514  # UDP
sudo netstat -tlnp | grep 514  # TCP

# Check firewall
sudo ufw status | grep 514
```

**Test Event Receipt**:
```bash
# Send test message
logger -p user.info -t test "Test syslog message"

# Or use netcat
echo "<14>Jan 13 10:30:45 testhost test: Test message" | nc -u localhost 514
```

### Events Not Parsed

**Enable Debug Logging**:
```bash
./agent run --verbose --debug-modules probe.syslog
```

**Check Message Format**:
- Verify RFC 3164/5424 compliance
- Check for non-standard formats
- Review special characters

### High Memory Usage

**Symptoms**:
- Memory usage grows over time
- OOM errors

**Solutions**:
- Reduce buffer sizes
- Enable flow control (TCP)
- Filter at source
- Increase batch forwarding frequency

## Security Considerations

### Access Control

- **Firewall rules**: Only allow known sources
- **Network segmentation**: Isolate syslog network
- **Rate limiting**: Prevent DoS

### Message Integrity

- **Time sync**: Use NTP for accurate timestamps
- **Source validation**: Verify client IPs
- **Encryption**: Use VPN or TLS wrapper

### Log Retention

- **Compliance**: Retain per regulatory requirements
- **Storage**: Use write-once storage (S3, tape)
- **Access logs**: Audit who accesses logs

## Related Documentation

- [README.md](./README.md) - Syslog probe overview
- [Event Probe](./EVENT-README.md) - Custom HTTP events
- [Windows Events](./WINEVENTS-README.md) - Windows Event Log collection
