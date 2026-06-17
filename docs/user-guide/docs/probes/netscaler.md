!!! warning
    **License: Pro** - Requires a Pro or Enterprise license.

# Overview

The NetScaler probe collects performance, health, and configuration metrics from Citrix NetScaler Application Delivery Controllers using the NITRO REST API. It provides comprehensive visibility into load balancing, SSL offloading, high availability, and system health.

**Key Features:**
- Load Balancer Virtual Servers (LB vServers) monitoring
- Backend Services and Service Groups health tracking
- SSL Certificate expiration monitoring
- High Availability (HA) cluster state and synchronization
- System resource utilization (CPU, memory, network, disk)
- SSL/TLS transaction metrics

**Supported Versions:**
- Citrix NetScaler 11.x, 12.x, 13.x
- Citrix ADC 13.x, 14.x

**API:** NITRO REST API (HTTP/HTTPS)

# Prerequisites

## Required Components

1. **NetScaler NITRO API** - Enabled by default on all NetScaler appliances
2. **Network Connectivity** - Agent server must reach NetScaler NSIP (port 443)
3. **API User Account** - Local or LDAP user with read-only permissions

## API User Requirements

**Option 1: Local NetScaler User (Recommended)**

Create dedicated monitoring user with read-only permissions:

```bash
# NetScaler CLI
add system user monitoring-user "SecurePassword123!" -timeout 900
bind system user monitoring-user read-only
```

**Option 2: LDAP/AD Authentication**

If using external authentication:
- Permissions: Read-only access (Command Policy: `read-only`)
- Session Timeout: 900 seconds minimum (API queries can take time)

## Network Requirements

**Ports:**
- **443/tcp**: HTTPS (NITRO API)
- **80/tcp**: HTTP (optional, not recommended for production)

**Firewall Rules:**
- Allow agent server > NetScaler NSIP on port 443

# Quick Start

## Minimal Configuration

Basic configuration for single NetScaler:

```yaml
probes:
  - name: "netscaler-prod"
    type: netscaler
    params:
      base_url: "https://netscaler.company.com"
      username: "monitoring-user"
      password: "secure-password"
      interval: 60
```

## Recommended Production Configuration

Full configuration with SSL validation and custom tags:

```yaml
probes:
  - name: "netscaler-prod"
    type: netscaler
    params:
      base_url: "https://netscaler.company.com"
      username: "monitoring-user"
      password: "secure-password"
      insecure_skip_verify: false  # Validate SSL certificates
      timeout: 30                   # API request timeout (seconds)
      interval: 60                  # Collection interval (seconds)
    custom_tags:
      - key: "environment"
        value: "production"
      - key: "datacenter"
        value: "dc-paris-01"
```

# Configuration Parameters

## Complete Parameter Reference

| Parameter | Type | Required | Default | Description |
|-----------|------|----------|---------|-------------|
| `base_url` | string | Yes | - | NetScaler management URL (primary node IP recommended) |
| `secondary_url` | string | No | - | Secondary NetScaler URL for HA automatic failover |
| `username` | string | Yes | - | NITRO API username |
| `password` | string | Yes | - | NITRO API password |
| `insecure_skip_verify` | boolean | No | `false` | Skip SSL certificate verification (set `true` when using IPs) |
| `timeout` | integer | No | `30` | API request timeout in seconds |
| `interval` | integer | No | `60` | Metric collection interval in seconds |
| `custom_tags` | array | No | `[]` | Additional tags to attach to all metrics |

# Metrics Overview

The NetScaler probe collects **33 metrics** across **13 resource types**:

## Critical Metrics (Priority 1)

These metrics prevent production outages:

| Category | Metrics | Purpose |
|----------|---------|---------|
| **SSL Certificates** | 2 | Prevent outages from expired certificates |
| **High Availability** | 9 | Detect HA cluster issues, sync failures |
| **Disk Usage** | 3 per partition | Monitor disk space (prevents silent failures) |

## Performance Metrics

| Category | Metrics | Purpose |
|----------|---------|---------|
| **System Resources** | 9 | CPU, memory, network throughput |
| **Load Balancers** | 5+ per vServer | vServer health, request rates, connections |
| **Services** | 3+ per service | Backend service health and throughput |
| **Service Groups** | 3+ per group | Service group status and active members |
| **SSL/TLS** | 2 | Global SSL transaction metrics |

