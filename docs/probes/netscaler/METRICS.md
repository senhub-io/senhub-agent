# NetScaler ADC - Metrics Reference

Complete reference of metrics collected by the NetScaler probe.

## Metrics Summary

**Phase 1 (Available)**: 33 metrics across 13 resource types
**Phase 2 (Planned)**: 20+ additional metrics (interfaces, content switching, GSLB)
**Phase 3 (Planned)**: 15+ additional metrics (AAA, VPN, AppFW, compression)

---

## System Metrics

Global system resource utilization and performance.

### CPU & Memory

| Metric Name | Unit | Description | Typical Values |
|-------------|------|-------------|----------------|
| `netscaler.system.cpu.usage.percent` | % | Overall CPU utilization | 10-60% normal, >80% high |
| `netscaler.system.cpu.mgmt.usage.percent` | % | Management CPU utilization | <20% typically |
| `netscaler.system.memory.usage.percent` | % | Memory utilization | 40-70% normal, >85% high |

**Alerting Recommendations:**
- ⚠️ WARNING: cpu.usage > 70%
- 🚨 CRITICAL: cpu.usage > 85%
- ⚠️ WARNING: memory.usage > 80%
- 🚨 CRITICAL: memory.usage > 90%

### Network Throughput

| Metric Name | Unit | Description |
|-------------|------|-------------|
| `netscaler.system.network.rx.mbits_per_sec` | Mbits/s | System-wide receive throughput |
| `netscaler.system.network.tx.mbits_per_sec` | Mbits/s | System-wide transmit throughput |
| `netscaler.ns.throughput.total.mbits_per_sec` | Mbits/s | Total NetScaler throughput |
| `netscaler.ns.http.throughput.mbits_per_sec` | Mbits/s | HTTP-specific throughput |

**Use Cases:**
- Capacity planning and trend analysis
- Identifying traffic spikes or DDoS attacks
- Verifying bandwidth utilization vs. licensed capacity

### HTTP Traffic

| Metric Name | Unit | Description |
|-------------|------|-------------|
| `netscaler.system.http.requests.rate` | req/s | HTTP requests per second |
| `netscaler.system.http.responses.rate` | resp/s | HTTP responses per second |

**Alerting Recommendations:**
- Track baseline request rates
- Alert on sudden drops (service degradation)
- Alert on unexpected spikes (potential attack)

### TCP Connections

| Metric Name | Unit | Description |
|-------------|------|-------------|
| `netscaler.system.tcp.client.connections.current` | # | Active client-side TCP connections |
| `netscaler.system.tcp.server.connections.current` | # | Active server-side TCP connections |

**Use Cases:**
- Monitor connection pool saturation
- Detect connection leaks or TIME_WAIT issues
- Capacity planning for concurrent users

---

## SSL/TLS Metrics

### Global SSL Performance

| Metric Name | Unit | Description |
|-------------|------|-------------|
| `netscaler.ssl.transactions.rate` | tx/s | SSL/TLS transactions per second |
| `netscaler.ssl.sessions.total` | # | Total active SSL sessions |

**Use Cases:**
- SSL offloading performance monitoring
- Capacity planning for SSL acceleration hardware
- Detecting SSL handshake issues

### SSL Certificate Monitoring (⚠️ CRITICAL)

| Metric Name | Unit | Description | Tags |
|-------------|------|-------------|------|
| `netscaler.ssl.certificate.days_to_expiration` | days | Days until certificate expires | `certname` |
| `netscaler.ssl.certificate.status` | # | Certificate validity: 1=valid, 0=invalid/expired | `certname` |

**PRTG Lookup:** `netscaler.ssl.certificate.status`
- **1** = VALID
- **0** = INVALID/EXPIRED

**Alerting Recommendations:**
```
📅 30 days: WARNING (initiate renewal)
⚠️ 15 days: HIGH (urgent renewal)
🚨 7 days: CRITICAL (imminent expiration)
💥 status=0: EXPIRED (PRODUCTION OUTAGE!)
```

**Example Tags:**
- `certname=wildcard_prod_2025`
- `certname=api_server_cert`
- `certname=vpn_gateway_cert`

**Common Scenarios:**
- Wildcard certificates covering multiple domains
- Individual server certificates (www, api, etc.)
- VPN/Gateway certificates
- Internal CA certificates

---

## Load Balancer Virtual Servers

Per-vServer metrics for load balancing health and performance.

### VServer Health & State

| Metric Name | Unit | Description | Tags |
|-------------|------|-------------|------|
| `netscaler.lbvserver.state` | # | vServer operational state | `vserver` |

