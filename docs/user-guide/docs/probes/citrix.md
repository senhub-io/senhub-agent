<img src="https://cdn.simpleicons.org/citrix" alt="" class="probe-page-logo probe-page-logo-si">

!!! warning
    **License: Pro** - Requires a Pro or Enterprise license.

# Overview

The Citrix probe monitors Citrix Virtual Apps and Desktops (CVAD) environments through the Director OData API and DDC REST API, providing comprehensive metrics for sessions, infrastructure, logon performance, and connection failures.

**Collected Data:**
- Session counts and states (active, disconnected, zombie sessions)
- Logon performance with detailed phase breakdown
- Infrastructure health (VDA machines, delivery controllers)
- Connection failure analysis by category
- Multi-site support with site filtering

**API Compatibility:**
- Citrix Virtual Apps and Desktops 7.x (CVAD)
- Citrix Director OData API
- Delivery Controller REST API (optional, for multi-site)

# Prerequisites

## Required Components

1. **Citrix Director** - OData API access required
2. **Network Connectivity** - Agent server must reach Director (port 443)
3. **Service Account** - Domain account with monitoring permissions

## Service Account Requirements

The monitoring account must have:

**Citrix Permissions:**
- **"Read Only Administrator"** role in Citrix Studio
- Access to Director web interface
- API permissions for OData and DDC REST API

**Domain Requirements:**
- Active Directory domain account
- No special OU or group requirements
- Standard user permissions sufficient (no admin rights needed)

**Creating the service account:**
```powershell
# In Active Directory Users and Computers
New-ADUser -Name "svc-monitoring" `
  -UserPrincipalName "svc-monitoring@domain.com" `
  -AccountPassword (ConvertTo-SecureString "SecureP@ssw0rd" -AsPlainText -Force) `
  -Enabled $true `
  -PasswordNeverExpires $true
```

**Assigning Citrix permissions:**
1. Open Citrix Studio
2. Navigate to **Configuration > Administrators**
3. Click **Add Administrator**
4. Select `DOMAIN\svc-monitoring`
5. Assign **"Read Only Administrator"** role
6. Click **OK**

# Quick Start

## Basic Configuration (Director Only)

Minimal configuration for single-site Citrix environment:

```yaml
# probes.d/10-citrix.yaml — each file under probes.d/ is a YAML array of probes
- name: "production-citrix"
  type: citrix
  params:
    base_url: "https://director.company.com"
    interval: 120  # 2 minutes recommended
    auth:
      username: "DOMAIN\\svc-monitoring"  # Note: double backslash
      password: ${secret:production-citrix.password}   # OS secret store; inline plaintext is auto-sealed on install
    tls:
      verify_ssl: true
    timeout: 30
```

**Important notes:**
- `base_url`: Director URL **without** `/Director` path suffix
- `username`: Must use double backslash `DOMAIN\\username` format in YAML
- `interval`: 120 seconds (2 minutes) balances data freshness with API load

## Configuration with Site Filtering (Multi-Site)

For multi-site deployments requiring site-specific metrics:

```yaml
# probes.d/10-citrix.yaml
- name: "citrix-paris-site"
  type: citrix
  params:
    base_url: "https://director-paris.company.com"

    delivery_controller:
      url: "https://citrix-ddc-paris.company.com"
      fallback_urls:
        - "https://citrix-ddc-paris-backup.company.com"
      site_filter: "SITE-PARIS"  # Filter to specific site

    interval: 120
    auth:
      username: "DOMAIN\\svc-monitoring"
      password: ${secret:citrix-paris-site.password}   # OS secret store; inline plaintext is auto-sealed on install
    retry:
      max_attempts: 3
      backoff_factor: 2.0
```

**Site filtering benefits:**
- Isolate metrics by datacenter or region
- Monitor multiple sites with separate probe instances
- Reduce metric cardinality for large multi-site deployments

# Configuration Parameters

## Complete Parameter Reference

| Parameter | Type | Required | Default | Description |
|-----------|------|----------|---------|-------------|
| `base_url` | string | Yes | - | Citrix Director URL (without `/Director` path) |
| `interval` | integer | No | `120` | Metric collection interval (seconds) |
| `auth.username` | string | Yes | - | Domain account (`DOMAIN\\username`) |
| `auth.password` | string | Yes | - | Account password — reference via `${secret:<name>.password}`, `${env:VAR}` or `${file:/path}`; inline plaintext is auto-sealed into the OS secret store on install |
| `tls.verify_ssl` | boolean | No | `true` | Validate SSL certificates |
| `timeout` | integer | No | `30` | API request timeout (seconds) |
| `delivery_controller.url` | string | No | - | DDC URL for site filtering |
| `delivery_controller.fallback_urls` | array | No | `[]` | Backup DDC URLs for failover |
| `delivery_controller.site_filter` | string | No | - | Site name to filter metrics |
| `retry.max_attempts` | integer | No | `3` | Retry count for failed API calls |
| `retry.backoff_factor` | float | No | `2.0` | Exponential backoff multiplier |
| `custom_tags` | array | No | `[]` | Additional tags for all metrics |

