# NetScaler ADC Probe

Monitor Citrix NetScaler (ADC) infrastructure using the NITRO REST API.

## Overview

The NetScaler probe collects performance, health, and configuration metrics from Citrix NetScaler Application Delivery Controllers. It provides comprehensive visibility into load balancing, SSL offloading, high availability, and system health.

**Key Features:**
- Load Balancer Virtual Servers (LB vServers) monitoring
- Backend Services and Service Groups health tracking
- SSL Certificate expiration monitoring
- High Availability (HA) cluster state and synchronization
- System resource utilization (CPU, memory, network, disk)
- SSL/TLS transaction metrics
- GSLB, Content Switching, and VPN monitoring (Phase 2+)

**Supported Versions:**
- Citrix NetScaler 11.x, 12.x, 13.x
- Citrix ADC 13.x, 14.x

**API:** NITRO REST API (HTTP/HTTPS)

## Quick Start

### Minimal Configuration

```yaml
probes:
  - name: netscaler-prod
    type: netscaler
    params:
      base_url: "https://netscaler.company.com"
      username: "monitoring-user"
      password: "secure-password"
      interval: 60
```

### Recommended Configuration

```yaml
probes:
  - name: netscaler-prod
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
      - key: "team"
        value: "infrastructure"
```

## Configuration Parameters

| Parameter | Type | Required | Default | Description |
|-----------|------|----------|---------|-------------|
| `base_url` | string | Yes | - | NetScaler management URL (primary node IP recommended) |
| `secondary_url` | string | No | - | Secondary NetScaler URL for HA failover |
| `username` | string | Yes | - | NITRO API username |
| `password` | string | Yes | - | NITRO API password |
| `insecure_skip_verify` | boolean | No | `false` | Skip SSL certificate verification (set `true` when using IPs) |
| `timeout` | integer | No | `30` | API request timeout in seconds |
| `interval` | integer | No | `60` | Metric collection interval in seconds |
| `custom_tags` | array | No | `[]` | Additional tags to attach to all metrics |

## Authentication

### NITRO API User

Create a dedicated monitoring user with read-only permissions:

```bash
# NetScaler CLI
add system user monitoring-user "SecurePassword123!" -timeout 900
bind system user monitoring-user read-only
```

### LDAP/AD Authentication

If using external authentication, ensure the monitoring user has:
- **Permissions**: Read-only access (Command Policy: read-only)
- **Session Timeout**: 900 seconds minimum (API queries can take time)

## Metrics Overview

The NetScaler probe collects **33 metrics** across **13 resource types** in Phase 1:

### Critical Metrics (Priority 1)

| Metric Category | Count | Purpose |
|-----------------|-------|---------|
| **SSL Certificates** | 2 | Prevent outages from expired certificates |
| **High Availability** | 9 | Detect HA cluster issues, sync failures |
| **Disk Usage** | 3 per partition | Monitor disk space (prevents silent failures) |

### Performance Metrics

| Metric Category | Count | Purpose |
|-----------------|-------|---------|
| **System Resources** | 9 | CPU, memory, network throughput |
| **Load Balancers** | 5+ per vServer | vServer health, request rates, connections |
| **Services** | 3+ per service | Backend service health and throughput |
| **Service Groups** | 3+ per group | Service group status and active members |
| **SSL/TLS** | 2 | Global SSL transaction metrics |

See [METRICS.md](./METRICS.md) for the complete metrics reference.

## High Availability (HA) Monitoring

The NetScaler probe fully supports **High Availability clusters**:

**Architecture:**
- Connects to one node (preferably primary via `base_url`)
- Automatic failover to `secondary_url` after 3 consecutive errors
- Proactive switch: if connected to secondary, auto-switches to primary
- Collects metrics for BOTH nodes (local via stats, remote via config)
- Identifies nodes by IP address, hostname, and node ID

**Per-Node Metrics:**
- `ha.state`: Node role (PRIMARY/SECONDARY/UNKNOWN)
- `ha.node.state`: Operational state (UP/DOWN)
- `ha.sync_status`: Configuration sync status (SUCCESS/FAILED)
- `ha.sync_failures`: Sync failure counter

**Tags:**
- `ha_node_id`: Node ID (0 or 1)
- `ha_node_ip`: Node IP address (e.g., "10.0.208.7")
- `is_local_node`: "true" for connected node, "false" for remote
- `connected_to`: Hostname of connected node

**Recommended Alerting:**
```
⚠️ WARNING: HA sync_status = 0 (FAILED)
🚨 CRITICAL: HA state = 0 (UNKNOWN - HA broken)
📊 INFO: HA sync_failures > 0 (degraded HA, investigate)
```

## SSL Certificate Monitoring

**Purpose:** Prevent production outages from expired SSL certificates.

**Metrics:**
- `netscaler.ssl.certificate.days_to_expiration`: Days until certificate expires
- `netscaler.ssl.certificate.status`: 1=valid, 0=expired/invalid

**Tags:**
- `certname`: Certificate name (e.g., "wildcard_prod_2025")

**Recommended Alerting:**
```
📅 30 days: WARNING (start renewal process)
⚠️ 15 days: HIGH (renewal urgent)
🚨 7 days: CRITICAL (imminent expiration)
💥 0 days: status=0 EXPIRED (OUTAGE!)
```

## PRTG Integration

### Value Lookups

The NetScaler probe includes **PRTG Value Lookups** for human-readable status values:

