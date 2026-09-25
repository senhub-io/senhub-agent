<img src="../../assets/probe-logos/citrix.svg" alt="" class="probe-page-logo probe-page-logo-si">

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

<!-- schema:params:start -->
<!-- Generated from the probe's schema. Run `make docs-params` after changing it. -->

| Parameter | Must set | Default | Description |
|---|---|---|---|
| `director` | In practice | - | Citrix Director, queried over OData with NTLM |
| `director.url` | Yes | - | Director URL without the /Director path. Example: `https://director.example.com` |
| `director.auth` | Yes | - | Domain account with the Read Only Administrator role |
| `director.auth.username` | Yes | - | Account as DOMAIN\user. Example: `DOMAIN\svc-monitoring` |
| `director.auth.password` | Yes | - | Account password. A secret: reference it with `${secret:…}`, `${env:…}` or `${file:…}` rather than writing it in the file |
| `director.verify_ssl` | No | `true` | Verify the Director certificate |
| `director.fallback_urls` | No | - | Other Director URLs tried when the first one fails |
| `delivery_controller` | No | - | Delivery Controller REST API, queried with Basic auth; empty skips site inventory and site filtering |
| `delivery_controller.url` | No | - | Controller URL. Example: `https://ddc.example.com` |
| `delivery_controller.fallback_urls` | No | - | Other controllers tried when the first one fails |
| `delivery_controller.site_filter` | No | - | Site name the metrics are restricted to; empty keeps every site |
| `delivery_controller.verify_ssl` | No | `true` | Verify the controller certificate |
| `delivery_controller.auth` | No | - | Credentials for the controller; empty reuses the Director account |
| `delivery_controller.auth.username` | No | - | Account for the controller |
| `delivery_controller.auth.password` | No | - | Password of that account. A secret: reference it with `${secret:…}`, `${env:…}` or `${file:…}` rather than writing it in the file |
| `license_server` | No | - | Citrix License Server, queried with Basic auth; empty skips licence metrics. The deprecated flat layout also accepts a bare URL here |
| `license_server.url` | No | - | License Server URL. Example: `https://license.example.com` |
| `license_server.fallback_urls` | No | - | Other license servers tried when the first one fails |
| `license_server.verify_ssl` | No | `true` | Verify the license server certificate |
| `license_server.auth` | No | - | Credentials for the license server; empty reuses the Director account |
| `license_server.auth.username` | No | - | Account for the license server |
| `license_server.auth.password` | No | - | Password of that account. A secret: reference it with `${secret:…}`, `${env:…}` or `${file:…}` rather than writing it in the file |
| `interval` | No | `120` | Seconds between collections; logon metrics are computed on a two-minute window |
| `timeout` | No | `30` | API request timeout in seconds |
| `retry` | No | - | Retry policy for failed API calls |
| `retry.max_attempts` | No | `3` | Attempts per call |
| `retry.backoff_factor` | No | `2` | Multiplier applied to the wait between attempts |
| `debug_identifiers` | No | `false` | Log how session and machine identifiers map instead of collecting metrics, for support |
| `director_url` | No | - | Deprecated flat layout: Director URL, read only when no director block is set. Also accepted: `base_url` |
| `auth` | No | - | Deprecated flat layout: account shared by every component, read only when no director block is set |
| `auth.username` | No | - | Account used for every component |
| `auth.password` | No | - | Password of that account. A secret: reference it with `${secret:…}`, `${env:…}` or `${file:…}` rather than writing it in the file |
| `tls` | No | - | Deprecated flat layout: certificate verification shared by every component |
| `tls.verify_ssl` | No | `true` | Verify the certificate of every component |

<!-- schema:params:end -->

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

Each metric is listed under the name the OTLP and Prometheus outputs use,
followed by the PRTG/Nagios channel that carries the same value. Several
channels collapse into one OTel metric distinguished by an attribute; the
attribute value is given where that happens.

## Sessions

| Metric | Attribute | Channel | Unit |
|---|---|---|---|
| `senhub.citrix.sessions.count` | `session.state=connected` | `sessions_connected` | `#` |
| `senhub.citrix.sessions.count` | `session.state=disconnected` | `sessions_disconnected` | `#` |

**Tags:** `site`, `delivery_group`

## Logon performance

Logon duration is broken down by phase. Every phase shares one metric,
distinguished by `senhub.citrix.logon.phase`.