## Authentication Methods

The Citrix probe automatically selects authentication based on configuration:

| API | Authentication Type | When Used |
|-----|-------------------|-----------|
| **Director OData API** | NTLM | Always (automatic) |
| **DDC REST API** | Basic Auth | When `delivery_controller.url` configured |

Both methods use the same credentials from `auth.username` and `auth.password`.

## Base URL Format

**Correct formats:**
```yaml
# Correct - no path suffix
base_url: "https://director.company.com"
base_url: "https://citrix.company.com"

# Incorrect - includes path
base_url: "https://director.company.com/Director"
```

The probe automatically appends required API paths (`/Odata/v4/Data`, `/Controller`, etc.).

# Metrics Collected

## Session Metrics

Track active and disconnected user sessions:

| Metric Name | Description | Type | Unit |
|------------|-------------|------|------|
| `citrix.sessions.connected` | Active user sessions | Gauge | `#` |
| `citrix.sessions.disconnected` | Disconnected sessions (still consuming resources) | Gauge | `#` |
| `citrix.sessions.zombie` | Sessions disconnected >24 hours | Gauge | `#` |
| `citrix.sessions.simultaneous_users` | Users with multiple active sessions | Gauge | `#` |

**Tags:** `site`, `delivery_group`

## Logon Performance Metrics

Detailed breakdown of logon duration by phase:

| Metric Name | Description | Type | Unit |
|------------|-------------|------|------|
| `citrix.logon.duration_total` | Average total logon time (2-minute window) | Gauge | `s` |
| `citrix.logon.brokering` | Brokering phase duration | Gauge | `s` |
| `citrix.logon.vmstart` | VM start duration | Gauge | `s` |
| `citrix.logon.hdx` | HDX connection establishment | Gauge | `s` |
| `citrix.logon.authentication` | Authentication duration | Gauge | `s` |
| `citrix.logon.gpo` | Group Policy processing time | Gauge | `s` |
| `citrix.logon.scripts` | Logon scripts execution time | Gauge | `s` |
| `citrix.logon.profile` | User profile load time | Gauge | `s` |
| `citrix.logon.interactive` | Interactive session start time | Gauge | `s` |
| `citrix.logon.sessions_opened` | New sessions in 2-minute window | Gauge | `#` |

**Tags:** `site`, `delivery_group`

**Calculation window:** All logon metrics use a complete 2-minute window aligned on minute boundaries, matching Citrix Director calculations.

## Infrastructure Metrics

VDA machine health and capacity:

| Metric Name | Description | Type | Unit |
|------------|-------------|------|------|
| `citrix.machines.registered` | VDA machines successfully registered | Gauge | `#` |
| `citrix.machines.unregistered` | VDA machines failed to register | Gauge | `#` |
| `citrix.machines.faulty` | Machines in fault state | Gauge | `#` |
| `citrix.machines.maintenance` | Machines in maintenance mode | Gauge | `#` |

**Tags:** `site`, `delivery_group`, `machine_catalog`

## Connection Failure Metrics

Track connection failures by category:

| Metric Name | Description | Type | Unit |
|------------|-------------|------|------|
| `citrix.failures.total` | Total connection failures | Counter | `#` |
| `citrix.failures.by_category` | Failures per failure category | Counter | `#` |

**Tags:** `site`, `failure_category` (e.g., `NoCapacityAvailable`, `MachineNotPoweredOn`, `LicenseUnavailable`)

# Integration with Monitoring Systems

## PRTG Network Monitor

### Sensor Configuration

**Sensor 1: Session Metrics**

| Setting | Value |
|---------|-------|
| Device | SenHub Agent |
| Sensor Type | HTTP Data Advanced |
| Name | Citrix - Session Metrics |
| URL | `https://agent:8443/api/{key}/prtg/metrics/citrix` |
| Scanning Interval | 120 seconds |

Key Channels: `citrix.sessions.connected`, `citrix.sessions.disconnected`, `citrix.sessions.zombie`, `citrix.sessions.simultaneous_users`