**PRTG Lookup:** `netscaler.lbvserver.state`
- **7** = UP (operational)
- **5** = TROFS (transitioning out of service - graceful shutdown)
- **4** = OUT OF SERVICE (administratively disabled)
- **3** = BUSY (max connections reached)
- **2** = UNKNOWN (state cannot be determined)
- **1** = DOWN (not responding to health checks)

**Alerting:**
- 🚨 CRITICAL: state = 1 (DOWN)
- ⚠️ WARNING: state = 2 (UNKNOWN)
- ⚠️ WARNING: state = 3 (BUSY - at capacity)
- 📊 INFO: state = 5 (TROFS - maintenance mode)

### VServer Performance

| Metric Name | Unit | Description | Tags |
|-------------|------|-------------|------|
| `netscaler.lbvserver.requests.rate` | req/s | Requests per second | `vserver` |
| `netscaler.lbvserver.connections.current` | # | Current active connections | `vserver` |
| `netscaler.lbvserver.throughput.rx.bytes_per_sec` | bytes/s | Received throughput | `vserver` |
| `netscaler.lbvserver.throughput.tx.bytes_per_sec` | bytes/s | Transmitted throughput | `vserver` |

**Use Cases:**
- Identify hot vServers (top request rates)
- Monitor connection distribution across vServers
- Capacity planning per application
- Detect anomalies (sudden traffic drops or spikes)

**Example vServer Names:**
- `http_webservers_80`
- `https_api_443`
- `https_portal_ssl`

---

## Backend Services

Per-service metrics for backend server health.

### Service Health & State

| Metric Name | Unit | Description | Tags |
|-------------|------|-------------|------|
| `netscaler.service.state` | # | Service operational state | `service` |

**PRTG Lookup:** `netscaler.service.state` (same values as vServer state)

**Alerting:**
- 🚨 CRITICAL: state = 1 (DOWN - backend unreachable)
- ⚠️ WARNING: state = 2 (UNKNOWN)
- ⚠️ WARNING: state = 3 (BUSY)

### Service Performance

| Metric Name | Unit | Description | Tags |
|-------------|------|-------------|------|
| `netscaler.service.throughput.bytes_per_sec` | bytes/s | Service throughput | `service` |
| `netscaler.service.transactions.active` | # | Active transactions | `service` |

**Use Cases:**
- Identify slow or failed backend servers
- Detect service degradation before total failure
- Load distribution analysis across backends
- Performance tuning (identify bottlenecks)

**Example Service Names:**
- `webserver01_http`
- `api-backend-1_443`
- `database-lb-service`

---

## Service Groups

Per-service-group metrics for pool health and member status.

### Service Group Health

| Metric Name | Unit | Description | Tags |
|-------------|------|-------------|------|
| `netscaler.servicegroup.state` | # | Service group operational state | `servicegroup` |

**PRTG Lookup:** `netscaler.servicegroup.state`

### Service Group Performance

| Metric Name | Unit | Description | Tags |
|-------------|------|-------------|------|
| `netscaler.servicegroup.requests.rate` | req/s | Requests per second | `servicegroup` |
| `netscaler.servicegroup.members.active` | # | Number of active members | `servicegroup` |

**Use Cases:**
- Monitor pool member availability
- Detect partial outages (some members down)
- Capacity planning (active members vs total members)
- Auto-scaling validation (members joining/leaving)

**Alerting:**
- 🚨 CRITICAL: members.active = 0 (entire pool down)
- ⚠️ WARNING: members.active < 50% of total (degraded redundancy)

**Example Service Group Names:**
- `webserver-pool`
- `api-backends`
- `database-cluster`

---

## High Availability (HA)

Per-node metrics for HA cluster monitoring.

### HA Architecture

- **Dual-Node Clusters**: Primary + Secondary configuration
- **Metrics per Node**: Collected for BOTH nodes (local + remote)
- **Node Identification**: By IP address and node ID

**Tags:**
- `ha_node_id`: Node ID (0 or 1)
- `ha_node_ip`: Node IP address (e.g., "10.0.208.7")
- `is_local_node`: "true" for connected node, "false" for remote
- `connected_to`: Hostname of connected NetScaler

### HA Node Role & State

| Metric Name | Unit | Description | Tags |
|-------------|------|-------------|------|
| `netscaler.ha.state` | # | Node HA role | `ha_node_id`, `ha_node_ip` |
| `netscaler.ha.node.state` | # | Node operational state | `ha_node_id`, `ha_node_ip` |

**PRTG Lookups:**

