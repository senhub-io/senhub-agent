# Syslog Event Fields Complete Reference

This document provides a comprehensive reference for all event fields captured by the SenHub Agent Syslog probe.

## Table of Contents

- [Introduction](#introduction)
- [Event Structure](#event-structure)
- [Event Fields Reference](#event-fields-reference)
- [Facility Codes](#facility-codes)
- [Severity Levels](#severity-levels)
- [Priority (PRI) Calculation](#priority-pri-calculation)
- [Event Tags](#event-tags)
- [RFC Format Differences](#rfc-format-differences)
- [Event Classification](#event-classification)
- [Use Cases by Field](#use-cases-by-field)
- [Monitoring Best Practices](#monitoring-best-practices)

## Introduction

The Syslog probe captures structured event data from network devices, servers, and applications using the Syslog protocol. Each received message is parsed and stored as a DataPoint with multiple tags containing event metadata.

### Event-Driven Architecture

- **Real-time processing**: Events processed immediately upon receipt
- **Zero-latency forwarding**: Events forwarded to storage strategies without buffering
- **No periodic collection**: Unlike metric probes, syslog is event-driven
- **Automatic format detection**: Supports both RFC 3164 and RFC 5424 formats

## Event Structure

Each syslog message is converted into a DataPoint:

```json
{
  "name": "syslog_event",
  "timestamp": "2025-10-13T14:23:45.000Z",
  "value": 3.0,  // Severity level (0-7)
  "tags": [
    {"key": "facility", "value": "1"},
    {"key": "severity", "value": "3"},
    {"key": "host", "value": "firewall-01.example.com"},
    {"key": "message", "value": "Connection denied from 192.168.1.100"},
    {"key": "tag", "value": "kernel"},
    {"key": "client", "value": "192.168.1.50"},
    {"key": "priority", "value": "11"}
  ]
}
```

## Event Fields Reference

### Complete Field List

| Field Name | Tag Key | Type | Range | Description | Example |
|------------|---------|------|-------|-------------|---------|
| **Facility** | `facility` | Integer | `0-23` | Syslog facility code (message source type) | `1` (user-level) |
| **Severity** | `severity` | Integer | `0-7` | Message severity/priority level | `3` (error) |
| **Hostname** | `host` | String | - | Originating system hostname or IP | `firewall-01.example.com` |
| **Message Content** | `message` | String | - | Actual log message text | `Connection denied from 192.168.1.100` |
| **Tag** | `tag` | String | - | Application/process name | `sshd`, `kernel`, `nginx` |
| **Client IP** | `client` | String | - | IP address of syslog message sender | `192.168.1.50` |
| **Priority (PRI)** | `priority` | Integer | `0-191` | Combined facility/severity value | `11` ((1*8)+3) |
| **Timestamp** | (DataPoint) | Timestamp | - | Message creation time | `2025-10-13T14:23:45Z` |

### Field Descriptions

#### Facility

**Description:** Identifies the type of system or application that generated the message.

**Storage:** Tag key `facility` (integer string)

**Range:** 0-23 (see [Facility Codes](#facility-codes) for complete list)

**Use Cases:**
- Filter events by source type (kernel, security, applications)
- Route events to different destinations based on facility
- Security monitoring (facilities 4, 10, 13)
- Compliance filtering (authentication, audit logs)

**Example Queries:**
```
# Security-related events (facilities 4 and 10)
facility=4 OR facility=10

# Kernel messages only
facility=0

# Local application logs (facilities 16-23)
facility>=16 AND facility<=23
```

#### Severity

**Description:** Indicates the urgency/importance of the message.

**Storage:**
- Tag key `severity` (integer string)
- DataPoint `value` field (float32)

**Range:** 0-7 (see [Severity Levels](#severity-levels) for complete list)

**Use Cases:**
- Alert on critical/error messages
- Filter noise (informational/debug messages)
- Prioritize incident response
- SLA compliance (response time by severity)

**Example Queries:**
```
# Critical and above (emergency, alert, critical)
severity<=2

# Errors only
severity=3

# Warnings and errors
severity<=4

# Informational messages
severity=6
```

**Alert Thresholds:**
```yaml
# Emergency/Alert/Critical
severity <= 2: Immediate response

# Error
severity = 3: Investigate within 1 hour

# Warning
severity = 4: Review within 24 hours

# Notice/Info/Debug
severity >= 5: Informational only
```

#### Hostname

**Description:** The hostname or IP address of the system that generated the message.

**Storage:** Tag key `host` (string)

**Format:**
- Fully Qualified Domain Name (FQDN): `server01.example.com`
- Short hostname: `server01`
- IP address: `192.168.1.100`

**Use Cases:**
- Identify problem sources
- Filter events by specific hosts
- Aggregate events per host
- Multi-tenant log separation

**Example Queries:**
```
# Specific host
host="firewall-01.example.com"

# All web servers (wildcard)
host LIKE "web-%.example.com"

# Events from subnet (requires IP in host field)
host LIKE "192.168.1.%"
```

**Note:** Hostname accuracy depends on syslog source configuration. Some devices may send IP addresses instead of hostnames.

#### Message Content

**Description:** The actual log message text.

**Storage:** Tag key `message` (string)

**Format:** Free-form text, content varies by source

**Use Cases:**
- Full-text search
- Pattern matching (regex)
- Anomaly detection
- Incident investigation

**Example Queries:**
```
# Authentication failures
message CONTAINS "authentication failure"
message CONTAINS "failed password"

# Connection denials
message CONTAINS "denied"
message CONTAINS "rejected"

# Error patterns
message REGEX "error|fail|exception"

# Security events
message CONTAINS "unauthorized access"
message CONTAINS "permission denied"
```

**Common Message Patterns:**

| Pattern | Example | Meaning |
|---------|---------|---------|
| Authentication | `Failed password for root from 192.168.1.100` | Login failure |
| Connection | `Connection from 192.168.1.50 port 54321` | New connection |
| Error | `disk write error: I/O error` | Hardware/software error |
| Security | `Unauthorized access attempt detected` | Security violation |
| Service | `Service httpd started` | Service lifecycle |

#### Tag

**Description:** Application or process name that generated the message.

**Storage:** Tag key `tag` (string)

**Common Values:**
- `kernel`: Kernel messages
- `sshd`: SSH daemon
- `sudo`: Privilege elevation
- `cron`: Scheduled tasks
- `systemd`: System/service manager
- `nginx`, `apache`: Web servers
- `mysql`, `postgres`: Databases

**Use Cases:**
- Filter by application
- Application-specific monitoring
- Service health tracking
- Performance analysis by service

**Example Queries:**
```
# SSH authentication monitoring
tag="sshd"

# Web server logs
tag="nginx" OR tag="apache"

# System services
tag="systemd"

# Database logs
tag LIKE "%sql%"
```

#### Client IP

**Description:** IP address of the system that sent the syslog message to the probe.

**Storage:** Tag key `client` (string)

**Format:** IPv4 or IPv6 address

**Use Cases:**
- Network topology mapping
- Multi-source aggregation
- Spoofing detection (client ≠ hostname)
- Access control validation

**Example Queries:**
```
# Events from specific client
client="192.168.1.50"

# Events from subnet
client LIKE "10.0.0.%"

# Detect potential spoofing (hostname doesn't match client)
host!="192.168.1.50" AND client="192.168.1.50"
```

**Security Note:** Client IP can differ from hostname field if:
- NAT/proxy in the path
- Spoofed hostname in syslog message
- Syslog relay/forwarder used

#### Priority (PRI)

**Description:** Combined facility and severity value (standard syslog PRI field).

**Storage:** Tag key `priority` (integer string)

**Calculation:** `PRI = (Facility * 8) + Severity`

**Range:** 0-191

**Use Cases:**
- Standard syslog filtering
- Legacy syslog tool integration
- Message routing by PRI
- Validation of facility/severity extraction

**Example Calculations:**
```
Facility 0 (kernel), Severity 0 (emergency):  PRI = (0*8)+0 = 0
Facility 1 (user),   Severity 3 (error):     PRI = (1*8)+3 = 11
Facility 4 (auth),   Severity 6 (info):      PRI = (4*8)+6 = 38
Facility 16 (local0), Severity 2 (critical): PRI = (16*8)+2 = 130
```

**Extract Facility/Severity from PRI:**
```
Facility = PRI / 8 (integer division)
Severity = PRI % 8 (modulo)

Example: PRI = 38
- Facility = 38 / 8 = 4 (auth)
- Severity = 38 % 8 = 6 (info)
```

#### Timestamp

**Description:** Date and time when the message was generated.

**Storage:** DataPoint `timestamp` field (RFC 3339 format)

**Sources:**
1. **From message**: Parsed from syslog message timestamp
2. **Fallback**: Agent's current time if message timestamp is missing/invalid

**Formats Supported:**
- RFC 3164: `Oct 13 14:23:45` (no year, assumes current year)
- RFC 5424: `2025-10-13T14:23:45.000Z` (full ISO 8601)

**Use Cases:**
- Event timeline reconstruction
- Time-based filtering
- Latency analysis (receive time - message time)
- Compliance retention

**Timestamp Accuracy Considerations:**
- **No NTP**: Source system clock drift can cause incorrect timestamps
- **Time zones**: RFC 3164 has no timezone; RFC 5424 uses UTC
- **Year rollover**: RFC 3164 assumes current year (problems on Jan 1st)

## Facility Codes

Standard syslog facility codes (RFC 3164/5424):

| Code | Facility | Description | Common Sources |
|------|----------|-------------|----------------|
| `0` | kern | Kernel messages | Linux kernel, BSD kernel |
| `1` | user | User-level messages | Generic user processes |
| `2` | mail | Mail system | Postfix, Sendmail, Exchange |
| `3` | daemon | System daemons | Background services |
| `4` | auth | Security/authorization | SSH, login, sudo |
| `5` | syslog | Syslog internal messages | rsyslog, syslog-ng |
| `6` | lpr | Line printer subsystem | Printing services |
| `7` | news | Network news subsystem | NNTP servers |
| `8` | uucp | UUCP subsystem | Legacy Unix-to-Unix copy |
| `9` | cron | Clock daemon | cron, at |
| `10` | authpriv | Security/authorization (private) | Sensitive auth logs |
| `11` | ftp | FTP daemon | FTP servers |
| `12` | ntp | NTP subsystem | ntpd, chronyd |
| `13` | audit | Log audit | auditd, Linux audit |
| `14` | alert | Log alert | Alert/monitoring systems |
| `15` | clock | Clock daemon (2) | Legacy time services |
| `16` | local0 | Local use 0 | Custom applications |
| `17` | local1 | Local use 1 | Custom applications |
| `18` | local2 | Local use 2 | Custom applications |
| `19` | local3 | Local use 3 | Custom applications |
| `20` | local4 | Local use 4 | Custom applications |
| `21` | local5 | Local use 5 | Custom applications |
| `22` | local6 | Local use 6 | Custom applications |
| `23` | local7 | Local use 7 | Custom applications |

### Facility Usage Guidelines

**Security Monitoring (Facilities 4, 10, 13):**
```
# Authentication events
facility=4 OR facility=10

# Audit logs
facility=13
```

**System Monitoring (Facilities 0, 3, 5):**
```
# Kernel and system issues
facility=0 OR facility=3 OR facility=5
```

**Application Logging (Facilities 16-23):**
```
# Custom application logs
facility>=16 AND facility<=23
```

## Severity Levels

Standard syslog severity levels (RFC 3164/5424):

| Code | Severity | Description | Action Required | Examples |
|------|----------|-------------|-----------------|----------|
| `0` | Emergency | System is unusable | **IMMEDIATE** | System panic, total failure |
| `1` | Alert | Action must be taken immediately | **IMMEDIATE** | Critical resource exhaustion |
| `2` | Critical | Critical conditions | **URGENT** | Hardware failure, data corruption |
| `3` | Error | Error conditions | **HIGH** | Service errors, failed operations |
| `4` | Warning | Warning conditions | **MEDIUM** | Resource limits, deprecated features |
| `5` | Notice | Normal but significant condition | **LOW** | Configuration changes, restarts |
| `6` | Informational | Informational messages | **NONE** | Normal operations, connections |
| `7` | Debug | Debug-level messages | **NONE** | Detailed debugging information |

### Severity Level Guidelines

**Production Alert Thresholds:**

```yaml
# Immediate Response (24/7)
Severity 0 (Emergency): Page on-call immediately
Severity 1 (Alert):     Page on-call immediately
Severity 2 (Critical):  Page on-call immediately

# Business Hours Response
Severity 3 (Error):     Create high-priority ticket
Severity 4 (Warning):   Create medium-priority ticket

# Informational Only
Severity 5 (Notice):    Log only
Severity 6 (Info):      Log only (can be filtered)
Severity 7 (Debug):     Log only (filter in production)
```

**Filtering Recommendations:**

```bash
# Production: Errors and above only
severity<=3

# Development: Include warnings
severity<=4

# Debugging: All messages
severity<=7

# Security monitoring: All severities
# (attackers may use low-severity to hide activity)
```

### Severity Distribution Analysis

**Normal Operations (Baseline):**
```
Debug (7):        5-10%  (development only)
Informational (6): 70-80%
Notice (5):        10-15%
Warning (4):       5-10%
Error (3):         1-3%
Critical (2):      <0.5%
Alert (1):         <0.1%
Emergency (0):     <0.01%
```

**Investigate if:**
- Error rate > 5%
- Critical/Alert/Emergency > 1%
- Sudden spike in any severity
- Warning rate increasing over time

## Priority (PRI) Calculation

### Formula

```
PRI = (Facility * 8) + Severity
```

### Example Calculations

| Facility | Severity | PRI | Description |
|----------|----------|-----|-------------|
| 0 (kern) | 0 (emerg) | 0 | Kernel emergency |
| 0 (kern) | 3 (error) | 3 | Kernel error |
| 1 (user) | 6 (info) | 14 | User informational |
| 4 (auth) | 3 (error) | 35 | Authentication error |
| 4 (auth) | 6 (info) | 38 | Authentication info |
| 16 (local0) | 3 (error) | 131 | Local0 error |
| 23 (local7) | 7 (debug) | 191 | Local7 debug (maximum PRI) |

### PRI Ranges by Facility

| Facility | PRI Range | Severity 0-7 |
|----------|-----------|--------------|
| kern (0) | 0-7 | Emergency through Debug |
| user (1) | 8-15 | Emergency through Debug |
| auth (4) | 32-39 | Emergency through Debug |
| local0 (16) | 128-135 | Emergency through Debug |
| local7 (23) | 184-191 | Emergency through Debug |

### Reverse Calculation

**Extract facility and severity from PRI:**

```python
# Python example
def parse_pri(pri):
    facility = pri // 8
    severity = pri % 8
    return facility, severity

# Example: PRI = 38
facility, severity = parse_pri(38)
# facility = 4 (auth)
# severity = 6 (info)
```

```bash
# Bash example
PRI=38
FACILITY=$((PRI / 8))
SEVERITY=$((PRI % 8))
echo "Facility: $FACILITY, Severity: $SEVERITY"
# Output: Facility: 4, Severity: 6
```

## Event Tags

All syslog events include standard tags for filtering and aggregation.

### Tag List

| Tag Key | Type | Description | Example Values |
|---------|------|-------------|----------------|
| `facility` | integer | Facility code | `0`, `1`, `4`, `16` |
| `severity` | integer | Severity level | `0`, `3`, `6`, `7` |
| `host` | string | Source hostname | `server01.example.com` |
| `message` | string | Log message content | `Connection denied from...` |
| `tag` | string | Application name | `sshd`, `kernel`, `nginx` |
| `client` | string | Sender IP address | `192.168.1.50` |
| `priority` | integer | PRI value | `11`, `38`, `131` |
| `probe_name` | string | Probe identifier | `syslog` |

### Tag Privacy

All tags are marked as **non-private** (`Private: false`), meaning they are included in exports and API responses.

**Security Consideration:** If messages contain sensitive information (passwords, tokens, etc.), implement filtering at the source before sending to syslog.

### Tag Usage Examples

**Filter by security facility:**
```
facility=4 OR facility=10
```

**Critical authentication errors:**
```
facility=4 AND severity<=2
```

**SSH events from specific host:**
```
tag="sshd" AND host="server01.example.com"
```

**All errors from web servers:**
```
severity=3 AND (tag="nginx" OR tag="apache")
```

**Events from specific subnet:**
```
client LIKE "192.168.1.%"
```

## RFC Format Differences

### RFC 3164 (BSD Syslog)

**Format:**
```
<PRI>TIMESTAMP HOSTNAME TAG: MESSAGE

Example:
<13>Oct 13 14:23:45 server01 sshd[1234]: Connection from 192.168.1.100
```

**Characteristics:**
- No version field
- Short timestamp (no year, no timezone)
- Simple structure
- Most common format

**Limitations:**
- No timezone information
- Assumes current year
- No structured data
- Limited message length (1024 bytes)

**Parsing Challenges:**
- Year rollover issues (Dec 31 → Jan 1)
- Timezone ambiguity
- Varying TAG formats (with/without PID)

### RFC 5424 (IETF Syslog)

**Format:**
```
<PRI>VERSION TIMESTAMP HOSTNAME APP-NAME PROCID MSGID [STRUCTURED-DATA] MESSAGE

Example:
<13>1 2025-10-13T14:23:45.000Z server01 sshd 1234 ID47 [exampleSDID@32473 eventID="1" eventSource="Application"] Connection from 192.168.1.100
```

**Characteristics:**
- Version field (always `1`)
- Full ISO 8601 timestamp with timezone
- Structured data support
- Larger message size (up to 2048+ bytes)

**Advantages:**
- Unambiguous timestamps (UTC)
- Structured metadata
- Better internationalization
- Explicit field delimiters

**Adoption:**
- Less common than RFC 3164
- Modern devices/applications
- Enterprise logging systems

### Automatic Format Detection

The SenHub Agent Syslog probe automatically detects and parses both RFC 3164 and RFC 5424 formats:

```go
server.SetFormat(syslog.Automatic)
```

**Detection Logic:**
1. Check for version field (`<PRI>1 ...`)
2. If version present → RFC 5424
3. Otherwise → RFC 3164

## Event Classification

### By Security Relevance

**Critical Security Events:**
```
# Authentication failures
facility=4 OR facility=10
severity<=4
message CONTAINS "fail" OR message CONTAINS "denied" OR message CONTAINS "invalid"

# Unauthorized access
message CONTAINS "unauthorized" OR message CONTAINS "permission denied"
severity<=4

# Security alerts
facility=14
severity<=3
```

**Compliance-Relevant Events:**
```
# Authentication and authorization (PCI-DSS, HIPAA, SOX)
facility=4 OR facility=10

# Audit logs (PCI-DSS 10.x)
facility=13

# Access logs (GDPR Article 30)
tag CONTAINS "access"
facility=4
```

### By Infrastructure Layer

**Network Layer:**
```
# Firewalls, routers, switches
facility=16  # Often local0 for network devices
tag CONTAINS "firewall" OR tag CONTAINS "router" OR tag CONTAINS "switch"
```

**System Layer:**
```
# Operating system
facility=0 OR facility=3 OR facility=5
```

**Application Layer:**
```
# Applications and services
facility>=16 AND facility<=23
```

### By Incident Type

**Authentication Incidents:**
```
facility=4 OR facility=10
message CONTAINS "fail" OR message CONTAINS "invalid" OR message CONTAINS "denied"
```

**Availability Incidents:**
```
severity<=2
message CONTAINS "down" OR message CONTAINS "unreachable" OR message CONTAINS "timeout"
```

**Performance Incidents:**
```
message CONTAINS "slow" OR message CONTAINS "timeout" OR message CONTAINS "overload"
severity=4
```

## Use Cases by Field

### Security Monitoring (SIEM)

**Primary Fields:**
- `facility`: Filter auth events (4, 10, 13)
- `severity`: Alert on errors/critical (≤3)
- `message`: Pattern matching for threats
- `host`: Identify compromised systems
- `client`: Network forensics

**Example Query:**
```
facility IN (4,10) AND severity<=3 AND
(message CONTAINS "failed" OR message CONTAINS "invalid" OR message CONTAINS "denied")
```

**Alert Rules:**
```yaml
# Brute force detection
- alert: SSHBruteForce
  query: tag="sshd" AND message CONTAINS "Failed password"
  threshold: 5 attempts in 5 minutes
  action: Block source IP, notify SOC

# Privilege escalation
- alert: UnauthorizedSudo
  query: tag="sudo" AND message CONTAINS "NOT permitted"
  threshold: 1 event
  action: Immediate investigation

# Account compromise
- alert: UnusualAuthLocation
  query: facility=4 AND message CONTAINS "accepted"
  condition: source_ip NOT IN known_locations
  action: Verify with user, lock account
```

### Compliance Auditing

**PCI-DSS Requirement 10.2:**
```
# All user access to cardholder data
facility=4 OR facility=10

# All actions by root/admin
tag="sudo" OR message CONTAINS "root" OR message CONTAINS "administrator"

# All invalid logical access attempts
severity<=4 AND (message CONTAINS "fail" OR message CONTAINS "denied")

# Audit trail changes
facility=13 OR message CONTAINS "audit"
```

**HIPAA Security Rule:**
```
# Access to ePHI systems
facility=4
message CONTAINS "login" OR message CONTAINS "access"

# Audit log access
facility=13

# Security incidents
severity<=3
```

**GDPR Article 30 (Records of Processing):**
```
# Data access logs
message CONTAINS "database" OR message CONTAINS "query" OR message CONTAINS "select"
facility=4

# User activity logs
facility=4 OR facility=10
```

### Operational Monitoring

**Service Health:**
```
# Service start/stop
message CONTAINS "start" OR message CONTAINS "stop" OR message CONTAINS "restart"

# Service errors
severity=3
tag IN ("nginx", "apache", "mysql", "postgres")

# Resource issues
message CONTAINS "memory" OR message CONTAINS "disk" OR message CONTAINS "cpu"
severity<=4
```

**Performance Monitoring:**
```
# Slow operations
message CONTAINS "slow" OR message CONTAINS "timeout"

# Queue/backlog
message CONTAINS "queue" OR message CONTAINS "backlog" OR message CONTAINS "pending"

# Connection limits
message CONTAINS "connection limit" OR message CONTAINS "too many"
```

### Capacity Planning

**Usage Trends:**
```
# Connection counts
message CONTAINS "connection" AND severity=6

# Resource utilization
message CONTAINS "utilization" OR message CONTAINS "usage"

# Peak load indicators
severity=4 AND (message CONTAINS "high" OR message CONTAINS "peak")
```

## Monitoring Best Practices

### Alert Configuration

**Severity-Based Alerts:**

```yaml
# Critical alerts (immediate response)
- alert: CriticalSyslogEvent
  expr: syslog_event{severity<=2}
  for: 0s  # Immediate
  labels:
    priority: P1
  annotations:
    description: Critical syslog event from {{ $labels.host }}

# Error alerts (high priority)
- alert: ErrorSyslogEvent
  expr: syslog_event{severity=3}
  for: 5m  # Sustained errors
  labels:
    priority: P2
  annotations:
    description: Error events from {{ $labels.host }}
```

**Pattern-Based Alerts:**

```yaml
# Authentication failures
- alert: AuthenticationFailures
  expr: count_over_time(syslog_event{facility=4,message~=".*fail.*"}[5m]) > 5
  labels:
    priority: P2
  annotations:
    description: Multiple authentication failures on {{ $labels.host }}

# Disk space warnings
- alert: DiskSpaceWarning
  expr: syslog_event{message~=".*disk.*space.*"}
  labels:
    priority: P3
  annotations:
    description: Disk space warning on {{ $labels.host }}
```

### Dashboard Design

**Essential Panels:**

1. **Event Volume Over Time**
   - Time series of event count
   - Group by severity
   - Identify anomalies

2. **Top Event Sources**
   - Bar chart of event count by host
   - Identify chatty systems

3. **Severity Distribution**
   - Pie chart of events by severity
   - Monitor for increased errors

4. **Facility Breakdown**
   - Stacked area chart by facility
   - Understand message composition

5. **Critical Events Table**
   - Recent events with severity ≤2
   - Quick incident response

6. **Authentication Events**
   - Time series of facility=4,10 events
   - Security monitoring

### Retention Policies

**Compliance-Driven Retention:**

| Regulation | Minimum Retention | Recommended |
|------------|-------------------|-------------|
| PCI-DSS | 1 year (3 months online) | 2 years |
| HIPAA | 6 years | 7 years |
| SOX | 7 years | 7 years |
| GDPR | Varies by purpose | 1-2 years |

**Storage Optimization:**

```yaml
# Hot storage (fast access)
- severity: 0-3  # Critical/Error
  retention: 90 days
  storage: SSD

# Warm storage (regular access)
- severity: 4-5  # Warning/Notice
  retention: 30 days
  storage: SSD

# Cold storage (archive)
- severity: 6-7  # Info/Debug
  retention: 7 days
  storage: HDD
  compression: enabled

# Compliance archive
- facility: [4,10,13]  # Auth/Audit
  retention: 7 years
  storage: archive (S3, tape)
  encryption: required
```

### Event Filtering

**Production Filtering (Reduce Noise):**

```yaml
# Drop debug messages in production
- severity: 7
  action: drop

# Drop informational from non-critical sources
- severity: 6
  host: NOT IN critical_hosts
  action: drop

# Rate limit chatty applications
- tag: "noisy-app"
  rate_limit: 10 events/minute
  action: sample
```

**Security Filtering (Increase Sensitivity):**

```yaml
# Capture ALL security events
- facility: [4,10,13]
  action: always_capture

# Capture ALL critical events
- severity: [0,1,2]
  action: always_capture

# Capture authentication patterns
- message: REGEX "(fail|denied|invalid|unauthorized)"
  action: always_capture
```

### Performance Tuning

**High-Volume Environments:**

Per-probe tuning (dedicated receiver, TCP for reliability):

```yaml
protocol: tcp        # Use TCP for reliability
port: 514            # Dedicated syslog receiver
bind_address: "10.0.0.100"
```

Multiple probe instances (load balancing):

```yaml
# probes.d/10-syslog.yaml — each file under probes.d/ is a YAML array of probes
- name: syslog_1
  type: syslog
  params: {port: 514, protocol: tcp}
- name: syslog_2
  type: syslog
  params: {port: 1514, protocol: tcp}
```

**Network Optimization:**

```bash
# Increase UDP receive buffer (Linux)
sudo sysctl -w net.core.rmem_max=26214400
sudo sysctl -w net.core.rmem_default=26214400

# Increase socket buffer
sudo sysctl -w net.core.netdev_max_backlog=5000

# TCP tuning for high connection rate
sudo sysctl -w net.ipv4.tcp_max_syn_backlog=4096
sudo sysctl -w net.core.somaxconn=4096
```

## Related Documentation

- [Syslog Probe README](./README.md) - Configuration and quick start
- [Event Monitoring Guide](../../guides/event-monitoring.md) - Event collection strategies
- [Security Monitoring](../../guides/security-monitoring.md) - SIEM integration
- [Troubleshooting Guide](../../troubleshooting/) - Common issues and solutions