**Sensor 2: Logon Performance**

| Setting | Value |
|---------|-------|
| Device | SenHub Agent |
| Sensor Type | HTTP Data Advanced |
| Name | Citrix - Logon Performance |
| URL | `https://agent:8443/api/{key}/prtg/metrics/citrix` |
| Scanning Interval | 120 seconds |

Key Channels: `citrix.logon.duration_total`, `citrix.logon.gpo`, `citrix.logon.profile`, `citrix.logon.sessions_opened`

**Sensor 3: Infrastructure Health**

| Setting | Value |
|---------|-------|
| Device | SenHub Agent |
| Sensor Type | HTTP Data Advanced |
| Name | Citrix - Infrastructure |
| URL | `https://agent:8443/api/{key}/prtg/metrics/citrix` |
| Scanning Interval | 120 seconds |

Key Channels: `citrix.machines.registered`, `citrix.machines.unregistered`, `citrix.machines.faulty`, `citrix.failures.total`

## Nagios Integration

### Check Command Configuration

**Check 1: Session Health**
```bash
define command {
    command_name    check_senhub_citrix_sessions
    command_line    $USER1$/check_http \
                    -H $HOSTADDRESS$ \
                    -p 8443 \
                    -S \
                    -u "/api/$ARG1$/nagios/status?probe=citrix&metric=citrix.sessions.connected" \
                    -w $ARG2$ \
                    -c $ARG3$
}

define service {
    use                     generic-service
    host_name               senhub-agent
    service_description     Citrix - Active Sessions
    check_command           check_senhub_citrix_sessions!{agent-key}!400!480
}
```

**Check 2: Logon Performance**
```bash
define command {
    command_name    check_senhub_citrix_logon
    command_line    $USER1$/check_http \
                    -H $HOSTADDRESS$ \
                    -p 8443 \
                    -S \
                    -u "/api/$ARG1$/nagios/status?probe=citrix&metric=citrix.logon.duration_total" \
                    -w $ARG2$ \
                    -c $ARG3$
}

define service {
    use                     generic-service
    host_name               senhub-agent
    service_description     Citrix - Logon Duration
    check_command           check_senhub_citrix_logon!{agent-key}!30!60
}
```


## Common Issues

### Error: 401 Unauthorized

**Symptom:**
```
[ERR] [probe.citrix] Authentication failed url="https://director.company.com" error="401 Unauthorized"
```

**Diagnosis:**

1. **Verify username format** - Citrix requires domain backslash format:
```yaml
# Incorrect formats
username: "user@domain.com"
username: "domain/user"

# Correct format
username: "DOMAIN\\user"  # Note: double backslash in YAML
```

2. **Test credentials manually** with Director web interface:
   - Navigate to: `https://director.company.com`
   - Login with same credentials
   - If web login fails: Credentials incorrect
   - If web login succeeds: Check API permissions

3. **Verify API access permissions:**
   - User must have "Read Only Administrator" or higher role
   - Check Studio > Configuration > Administrators

### Error: No delivery controllers found

**Symptom:**
```
[ERR] [probe.citrix] No delivery controllers discovered base_url="https://director.company.com"
```

**Diagnosis:**

1. **Verify base_url points to Director** (not StoreFront)
2. **Check Director is configured** - Director must be installed and configured with a functional database connection

### Error: Connection timeout

**Symptom:**
```
[ERR] [probe.citrix] Request timeout url="https://director.company.com" error="context deadline exceeded"
```

**Resolution:**

Increase timeout parameter:
```yaml
params:
  timeout: 60  # Increase from default 30 seconds
```

Possible causes: slow network connectivity, Director server overloaded, large Citrix deployment with slow OData queries.

## Debug Logging

Enable debug logging for Citrix probe:

**Runtime log level change:**
```bash
curl -X POST http://localhost:8080/api/{key}/debug/logs \
  -H "Content-Type: application/json" \
  -d '{"module_levels": [{"module": "probe.citrix", "level": "debug"}]}'
```

**Or start agent with verbose logging:**
```bash
./senhub-agent run --verbose --debug-modules probe.citrix
```

## License Requirements

The Citrix probe requires a **Pro** or **Enterprise** license.

| Tier | Citrix Probe |
|------|-------------|
| Free | Not available |
| Pro | Included |
| Enterprise | Included |

Contact support@senhub.io for license information.

## Support

- **Email**: support@senhub.io
- **Documentation**: [docs.senhub.io](https://docs.senhub.io)