**`netscaler.ha.state`** (HA Role):
- **2** = PRIMARY (active node)
- **1** = SECONDARY (standby node)
- **0** = UNKNOWN (HA broken or misconfigured)

**`netscaler.ha.node.state`** (Operational State):
- **1** = UP (node is operational)
- **0** = DOWN (node is unreachable or failed)

**Alerting:**
- 🚨 CRITICAL: ha.state = 0 (HA cluster broken)
- 🚨 CRITICAL: ha.node.state = 0 (node failure)
- 📊 INFO: Both nodes PRIMARY (split-brain scenario!)

### HA Synchronization

| Metric Name | Unit | Description | Tags |
|-------------|------|-------------|------|
| `netscaler.ha.sync_status` | # | Config sync status | `ha_node_id`, `ha_node_ip` |
| `netscaler.ha.sync_failures` | # | Sync failure counter | `ha_node_id`, `ha_node_ip` |

**PRTG Lookup: `netscaler.ha.sync_status`**
- **1** = SUCCESS (configuration synchronized)
- **0** = FAILED (sync failure - investigate!)

**Alerting:**
- 🚨 CRITICAL: sync_status = 0 (sync failed)
- ⚠️ WARNING: sync_failures > 0 (transient issues)
- 📊 INFO: sync_failures increasing (degraded HA)

### HA Cluster Metrics (Global)

| Metric Name | Unit | Description |
|-------------|------|-------------|
| `netscaler.ha.propagation_timeouts` | # | Config propagation timeouts |
| `netscaler.ha.heartbeat.rx.packets` | # | Total heartbeat packets received |
| `netscaler.ha.heartbeat.rx.rate` | pkts/s | Heartbeat receive rate |
| `netscaler.ha.heartbeat.tx.packets` | # | Total heartbeat packets transmitted |
| `netscaler.ha.heartbeat.tx.rate` | pkts/s | Heartbeat transmit rate |

**Use Cases:**
- Monitor HA heartbeat health (early warning for network issues)
- Detect split-brain scenarios (both nodes PRIMARY)
- Verify failover readiness
- Track configuration drift

---

## Disk Usage

Per-partition disk space monitoring.

| Metric Name | Unit | Description | Tags |
|-------------|------|-------------|------|
| `netscaler.disk.percent_used` | % | Disk usage percentage | `partition` |
| `netscaler.disk.used_kb` | KB | Used disk space | `partition` |
| `netscaler.disk.available_kb` | KB | Available disk space | `partition` |

**Common Partitions:**
- `/var/log` - Log files (CRITICAL: fills up quickly)
- `/var` - Variable data
- `/flash` - Firmware/config storage

**Alerting Recommendations:**
- ⚠️ WARNING: percent_used > 80%
- 🚨 CRITICAL: percent_used > 90%
- 💥 URGENT: percent_used > 95% (NetScaler may stop logging!)

**Why Critical:**
- Full `/var/log` causes silent failures (no logs written)
- NetScaler may become unresponsive or fail health checks
- Cannot troubleshoot issues without logs

---

## Phase 2 Metrics (Planned)

### Network Interfaces (Per-Interface)

- `netscaler.interface.state` - Interface state (UP/DOWN)
- `netscaler.interface.rx.bytes.total` - Total bytes received
- `netscaler.interface.tx.bytes.total` - Total bytes transmitted
- `netscaler.interface.rx.mbits_per_sec` - Receive rate
- `netscaler.interface.tx.mbits_per_sec` - Transmit rate
- `netscaler.interface.rx.errors.total` - RX error counter
- `netscaler.interface.tx.errors.total` - TX error counter
- `netscaler.interface.rx.drops.total` - RX packet drops
- `netscaler.interface.tx.drops.total` - TX packet drops
- `netscaler.interface.link_speed_mbps` - Link speed

**Tags:** `interface` (e.g., "LO/1", "LA/1")

### Content Switching (CS)

- `netscaler.cs.vserver.state` - CS vServer state
- `netscaler.cs.vserver.hits.total` - Total hits
- `netscaler.cs.vserver.requests.rate` - Requests per second
- `netscaler.cs.vserver.connections.current` - Active connections
- `netscaler.cs.policy.hits.total` - CS policy hits
- `netscaler.cs.policy.undefine_hits.total` - Unmatched requests

**Tags:** `csvserver`, `cspolicy`

### GSLB (Global Server Load Balancing)

- `netscaler.gslb.vserver.state` - GSLB vServer state
- `netscaler.gslb.vserver.hits.total` - Total hits
- `netscaler.gslb.vserver.requests.rate` - Requests per second
- `netscaler.gslb.site.state` - GSLB site state (UP/DOWN)
- `netscaler.gslb.site.network_rtt_microseconds` - RTT to remote site
- `netscaler.gslb.service.state` - GSLB service state
- `netscaler.gslb.service.hits.total` - Service hits