| Metric | Attribute | Channel | Unit |
|---|---|---|---|
| `senhub.citrix.logon.duration_1h_average` | - | `logon_duration_avg_1h` | `s` |
| `senhub.citrix.logon.last_session_duration` | - | `logon_duration_total` | `s` |
| `senhub.citrix.logon.sessions_opened` | - | `logon_sessions_opened` | `#` |
| `senhub.citrix.logon.phase_duration` | `logon.phase=brokering` | `logon_brokering` | `s` |
| `senhub.citrix.logon.phase_duration` | `logon.phase=vm_start` | `logon_vmstart` | `s` |
| `senhub.citrix.logon.phase_duration` | `logon.phase=hdx` | `logon_hdx` | `s` |
| `senhub.citrix.logon.phase_duration` | `logon.phase=authentication` | `logon_authentication` | `s` |
| `senhub.citrix.logon.phase_duration` | `logon.phase=gpo` | `logon_gpo` | `s` |
| `senhub.citrix.logon.phase_duration` | `logon.phase=scripts` | `logon_scripts` | `s` |
| `senhub.citrix.logon.phase_duration` | `logon.phase=profile` | `logon_profile` | `s` |
| `senhub.citrix.logon.phase_duration` | `logon.phase=interactive` | `logon_interactive` | `s` |

**Tags:** `site`, `delivery_group`

**Calculation window:** every logon metric uses a complete 2-minute window aligned on minute boundaries, matching Citrix Director's own calculation.

## Machines

| Metric | Attribute | Channel | Unit |
|---|---|---|---|
| `senhub.citrix.machines.total` | - | `machines_total` | `#` |
| `senhub.citrix.machines.by_registration_state` | `machine.registration_state=registered` | `machines_registered` | `#` |
| `senhub.citrix.machines.by_registration_state` | `machine.registration_state=unregistered` | `machines_unregistered` | `#` |
| `senhub.citrix.machines.by_registration_state` | `machine.registration_state=faulty` | `machines_faulty` | `#` |
| `senhub.citrix.machines.by_registration_state` | `machine.registration_state=maintenance` | `machines_maintenance` | `#` |
| `senhub.citrix.machines.overloaded` | - | `load_overloaded_machines` | `#` |
| `senhub.citrix.machines.multi_session_fault_total` | - | `machines_faulty_total` | `#` |
| `senhub.citrix.machines.by_fault_state` | `machine.fault_state=boot_failure` | `boot_failure` | `#` |
| `senhub.citrix.machines.by_fault_state` | `machine.fault_state=stuck_at_boot` | `stuck_at_boot` | `#` |
| `senhub.citrix.machines.by_fault_state` | `machine.fault_state=unregistered` | `unregistered` | `#` |
| `senhub.citrix.machines.by_fault_state` | `machine.fault_state=max_capacity` | `max_capacity` | `#` |
| `senhub.citrix.machines.by_fault_state` | `machine.fault_state=vm_not_found` | `vm_not_found` | `#` |
| `senhub.citrix.machines.by_fault_state` | `machine.fault_state=unknown` | `unknown` | `#` |

**Tags:** `site`, `delivery_group`, `machine_catalog`

## Load index

| Metric | Attribute | Channel | Unit |
|---|---|---|---|
| `senhub.citrix.load_index.ratio` | `load_index.dimension=effective` | `load_index_effective` | `%` |
| `senhub.citrix.load_index.ratio` | `load_index.dimension=cpu` | `load_index_cpu` | `%` |
| `senhub.citrix.load_index.ratio` | `load_index.dimension=memory` | `load_index_memory` | `%` |
| `senhub.citrix.load_index.ratio` | `load_index.dimension=disk` | `load_index_disk` | `%` |
| `senhub.citrix.load_index.ratio` | `load_index.dimension=network` | `load_index_network` | `%` |
| `senhub.citrix.load_index.ratio` | `load_index.dimension=sessions` | `load_index_sessions` | `%` |

## Connection failures

| Metric | Attribute | Channel | Unit |
|---|---|---|---|
| `senhub.citrix.connection_failures.total` | - | `failures_total` | `#` |
| `senhub.citrix.connection_failures.by_category` | `connection_failure.category=client_connection` | `client_connection_failures` | `#` |
| `senhub.citrix.connection_failures.by_category` | `connection_failure.category=configuration` | `configuration_errors` | `#` |
| `senhub.citrix.connection_failures.by_category` | `connection_failure.category=machine` | `machine_failures` | `#` |
| `senhub.citrix.connection_failures.by_category` | `connection_failure.category=capacity_unavailable` | `capacity_unavailable` | `#` |
| `senhub.citrix.connection_failures.by_category` | `connection_failure.category=licenses_unavailable` | `licenses_unavailable` | `#` |
| `senhub.citrix.connection_failures.by_category` | `connection_failure.category=other` | `other_failures` | `#` |

