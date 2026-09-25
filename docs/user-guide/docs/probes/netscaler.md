<img src="../../assets/probe-logos/netscaler.svg" alt="" class="probe-page-logo probe-page-logo-mdi">

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
# probes.d/20-netscaler.yaml — each file under probes.d/ is a YAML array of probes
- name: "netscaler-prod"
  type: netscaler
  params:
    base_url: "https://netscaler.company.com"
    username: "monitoring-user"
    password: ${secret:netscaler-prod.password}   # OS secret store; inline plaintext is auto-sealed on install
    interval: 60
```

## Recommended Production Configuration

Full configuration for an HA pair, with SSL validation and custom tags:

```yaml
# probes.d/20-netscaler.yaml
- name: "netscaler-prod"
  type: netscaler
  params:
    base_url: "https://netscaler-1.company.com"      # primary node NSIP
    secondary_url: "https://netscaler-2.company.com" # other HA node, the probe follows the primary role
    username: "monitoring-user"
    password: ${secret:netscaler-prod.password}   # OS secret store; inline plaintext is auto-sealed on install
    insecure_skip_verify: false  # Validate SSL certificates
    timeout: 30                   # API request timeout (seconds)
    interval: 60                  # Collection interval (seconds)
    custom_tags:                  # key: value pairs, attached to every metric
      environment: "production"
      datacenter: "dc-paris-01"
```

`custom_tags` is a map under `params`, not a list of `key` / `value`
entries: a list is ignored without a warning, and so is a map placed
outside `params`. Values must be strings.

# Configuration Parameters

## Complete Parameter Reference

<!-- schema:params:start -->
<!-- Generated from the probe's schema. Run `make docs-params` after changing it. -->

| Parameter | Must set | Default | Description |
|---|---|---|---|
| `base_url` | Yes | - | Management (NSIP) URL of the appliance, or of the primary node of an HA pair. Example: `https://netscaler.example.com` |
| `secondary_url` | No | - | Management URL of the other HA node; the probe follows the primary role, empty disables failover. Example: `https://netscaler-2.example.com` |
| `username` | Yes | - | NITRO API user |
| `password` | In practice | - | NITRO API password; this or api_key is required. A secret: reference it with `${secret:…}`, `${env:…}` or `${file:…}` rather than writing it in the file |
| `api_key` | No | - | NITRO API key used instead of the password. A secret: reference it with `${secret:…}`, `${env:…}` or `${file:…}` rather than writing it in the file |
| `insecure_skip_verify` | No | `false` | Accept the management certificate without verifying it |
| `timeout` | No | `30` | API request timeout in seconds |
| `interval` | No | `60` | Seconds between collections |
| `custom_tags` | No | - | Extra tags attached to every metric, as key: value pairs |