**Tags:** `gslbvserver`, `gslbsite`, `gslbservice`

### Advanced Load Balancer Metrics

- `netscaler.lbvserver.spillovers.total` - Spillovers to backup vServer
- `netscaler.lbvserver.connections.established` - Established connections
- `netscaler.lbvserver.hits.total` - Total hits (request distribution)
- `netscaler.service.surge_queue_length` - Surge queue (backend saturation)
- `netscaler.servicegroup.members.inactive` - Inactive members count
- `netscaler.servicegroup.surge_queue_length` - Service group surge queue

---

## Phase 3 Metrics (Planned)

### AAA & Authentication

- `netscaler.aaa.sessions.active.total` - Active AAA sessions
- `netscaler.aaa.vserver.state` - Auth vServer state
- `netscaler.aaa.vserver.auth.successes.total` - Successful authentications
- `netscaler.aaa.vserver.auth.failures.total` - Failed authentications

**Tags:** `authvserver`

### VPN/Gateway

- `netscaler.vpn.vserver.state` - VPN vServer state
- `netscaler.vpn.vserver.hits.total` - Total hits
- `netscaler.vpn.vserver.ica.sessions.active` - Active ICA sessions (Citrix Virtual Apps)
- `netscaler.vpn.vserver.connections.established` - Established VPN connections

**Tags:** `vpnvserver`

### Application Firewall (AppFW)

- `netscaler.appfw.violations.total` - Total WAF violations
- `netscaler.appfw.requests.blocked.total` - Requests blocked
- `netscaler.appfw.responses.blocked.total` - Responses blocked
- `netscaler.appfw.violations.sqli.total` - SQL injection violations
- `netscaler.appfw.violations.xss.total` - XSS violations
- `netscaler.appfw.violations.buffer_overflow.total` - Buffer overflow violations

### Cache & Compression

- `netscaler.cache.hit_ratio_percent` - Cache hit ratio
- `netscaler.cache.objects.count` - Cached objects count
- `netscaler.cache.memory.used_kb` - Cache memory usage
- `netscaler.compression.ratio` - Compression ratio
- `netscaler.compression.bandwidth_savings.bytes` - Bandwidth saved

---

## Metric Naming Convention

### Structure

```
netscaler.{category}.{subcategory}.{metric_name}[.unit]
```

**Examples:**
- `netscaler.system.cpu.usage.percent`
- `netscaler.lbvserver.requests.rate`
- `netscaler.ha.sync_failures`

### Tags (Multi-Instance Metrics)

Metrics with multiple instances include discriminant tags:

- **vServer metrics**: `vserver` tag (e.g., `vserver=http_webservers_80`)
- **Service metrics**: `service` tag (e.g., `service=webserver01_http`)
- **Service Group metrics**: `servicegroup` tag
- **SSL Certificates**: `certname` tag
- **Disk partitions**: `partition` tag
- **HA nodes**: `ha_node_id`, `ha_node_ip` tags
- **Network interfaces** (Phase 2): `interface` tag

### PRTG Channel Names

**With ShowTags=true** (default):
```
SSL Certificate Days to Expiration (wildcard_prod_2025)
VServer State (http_webservers_80)
HA Sync Status (10.0.208.7)
```

**With ShowTags=false** (filtered view):
```
SSL Certificate Days to Expiration
VServer State
HA Sync Status
```

Use `show_tags=false` parameter in API Explorer to get cleaner channel names when filtering by specific tags.

---

## Units Reference

| Unit | Meaning | Example Metrics |
|------|---------|-----------------|
| `%` | Percentage | CPU usage, memory usage, disk usage |
| `Mbits/s` | Megabits per second | Network throughput |
| `bytes/s` | Bytes per second | Service throughput |
| `req/s` | Requests per second | HTTP request rate |
| `tx/s` | Transactions per second | SSL transactions |
| `pkts/s` | Packets per second | HA heartbeat |
| `#` | Count/Integer | Connections, sessions, failures |
| `days` | Days | Certificate expiration |
| `KB` | Kilobytes | Disk space |

---

## References

- [NetScaler NITRO API Documentation](https://developer-docs.netscaler.com/)
- [Citrix ADC Monitoring Best Practices](https://docs.citrix.com/en-us/citrix-adc/)
- [Configuration Examples](./configuration-examples.yaml)
- [Main Documentation](./README.md)