## Complete Metrics List

**System Metrics:**
- `netscaler.system.cpu.usage` - Management CPU utilization (%)
- `netscaler.system.cpu.packet_engine` - Packet engine CPU usage (%)
- `netscaler.system.memory.usage` - Memory utilization (%)
- `netscaler.system.memory.used_mb` - Used memory (MB)
- `netscaler.system.memory.available_mb` - Available memory (MB)
- `netscaler.system.network.throughput_mbps` - Network throughput (Mbps)
- `netscaler.system.network.packets_per_sec` - Packet rate (packets/sec)

**Load Balancer Virtual Server Metrics:**
- `netscaler.lbvserver.state` - vServer state (UP=1, DOWN=0)
- `netscaler.lbvserver.health` - Health percentage (0-100%)
- `netscaler.lbvserver.requests_per_sec` - Request rate
- `netscaler.lbvserver.connections.active` - Active connections
- `netscaler.lbvserver.connections.established` - Total established connections

Tags: `vserver_name`, `vserver_type` (HTTP, SSL, TCP, UDP), `protocol`

**Service Metrics:**
- `netscaler.service.state` - Service state (UP=1, DOWN=0)
- `netscaler.service.throughput_mbps` - Service throughput
- `netscaler.service.active_transactions` - Current transactions

Tags: `service_name`, `vserver_name`, `ip_address`, `port`

**Service Group Metrics:**
- `netscaler.servicegroup.state` - Service group state (UP=1, DOWN=0)
- `netscaler.servicegroup.members.total` - Total group members
- `netscaler.servicegroup.members.active` - Active members

Tags: `servicegroup_name`, `vserver_name`

**SSL Certificate Metrics:**
- `netscaler.ssl.certificate.days_to_expiration` - Days until certificate expires
- `netscaler.ssl.certificate.status` - Certificate status (1=valid, 0=expired/invalid)

Tags: `certname`, `vserver_name`

**SSL Transaction Metrics:**
- `netscaler.ssl.transactions_per_sec` - SSL transactions rate
- `netscaler.ssl.sessions.active` - Active SSL sessions

**High Availability Metrics:**
- `netscaler.ha.state` - Node role (2=PRIMARY, 1=SECONDARY, 0=UNKNOWN)
- `netscaler.ha.node.state` - Operational state (1=UP, 0=DOWN)
- `netscaler.ha.sync_status` - Sync status (1=SUCCESS, 0=FAILED)
- `netscaler.ha.sync_failures` - Sync failure counter
- `netscaler.ha.heartbeat_failures` - Heartbeat failure counter

Tags: `ha_node_id` (0 or 1), `ha_node_ip`, `is_local_node`, `connected_to`

**Disk Usage Metrics:**
- `netscaler.disk.usage_percent` - Disk utilization (%)
- `netscaler.disk.used_mb` - Used space (MB)
- `netscaler.disk.available_mb` - Available space (MB)

Tags: `partition` (`/`, `/var`, `/flash`)

# High Availability (HA) Monitoring

The NetScaler probe fully supports **High Availability clusters**:

## HA Architecture

- Connects to **ONE node** (primary or secondary)
- Collects metrics for **BOTH nodes** (local + remote)
- Identifies nodes by IP address and node ID

## Per-Node Metrics

| Metric | Description | Values |
|--------|-------------|--------|
| `ha.state` | Node role | 2=PRIMARY, 1=SECONDARY, 0=UNKNOWN |
| `ha.node.state` | Operational state | 1=UP, 0=DOWN |
| `ha.sync_status` | Configuration sync status | 1=SUCCESS, 0=FAILED |
| `ha.sync_failures` | Sync failure counter | Incremental counter |

## HA Tags

- `ha_node_id`: Node ID (0 or 1)
- `ha_node_ip`: Node IP address (e.g., "10.0.208.7")
- `is_local_node`: "true" for connected node, "false" for remote
- `connected_to`: Hostname of connected node

## Recommended Alerting