**Tags:** `site`

## Licensing

Collected only when `license_server` is configured.

| Metric | Channel | Unit |
|---|---|---|
| `senhub.citrix.license.sessions_active` | `license_sessions_active` | `#` |
| `senhub.citrix.license.peak_concurrent_users` | `license_peak_concurrent` | `#` |
| `senhub.citrix.license.unique_users` | `license_unique_users` | `#` |
| `senhub.citrix.license.grace.sessions_remaining` | `license_grace_sessions_left` | `#` |
| `senhub.citrix.license.grace.active` | `license_grace_period_active` | `#` |
| `senhub.citrix.license.grace.time_remaining` | `license_grace_hours_left` | `h` |

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

Key Channels: `sessions_connected`, `sessions_disconnected`, `machines_registered`, `machines_unregistered`

**Sensor 2: Logon Performance**

| Setting | Value |
|---------|-------|
| Device | SenHub Agent |
| Sensor Type | HTTP Data Advanced |
| Name | Citrix - Logon Performance |
| URL | `https://agent:8443/api/{key}/prtg/metrics/citrix` |
| Scanning Interval | 120 seconds |

Key Channels: `logon_duration_total`, `logon_gpo`, `logon_profile`, `logon_sessions_opened`

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
senhub-agent run --filter probe.citrix
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

## Metric reference

Every metric this probe can emit. **Metric** is the OpenTelemetry name the
OTLP, Prometheus and Zabbix outputs derive theirs from. **Name** is what a
[Nagios check](../nagios.md) and the API `metrics=` filter match.
**PRTG channel** is the label PRTG shows, placeholders filled from the
series' tags.

<!-- schema:metrics:start -->
<!-- Generated from the probe's definition. Run `make docs-metrics` after changing it. -->

