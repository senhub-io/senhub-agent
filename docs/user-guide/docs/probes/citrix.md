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
    director:
      url: "https://director.company.com"
      auth:
        username: "DOMAIN\\svc-monitoring"  # Note: double backslash
        password: ${secret:production-citrix.password}   # OS secret store; inline plaintext is auto-sealed on install
      verify_ssl: true
    interval: 120  # 2 minutes recommended
    timeout: 30
```

**Important notes:**
- `director.url`: Director URL **without** `/Director` path suffix
- `director.auth.username`: Must use double backslash `DOMAIN\\username` format in YAML
- `interval`: 120 seconds (2 minutes) balances data freshness with API load
- Each component (Director, Delivery Controller, License Server) has its own block. An older flat layout (`base_url` with top-level `auth` and `tls`) is still accepted but deprecated; see [Deprecated flat layout](#deprecated-flat-layout).

## Configuration with Site Filtering (Multi-Site)

For multi-site deployments requiring site-specific metrics:

```yaml
# probes.d/10-citrix.yaml
- name: "citrix-paris-site"
  type: citrix
  params:
    director:
      url: "https://director-paris.company.com"
      auth:
        username: "DOMAIN\\svc-monitoring"
        password: ${secret:citrix-paris-site.password}   # OS secret store; inline plaintext is auto-sealed on install

    delivery_controller:
      url: "https://citrix-ddc-paris.company.com"
      fallback_urls:
        - "https://citrix-ddc-paris-backup.company.com"
      site_filter: "SITE-PARIS"  # Filter to specific site
      # No auth block: the Director account is reused

    license_server:
      url: "https://license.company.com"   # optional, enables the licence metrics

    interval: 120
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

<!-- Hand-maintained: this probe's schema lives in senhub-agent-enterprise; check its parser before editing. -->

| Parameter | Required | Default | Description |
|---|---|---|---|
| `director` | Yes | - | Citrix Director, queried over OData with NTLM. Its presence selects the per-component layout |
| `director.url` | Yes | - | Director URL without the `/Director` path |
| `director.auth.username` | Yes | - | Domain account with the Read Only Administrator role, written `DOMAIN\\user` |
| `director.auth.password` | Yes | - | Account password. A secret: reference it with `${secret:...}`, `${env:...}` or `${file:...}` rather than writing it in the file |
| `director.verify_ssl` | No | `true` | Verify the Director certificate |
| `director.fallback_urls` | No | - | Other Director URLs tried when the first one fails |
| `delivery_controller` | No | - | Delivery Controller REST API, queried with Basic auth. Absent: no site inventory and no site filtering |
| `delivery_controller.url` | No | - | Controller URL |
| `delivery_controller.fallback_urls` | No | - | Other controllers tried when the first one fails |
| `delivery_controller.site_filter` | No | - | Site name the metrics are restricted to; empty keeps every site |
| `delivery_controller.verify_ssl` | No | `true` | Verify the controller certificate |
| `delivery_controller.auth.username` | No | - | Account for the controller; empty reuses the Director account |
| `delivery_controller.auth.password` | No | - | Its password. A secret: reference it with `${secret:...}`, `${env:...}` or `${file:...}` |
| `license_server` | No | - | Citrix License Server, queried with Basic auth. Absent: no licence metrics |
| `license_server.url` | No | - | License Server URL |
| `license_server.fallback_urls` | No | - | Other license servers tried when the first one fails |
| `license_server.verify_ssl` | No | `true` | Verify the license server certificate |
| `license_server.auth.username` | No | - | Account for the license server; empty reuses the Director account |
| `license_server.auth.password` | No | - | Its password. A secret: reference it with `${secret:...}`, `${env:...}` or `${file:...}` |
| `interval` | No | `120` | Seconds between collections; logon metrics are computed on a two-minute window |
| `timeout` | No | `30` | API request timeout in seconds, shared by every component |
| `retry.max_attempts` | No | `3` | Attempts per API call |
| `retry.backoff_factor` | No | `2.0` | Multiplier applied to the wait between attempts |
| `debug_identifiers` | No | `false` | Log how session and machine identifiers map instead of collecting metrics, for support |

## Deprecated flat layout

Configurations written before the per-component blocks used one URL and one
account for everything. The probe still accepts that layout, logs a warning
at start, and maps it as follows:

| Flat key | Reads as |
|---|---|
| `director_url` (or the older `base_url`) | `director.url` |
| `auth.username`, `auth.password` | `director.auth`, reused for the Delivery Controller and the License Server |
| `tls.verify_ssl` | `verify_ssl` of every component |
| `delivery_controller.url`, `fallback_urls`, `site_filter` | the same keys of the block |
| `license_server.url`, or `license_server` as a plain URL string | `license_server.url` |

The flat keys are read only when no `director` block is present. Once you
add a `director` block, `director_url`, `base_url`, top-level `auth` and
`tls` are ignored, so migrate the whole file at once:

```yaml
# Deprecated, still accepted
- name: "production-citrix"
  type: citrix
  params:
    director_url: "https://director.company.com"
    auth:
      username: "DOMAIN\\svc-monitoring"
      password: ${secret:production-citrix.password}
    tls:
      verify_ssl: true

# Equivalent current layout
- name: "production-citrix"
  type: citrix
  params:
    director:
      url: "https://director.company.com"
      auth:
        username: "DOMAIN\\svc-monitoring"
        password: ${secret:production-citrix.password}
      verify_ssl: true
```

## Authentication Methods

Each component uses the method its API requires; nothing is selected by
configuration:

| API | Authentication Type | When Used |
|-----|-------------------|-----------|
| **Director OData API** | NTLM | Always |
| **DDC REST API** | Basic Auth | When `delivery_controller.url` is configured |
| **License Server API** | Basic Auth | When `license_server.url` is configured |

The Delivery Controller and the License Server reuse the Director account
unless their block carries its own `auth`.

## Director URL Format

**Correct formats:**
```yaml
# Correct - no path suffix
director:
  url: "https://director.company.com"

# Incorrect - includes path
director:
  url: "https://director.company.com/Director"
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

1. **Verify `director.url` points to Director** (not StoreFront)
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
- **Documentation**: [agent.senhub.io/docs](https://agent.senhub.io/docs)