<!-- schema:params:end -->

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
   - Open the SenHub Agent console: `http://localhost:8080/web/{agentkey}/`
   - Open **Outputs**, then the HTTP output, tab **Sensor URLs**
   - Click **Download PRTG lookups**
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
senhub-agent run --filter probe.netscaler
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
- **Documentation**: [agent.senhub.io/docs](https://agent.senhub.io/docs)

## Metric reference

Every metric this probe can emit. The first column is the name the
OTLP and Prometheus outputs use, the second the channel the PRTG and
Nagios outputs carry.

<!-- schema:metrics:start -->
<!-- Generated from the probe's definition. Run `make docs-metrics` after changing it. -->

| Metric | Channel | Unit | Description |
|---|---|---|---|
| `senhub.netscaler.system.cpu.utilization` | `system.cpu.usage` | % | Percentage of CPU resources consumed by the NetScaler appliance |
| `senhub.netscaler.system.cpu.utilization` | `system.cpu.mgmt.usage` | % | CPU usage of the management plane (NSPPE excluded) |
| `senhub.netscaler.system.memory.utilization` | `system.memory.usage` | % | Percentage of memory used by the NetScaler appliance |
| `senhub.netscaler.system.network.throughput` | `system.network.rx` | Mbits/s | Aggregate network receive throughput across all interfaces |
| `senhub.netscaler.system.network.throughput` | `system.network.tx` | Mbits/s | Aggregate network transmit throughput across all interfaces |
| `senhub.netscaler.system.http.messages.rate` | `system.http.requests` | req/s | Rate of HTTP requests received by the appliance |
| `senhub.netscaler.system.http.messages.rate` | `system.http.responses` | resp/s | Rate of HTTP responses sent by the appliance |
| `senhub.netscaler.system.tcp.connections.active` | `system.tcp.client.connections` | # | Current number of client-side TCP connections |
| `senhub.netscaler.system.tcp.connections.active` | `system.tcp.server.connections` | # | Current number of server-side TCP connections |
| `senhub.netscaler.ns.throughput` | `ns.throughput.total` | Mbits/s | Total network throughput (RX + TX) of the NetScaler |
| `senhub.netscaler.ns.throughput` | `ns.http.throughput` | Mbits/s | HTTP-only throughput of the NetScaler |
| `senhub.netscaler.ssl.transactions.rate` | `ssl.transactions.rate` | tx/s | Rate of SSL/TLS transactions processed |
| `senhub.netscaler.ssl.sessions.active` | `ssl.sessions.total` | # | Total number of active SSL sessions |
| `senhub.netscaler.lbvserver.status` | `lbvserver.state` | # | Operational state of the LB virtual server |
| `senhub.netscaler.lbvserver.requests.rate` | `lbvserver.requests.rate` | req/s | Rate of requests hitting the virtual server |
| `senhub.netscaler.lbvserver.connections.active` | `lbvserver.connections.current` | # | Number of active connections on the virtual server |
| `senhub.netscaler.lbvserver.throughput` | `lbvserver.throughput.rx.bytes_per_sec` | bytes/s | Inbound throughput to the virtual server |
| `senhub.netscaler.lbvserver.throughput` | `lbvserver.throughput.tx.bytes_per_sec` | bytes/s | Outbound throughput from the virtual server |
| `senhub.netscaler.lbvserver.spillovers` | `lbvserver.spillovers.total` | # | Total spillovers to backup vServer (saturation indicator) |
| `senhub.netscaler.lbvserver.connections.established` | `lbvserver.connections.established` | # | Established connections for capacity planning |
| `senhub.netscaler.lbvserver.hits` | `lbvserver.hits.total` | # | Total hits for request distribution analysis |
| `senhub.netscaler.service.status` | `service.state` | # | Operational state of the service |
| `senhub.netscaler.service.throughput` | `service.throughput.bytes_per_sec` | bytes/s | Data throughput of the service |
| `senhub.netscaler.service.transactions.active` | `service.transactions.active` | # | Number of active transactions on the service |
| `senhub.netscaler.service.surge_queue_length` | `service.surge_queue_length` | # | Surge queue length (backend saturation indicator) |
| `senhub.netscaler.servicegroup.status` | `servicegroup.state` | # | Operational state of the service group |
| `senhub.netscaler.servicegroup.http.messages.rate` | `servicegroup.requests.rate` | req/s | Rate of requests handled by the service group |
| `senhub.netscaler.servicegroup.http.messages.rate` | `servicegroup.responses.rate` | resp/s | Rate of responses returned by the service group |
| `senhub.netscaler.servicegroup.throughput` | `servicegroup.throughput.bytes_per_sec` | Bytes/s | Data throughput of the service group |
| `senhub.netscaler.servicegroup.connections.active` | `servicegroup.connections.current` | # | Number of active connections on the service group |
| `senhub.netscaler.servicegroup.members` | `servicegroup.members.active` | # | Number of healthy members serving traffic |
| `senhub.netscaler.servicegroup.members` | `servicegroup.members.inactive` | # | Number of inactive members in service group |
| `senhub.netscaler.servicegroup.surge_queue_length` | `servicegroup.surge_queue_length` | # | Surge queue length (backend saturation indicator) |
| `senhub.netscaler.ssl.certificate.days_to_expiration` | `ssl.certificate.days_to_expiration` | days | Days until SSL certificate expires (negative if expired) |
| `senhub.netscaler.ssl.certificate.status` | `ssl.certificate.status` | # | Certificate status: 1=valid, 0=expired |
| `senhub.netscaler.ha.role` | `ha.state` | # | HA role: 2=PRIMARY, 1=SECONDARY, 0=UNKNOWN (per node) |
| `senhub.netscaler.ha.node.status` | `ha.node.state` | # | Node operational state: 1=UP, 0=DOWN (per node) |
| `senhub.netscaler.ha.sync.status` | `ha.sync_status` | # | HA sync status: 1=success, 0=failed (per node) |
| `senhub.netscaler.ha.sync.failures` | `ha.sync_failures` | # | Number of HA synchronization failures (per node) |
| `senhub.netscaler.ha.propagation.timeouts` | `ha.propagation_timeouts` | # | Number of times configuration propagation timed out |
| `senhub.netscaler.ha.heartbeat.packets` | `ha.heartbeat.rx.packets` | # | Total heartbeat packets received from peer node |
| `senhub.netscaler.ha.heartbeat.rate` | `ha.heartbeat.rx.rate` | Custom | Heartbeat packets receive rate (packets/sec) |
| `senhub.netscaler.ha.heartbeat.packets` | `ha.heartbeat.tx.packets` | # | Total heartbeat packets transmitted to peer node |
| `senhub.netscaler.ha.heartbeat.rate` | `ha.heartbeat.tx.rate` | Custom | Heartbeat packets transmit rate (packets/sec) |
| `system.filesystem.utilization` | `disk.percent_used` | % | Disk partition usage percentage |
| `system.filesystem.usage` | `disk.used_kb` | KB | Disk space used in kilobytes |
| `system.filesystem.usage` | `disk.available_kb` | KB | Disk space available in kilobytes |
| `senhub.netscaler.system.network.packets.rate` | `system.network.rx.packets_per_sec` | pps | Receive packets per second |
| `senhub.netscaler.system.network.packets.rate` | `system.network.tx.packets_per_sec` | pps | Transmit packets per second |
| `senhub.netscaler.system.network.packets` | `system.network.rx.packets.total` | # | Total packets received (counter) |
| `senhub.netscaler.system.network.packets` | `system.network.tx.packets.total` | # | Total packets sent (counter) |
| `senhub.netscaler.interface.status` | `interface.state` | # | Interface state: 1=UP/enabled, 0=DOWN/disabled |
| `senhub.netscaler.interface.io` | `interface.rx.bytes.total` | bytes | Total bytes received on the interface |
| `senhub.netscaler.interface.io` | `interface.tx.bytes.total` | bytes | Total bytes transmitted on the interface |
| `senhub.netscaler.interface.throughput` | `interface.rx.mbits_per_sec` | Mbits/s | Receive throughput rate of the interface |
| `senhub.netscaler.interface.throughput` | `interface.tx.mbits_per_sec` | Mbits/s | Transmit throughput rate of the interface |
| `senhub.netscaler.interface.errors` | `interface.rx.errors.total` | # | Total receive errors on the interface |
| `senhub.netscaler.interface.errors` | `interface.tx.errors.total` | # | Total transmit errors on the interface |
| `senhub.netscaler.interface.packets.dropped` | `interface.rx.drops.total` | # | Total inbound packets dropped on the interface |
| `senhub.netscaler.interface.packets.dropped` | `interface.tx.drops.total` | # | Total outbound packets dropped on the interface |
| `senhub.netscaler.interface.link_speed` | `interface.link_speed_mbps` | Mbps | Negotiated link speed of the interface |
| `senhub.netscaler.csvserver.status` | `cs.vserver.state` | # | Content Switching vServer state: UP=7, DOWN=1, etc. |
| `senhub.netscaler.csvserver.hits` | `cs.vserver.hits.total` | # | Total hits on the content switching virtual server |
| `senhub.netscaler.csvserver.requests.rate` | `cs.vserver.requests.rate` | req/s | Rate of requests handled by the CS virtual server |
| `senhub.netscaler.csvserver.connections.active` | `cs.vserver.connections.current` | # | Number of active connections on the CS virtual server |
| `senhub.netscaler.cspolicy.evaluations` | `cs.policy.hits.total` | # | Total times the content switching policy was matched |
| `senhub.netscaler.cspolicy.evaluations` | `cs.policy.undefine_hits.total` | # | Rules not matched |
| `senhub.netscaler.gslb.vserver.status` | `gslb.vserver.state` | # | GSLB vServer state: UP=7, DOWN=1, etc. |
| `senhub.netscaler.gslb.vserver.hits` | `gslb.vserver.hits.total` | # | Total DNS requests resolved by the GSLB virtual server |
| `senhub.netscaler.gslb.vserver.requests.rate` | `gslb.vserver.requests.rate` | req/s | Rate of DNS requests handled by the GSLB virtual server |
| `senhub.netscaler.gslb.vserver.persistence_records` | `gslb.vserver.persistence.records` | # | Number of active persistence records for site affinity |
| `senhub.netscaler.gslb.site.status` | `gslb.site.state` | # | GSLB site state: 1=UP/ACTIVE, 0=DOWN |
| `senhub.netscaler.gslb.site.network_rtt` | `gslb.site.network_rtt_microseconds` | μs | Network round-trip time in microseconds |
| `senhub.netscaler.gslb.site.connections.active` | `gslb.site.connections.current` | # | Number of active connections at the GSLB site |
| `senhub.netscaler.gslb.service.status` | `gslb.service.state` | # | GSLB service state: 1=UP, 0=DOWN |
| `senhub.netscaler.gslb.service.hits` | `gslb.service.hits.total` | # | Total requests directed to the GSLB service |
| `senhub.netscaler.gslb.service.connections.active` | `gslb.service.connections.current` | # | Number of active connections on the GSLB service |
| `senhub.netscaler.cache.hit_ratio` | `cache.hit_ratio_percent` | % | Cache hit ratio percentage |
| `senhub.netscaler.cache.objects` | `cache.objects.count` | # | Number of objects in cache |
| `senhub.netscaler.cache.memory.used` | `cache.memory.used_kb` | KB | Cache memory used in kilobytes |
| `senhub.netscaler.cache.lookups` | `cache.hits.total` | # | Total number of cache hits |
| `senhub.netscaler.cache.lookups` | `cache.misses.total` | # | Total number of cache misses |
| `senhub.netscaler.compression.ratio` | `compression.ratio` | # | Compression ratio |
| `senhub.netscaler.compression.bytes` | `compression.bytes.compressed.total` | bytes | Total bytes after compression |
| `senhub.netscaler.compression.bytes` | `compression.bytes.original.total` | bytes | Original bytes before compression |
| `senhub.netscaler.compression.bandwidth_savings` | `compression.bandwidth_savings.bytes` | bytes | Bandwidth saved by compression |
| `senhub.netscaler.aaa.sessions.active` | `aaa.sessions.active.total` | # | Total active AAA sessions |
| `senhub.netscaler.aaa.vserver.status` | `aaa.vserver.state` | # | Authentication vServer state: 1=UP, 0=DOWN |
| `senhub.netscaler.aaa.vserver.auth_attempts` | `aaa.vserver.auth.successes.total` | # | Total successful authentication attempts |
| `senhub.netscaler.aaa.vserver.auth_attempts` | `aaa.vserver.auth.failures.total` | # | Total failed authentication attempts |
| `senhub.netscaler.vpn.vserver.status` | `vpn.vserver.state` | # | VPN vServer state: 1=UP, 0=DOWN |
| `senhub.netscaler.vpn.vserver.hits` | `vpn.vserver.hits.total` | # | Total hits on the VPN virtual server |
| `senhub.netscaler.vpn.vserver.ica_sessions.active` | `vpn.vserver.ica.sessions.active` | # | Active ICA sessions (Citrix Virtual Apps) |
| `senhub.netscaler.vpn.vserver.connections.established` | `vpn.vserver.connections.established` | # | Number of established VPN connections |
| `senhub.netscaler.appfw.violations.total` | `appfw.violations.total` | # | Total application firewall violations |
| `senhub.netscaler.appfw.blocked` | `appfw.requests.blocked.total` | # | Total requests blocked by WAF |
| `senhub.netscaler.appfw.blocked` | `appfw.responses.blocked.total` | # | Total responses blocked by WAF |
| `senhub.netscaler.appfw.violations.by_type` | `appfw.violations.sqli.total` | # | SQL injection violations detected |
| `senhub.netscaler.appfw.violations.by_type` | `appfw.violations.xss.total` | # | Cross-site scripting violations detected |
| `senhub.netscaler.appfw.violations.by_type` | `appfw.violations.buffer_overflow.total` | # | Buffer overflow violations detected |

<!-- schema:metrics:end -->