| Metric | Name | PRTG channel | Unit | Description |
|---|---|---|---|---|
| `senhub.citrix.sessions.count` | `sessions_connected` | Sessions Connected | # | Number of active user sessions currently connected to virtual desktops |
| `senhub.citrix.sessions.count` | `sessions_disconnected` | Sessions Disconnected | # | Number of user sessions in disconnected state but not yet logged off |
| `senhub.citrix.machines.total` | `machines_total` | Machines Total | # | Total number of VDA machines in the delivery group |
| `senhub.citrix.machines.by_registration_state` | `machines_registered` | Machines Registered | # | Number of VDA machines successfully registered with the Delivery Controller |
| `senhub.citrix.machines.by_registration_state` | `machines_unregistered` | Machines Unregistered | # | Number of VDA machines not registered with the Delivery Controller |
| `senhub.citrix.machines.by_registration_state` | `machines_faulty` | Machines Faulty | # | Number of VDA machines in a faulty state unable to accept user connections |
| `senhub.citrix.machines.by_registration_state` | `machines_maintenance` | Machines in Maintenance | # | Number of VDA machines placed in maintenance mode by an administrator |
| `senhub.citrix.logon.duration_1h_average` | `logon_duration_avg_1h` | Logon Duration Average (1h) | s | Average end-to-end logon duration over the last hour |
| `senhub.citrix.logon.last_session_duration` | `logon_duration_total` | Logon Duration Total | s | Total cumulative logon duration for the most recent logon session |
| `senhub.citrix.logon.sessions_opened` | `logon_sessions_opened` | Logon Sessions Opened | # | Number of new sessions opened during the measurement period |
| `senhub.citrix.logon.phase_duration` | `logon_brokering` | Logon Brokering | s | Time spent by the Delivery Controller brokering the session to a VDA |
| `senhub.citrix.logon.phase_duration` | `logon_vmstart` | Logon VM Start | s | Time spent starting or resuming the virtual machine for the session |
| `senhub.citrix.logon.phase_duration` | `logon_hdx` | Logon HDX | s | Time spent establishing the HDX/ICA connection between client and VDA |
| `senhub.citrix.logon.phase_duration` | `logon_authentication` | Logon Authentication | s | Time spent authenticating user credentials during logon |
| `senhub.citrix.logon.phase_duration` | `logon_gpo` | Logon GPO | s | Time spent applying Group Policy Objects during session logon |
| `senhub.citrix.logon.phase_duration` | `logon_scripts` | Logon Scripts | s | Time spent executing logon scripts during session initialization |
| `senhub.citrix.logon.phase_duration` | `logon_profile` | Logon Profile | s | Time spent loading the user profile during session logon |
| `senhub.citrix.logon.phase_duration` | `logon_interactive` | Logon Interactive | s | Time spent on interactive session setup after profile load completes |
| `senhub.citrix.connection_failures.total` | `failures_total` | Connection Failures Total | # | Total number of failed user connection attempts across all failure categories |
| `senhub.citrix.connection_failures.by_category` | `client_connection_failures` | Client Connection Failures | # | Connection failures caused by client-side issues such as network or endpoint errors |
| `senhub.citrix.connection_failures.by_category` | `configuration_errors` | Configuration Errors | # | Connection failures caused by misconfigured delivery groups or policies |
| `senhub.citrix.connection_failures.by_category` | `machine_failures` | Machine Failures | # | Connection failures caused by VDA machines being unavailable or unresponsive |
| `senhub.citrix.connection_failures.by_category` | `capacity_unavailable` | Capacity Unavailable | # | Connection failures due to no available capacity in the delivery group |
| `senhub.citrix.connection_failures.by_category` | `licenses_unavailable` | Licenses Unavailable | # | Connection failures due to insufficient Citrix licenses available |
| `senhub.citrix.connection_failures.by_category` | `other_failures` | Other Failures | # | Connection failures not classified in other failure categories |
| `senhub.citrix.load_index.ratio` | `load_index_effective` | Load Index Effective Avg | % | Average effective load evaluator index across all registered VDAs |
| `senhub.citrix.load_index.ratio` | `load_index_cpu` | Load Index CPU Avg | % | Average CPU load evaluator index across all registered VDAs |
| `senhub.citrix.load_index.ratio` | `load_index_memory` | Load Index Memory Avg | % | Average memory load evaluator index across all registered VDAs |
| `senhub.citrix.load_index.ratio` | `load_index_disk` | Load Index Disk Avg | % | Average disk load evaluator index across all registered VDAs |
| `senhub.citrix.load_index.ratio` | `load_index_network` | Load Index Network Avg | % | Average network load evaluator index across all registered VDAs |
| `senhub.citrix.load_index.ratio` | `load_index_sessions` | Load Index Sessions Avg | % | Average session count load evaluator index across all registered VDAs |
| `senhub.citrix.machines.overloaded` | `load_overloaded_machines` | Overloaded Machines | # | Number of VDA machines reporting a load index at or above the overload threshold |
| `senhub.citrix.license.sessions_active` | `license_sessions_active` | Licensed Sessions Active | # | Number of currently active sessions consuming a Citrix license |
| `senhub.citrix.license.peak_concurrent_users` | `license_peak_concurrent` | License Peak Concurrent Users | # | Highest number of concurrent licensed users recorded in the current period |
| `senhub.citrix.license.unique_users` | `license_unique_users` | License Unique Users | # | Number of unique users who have consumed a license in the current period |
| `senhub.citrix.license.grace.sessions_remaining` | `license_grace_sessions_left` | License Grace Sessions Left | # | Remaining supplemental grace sessions available when license limit is exceeded |
| `senhub.citrix.license.grace.active` | `license_grace_period_active` | License Grace Period Active | # | Indicates whether the supplemental grace period is currently active (1) or not (0) |
| `senhub.citrix.license.grace.time_remaining` | `license_grace_hours_left` | License Grace Hours Left | h | Hours remaining before the supplemental grace period expires |
| `senhub.citrix.machines.multi_session_fault_total` | `machines_faulty_total` | Machines Faulty Total (Multi-Session) | # | Total number of multi-session VDA machines in a fault state |
| `senhub.citrix.machines.by_fault_state` | `boot_failure` | Boot Failure | # | Number of machines that failed to boot within the expected timeframe |
| `senhub.citrix.machines.by_fault_state` | `stuck_at_boot` | Stuck At Boot | # | Number of machines stuck in the boot process and not progressing to registration |
| `senhub.citrix.machines.by_fault_state` | `unregistered` | Unregistered | # | Number of powered-on machines that have not registered with the Delivery Controller |
| `senhub.citrix.machines.by_fault_state` | `max_capacity` | Max Capacity | # | Number of machines that have reached their maximum session capacity |
| `senhub.citrix.machines.by_fault_state` | `vm_not_found` | VM Not Found | # | Number of machines whose virtual machine could not be found on the hypervisor |
| `senhub.citrix.machines.by_fault_state` | `unknown` | Unknown | # | Number of machines in an unrecognized or undetermined fault state |

<!-- schema:metrics:end -->