- **WARNING**: `ha.sync_status = 0` (FAILED) - Configuration out of sync, investigate network issues or config conflicts
- **CRITICAL**: `ha.state = 0` (UNKNOWN - HA broken) - Immediate action required
- **INFO**: `ha.sync_failures > 0` - Intermittent sync issues, monitor if persistent

# SSL Certificate Monitoring

## Purpose

Prevent production outages from expired SSL certificates.

## Metrics

| Metric | Description | Values |
|--------|-------------|--------|
| `netscaler.ssl.certificate.days_to_expiration` | Days until expiration | Positive integer (days) |
| `netscaler.ssl.certificate.status` | Certificate validity | 1=valid, 0=expired/invalid |

Tags: `certname` (e.g., "wildcard_prod_2025")

## Recommended Alert Thresholds

- **30 days**: WARNING - Start renewal process
- **15 days**: HIGH - Renewal urgent
- **7 days**: CRITICAL - Imminent expiration
- **0 days**: EXPIRED - Production outage risk

## PRTG Alert Configuration

```json
{
  "prtg": {
    "result": [
      {
        "channel": "SSL Certificate Days to Expire",
        "value": 23,
        "unit": "Custom",
        "customunit": "days",
        "LimitMinWarning": 30,
        "LimitMinError": 7,
        "LimitMode": 1
      }
    ]
  }
}
```

# PRTG Integration

## Value Lookups

The NetScaler probe includes **PRTG Value Lookups** for human-readable status values.

**Available Lookups:**
- `netscaler.lbvserver.state`: UP, DOWN, OUT OF SERVICE, BUSY, UNKNOWN
- `netscaler.service.state`: UP, DOWN, OUT OF SERVICE, BUSY, UNKNOWN
- `netscaler.servicegroup.state`: UP, DOWN, OUT OF SERVICE, BUSY, UNKNOWN
- `netscaler.interface.state`: ENABLED, DISABLED
- `netscaler.ssl.certificate.status`: VALID, INVALID
- `netscaler.ha.state`: PRIMARY, SECONDARY, UNKNOWN
- `netscaler.ha.node.state`: UP, DOWN
- `netscaler.ha.sync_status`: SUCCESS, FAILED

## Installing Lookups

1. **Download lookups:**
   - Open SenHub Agent dashboard: `http://localhost:8080/web/{agentkey}/`
   - Navigate to **Sensor Builder**
   - Click **"Download PRTG Lookups"** button
   - Save `senhub-prtg-lookups.zip`

2. **Install on PRTG Server:**
   ```powershell
   # Extract to PRTG lookups directory
   Expand-Archive senhub-prtg-lookups.zip `
     -DestinationPath "C:\Program Files (x86)\PRTG Network Monitor\lookups\custom\"
   ```

3. **Refresh lookups in PRTG:**
   - Navigate to: **Administration > System Administration > Administrative Tools**
   - Click **"Load Lookups and File Lists"**
   - Verify lookups appear in sensor channel configuration

## PRTG Sensor Configuration

**Sensor 1: Load Balancing**

| Setting | Value |
|---------|-------|
| Device | SenHub Agent |
| Sensor Type | HTTP Data Advanced |
| Name | NetScaler - Load Balancing |
| URL | `https://agent:8443/api/{key}/prtg/metrics/netscaler?filter=metric_view:load_balancing` |
| Scanning Interval | 120 seconds |

Key Channels: Virtual Server State (with lookup), Hits (requests/sec), Active Connections, Service Group Health (%)

**Sensor 2: SSL Monitoring**

| Setting | Value |
|---------|-------|
| Device | SenHub Agent |
| Sensor Type | HTTP Data Advanced |
| Name | NetScaler - SSL Certificates |
| URL | `https://agent:8443/api/{key}/prtg/metrics/netscaler?filter=metric_view:ssl_certificates` |
| Scanning Interval | 300 seconds |

Key Channels: SSL Certificate Days to Expire, SSL Certificate Status (with lookup), SSL Transactions per Second

**Sensor 3: System Resources**

| Setting | Value |
|---------|-------|
| Device | SenHub Agent |
| Sensor Type | HTTP Data Advanced |
| Name | NetScaler - System Health |
| URL | `https://agent:8443/api/{key}/prtg/metrics/netscaler?filter=metric_view:system` |
| Scanning Interval | 120 seconds |