**Available Lookups:**
- `netscaler.lbvserver.state`: UP, DOWN, OUT OF SERVICE, BUSY, UNKNOWN
- `netscaler.service.state`: UP, DOWN, OUT OF SERVICE, BUSY, UNKNOWN
- `netscaler.servicegroup.state`: UP, DOWN, OUT OF SERVICE, BUSY, UNKNOWN
- `netscaler.interface.state`: ENABLED, DISABLED
- `netscaler.ssl.certificate.status`: VALID, INVALID
- `netscaler.ha.state`: PRIMARY, SECONDARY, UNKNOWN
- `netscaler.ha.node.state`: UP, DOWN
- `netscaler.ha.sync_status`: SUCCESS, FAILED

**Download Lookups:**
1. Open SenHub Agent dashboard: `http://localhost:8080/web/{agentkey}/`
2. Navigate to **API Explorer**
3. Click **"Download PRTG Lookups"** button
4. Extract `.ovl` files to PRTG Server: `C:\Program Files (x86)\PRTG Network Monitor\lookups\custom\`
5. Refresh PRTG lookup files (Administration > System Administration > Administrative Tools)

See [PRTG Integration Guide](../../admin-guide/HTTP-STRATEGY.md#prtg-lookups) for details.

## Performance Tuning

### Collection Interval

**Recommended intervals:**
- **Production (critical)**: 60 seconds (default)
- **Development/Test**: 120-300 seconds
- **High-frequency (troubleshooting)**: 30 seconds (short-term only)

**Impact:**
- Lower interval = more API calls = higher NetScaler CPU usage
- Default 60s provides good balance for most environments

### API Timeout

**Default:** 30 seconds

**Increase timeout if:**
- NetScaler has 100+ vServers/services
- Slow network connectivity
- Seeing timeout errors in logs

```yaml
params:
  timeout: 60  # Increase for large deployments
```

### Cache Refresh

The probe caches NetScaler configuration (vServers, services, certificates) to avoid excessive API calls.

**Cache TTL:** 5 minutes (automatic refresh)

**Cached Data:**
- Virtual Server configurations
- Service configurations
- Service Group bindings
- SSL Certificate bindings

This reduces API calls by ~80% while keeping enrichment tags accurate.

## Troubleshooting

### Common Issues

#### 1. Authentication Failed

**Error:** `Failed to authenticate with Netscaler`

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

#### 2. SSL Certificate Verification Failed

**Error:** `Failed to create NITRO client: x509: certificate signed by unknown authority`

**Solutions:**
- Option 1 (Recommended): Install NetScaler CA certificate on agent host
- Option 2 (Not recommended): Set `insecure_skip_verify: true` (testing only)

#### 3. Timeout Errors

**Error:** `Context deadline exceeded` or `Timeout waiting for NITRO response`

**Solutions:**
- Increase `timeout` parameter (try 60 seconds)
- Check network latency to NetScaler
- Verify NetScaler CPU usage is not maxed out
- Reduce `interval` to reduce query frequency

#### 4. HA Metrics Missing

**Error:** No HA metrics collected

**Possible causes:**
- NetScaler is not configured for High Availability (standalone mode)
- This is expected behavior for standalone deployments
- Check NetScaler: `show ha node` (should return 0 or 2 nodes)

### Debug Logging

Enable debug logging for NetScaler probe:

```bash
# Runtime log level change (via HTTP API)
curl -X POST http://localhost:8080/api/{agentkey}/debug/logs \
  -H "Content-Type: application/json" \
  -d '{"module_levels": [{"module": "probe.netscaler", "level": "debug"}]}'
```

Or start agent with verbose logging:

```bash
./agent run --verbose --debug-modules probe.netscaler
```

**Debug output includes:**
- API request/response details
- Cache refresh operations
- Metric collection timings
- HA node detection logic

## License Requirements

The NetScaler probe requires a **Pro** or **Enterprise** license.

**License Tiers:**
- ❌ Free: NetScaler probe not available
- ✅ Pro: NetScaler probe included
- ✅ Enterprise: NetScaler probe included

See [License System Documentation](../../LICENSE-SYSTEM.md) for license management.

## Examples

See [configuration-examples.yaml](./configuration-examples.yaml) for complete configuration examples including:
- Basic monitoring (minimal config)
- Production HA cluster monitoring
- Multi-datacenter monitoring with custom tags
- Advanced tuning for large deployments

## Roadmap

### Phase 1 (✅ Complete)
- System metrics (CPU, memory, network, disk)
- Load Balancer vServers, Services, Service Groups
- SSL Certificates
- High Availability
- Global SSL/TLS metrics

### Phase 2 (Planned)
- Network interfaces (detailed per-interface metrics)
- Content Switching (CS vServers, policies)
- GSLB (Global Server Load Balancing)
- Advanced SSL metrics (cipher suites, protocol distribution)
- Cache metrics (hit ratios, object counts)

### Phase 3 (Planned)
- AAA (Authentication, Authorization, Accounting)
- VPN/Gateway metrics
- Application Firewall (AppFW) security events
- Compression metrics

See [internal roadmap](../../.internal/NETSCALER-ROADMAP.md) for detailed phase planning.

## References

- [NetScaler NITRO API Documentation](https://developer-docs.netscaler.com/en-us/netscaler-command-reference/current-release/)
- [Citrix ADC Product Documentation](https://docs.citrix.com/en-us/citrix-adc/)
- [Complete Metrics Reference](./METRICS.md)
- [Configuration Examples](./configuration-examples.yaml)
- [PRTG Integration Guide](../../admin-guide/HTTP-STRATEGY.md)

## Support

- **Issues**: Report bugs or request features via [GitHub Issues](https://github.com/senhub-io/senhub-agent/issues)
- **Commercial Support**: Contact SenHub for enterprise support contracts