Key Channels: CPU Usage (%), Memory Usage (%), Network Throughput (Mbps), Disk Usage (%)

**Sensor 4: High Availability**

| Setting | Value |
|---------|-------|
| Device | SenHub Agent |
| Sensor Type | HTTP Data Advanced |
| Name | NetScaler - HA Cluster |
| URL | `https://agent:8443/api/{key}/prtg/metrics/netscaler?filter=metric_view:ha` |
| Scanning Interval | 120 seconds |

Key Channels: HA State (with lookup), HA Node State (with lookup), HA Sync Status (with lookup), HA Sync Failures (counter)

# Nagios Integration

## Check Command Configuration

**Check 1: Virtual Server Health**
```bash
define command {
    command_name    check_senhub_netscaler_vserver
    command_line    $USER1$/check_http \
                    -H $HOSTADDRESS$ \
                    -p 8443 \
                    -S \
                    -u "/api/$ARG1$/nagios/status?probe=netscaler&metric=netscaler.lbvserver.state&filter=vserver_name:$ARG2$" \
                    -w 1: \
                    -c 1:
}

define service {
    use                     generic-service
    host_name               senhub-agent
    service_description     NetScaler - Web vServer
    check_command           check_senhub_netscaler_vserver!{agent-key}!Web-vServer
}
```

**Check 2: SSL Certificate Expiration**
```bash
define command {
    command_name    check_senhub_netscaler_ssl_cert
    command_line    $USER1$/check_http \
                    -H $HOSTADDRESS$ \
                    -p 8443 \
                    -S \
                    -u "/api/$ARG1$/nagios/status?probe=netscaler&metric=netscaler.ssl.certificate.days_to_expiration&filter=certname:$ARG2$" \
                    -w $ARG3$: \
                    -c $ARG4$:
}

define service {
    use                     generic-service
    host_name               senhub-agent
    service_description     NetScaler - SSL Cert wildcard_prod
    check_command           check_senhub_netscaler_ssl_cert!{agent-key}!wildcard_prod_2025!30!7
}
```


## Common Issues

### 1. Authentication Failed

**Error:**
```
Failed to authenticate with Netscaler
```

**Solutions:**
- Verify username/password in configuration
- Check user has read-only permissions on NetScaler
- Verify user session timeout is 900+ seconds
- Test credentials with `curl`:
  ```bash
  curl -k -X POST https://netscaler.example.com/nitro/v1/config/login \
    -H "Content-Type: application/json" \
    -d '{"login":{"username":"user","password":"pass"}}'
  ```

### 2. SSL Certificate Verification Failed

**Error:**
```
Failed to create NITRO client: x509: certificate signed by unknown authority
```

**Solutions:**
- **Option 1 (Recommended)**: Install NetScaler CA certificate on agent host
- **Option 2 (Not recommended)**: Set `insecure_skip_verify: true` (testing only)

### 3. Timeout Errors

**Error:**
```
Context deadline exceeded
```

**Solutions:**
- Increase `timeout` parameter (try 60 seconds)
- Check network latency to NetScaler
- Verify NetScaler CPU usage is not maxed out

### 4. HA Metrics Missing

**Error:** No HA metrics collected

**Possible causes:**
- NetScaler is not configured for High Availability (standalone mode)
- This is expected behavior for standalone deployments
- Check NetScaler: `show ha node` (should return 0 or 2 nodes)

## Debug Logging

Enable debug logging for NetScaler probe:

**Runtime log level change:**
```bash
curl -X POST http://localhost:8080/api/{agentkey}/debug/logs \
  -H "Content-Type: application/json" \
  -d '{"module_levels": [{"module": "probe.netscaler", "level": "debug"}]}'
```

**Or start agent with verbose logging:**
```bash
./senhub-agent run --verbose --debug-modules probe.netscaler
```

## License Requirements

The NetScaler probe requires a **Pro** or **Enterprise** license.

| Tier | NetScaler Probe |
|------|----------------|
| Free | Not available |
| Pro | Included |
| Enterprise | Included |

Contact support@senhub.io for license information.

## Support

- **Email**: support@senhub.io
- **Documentation**: [docs.senhub.io](https://docs.senhub.io)
