---
title: "Redfish"
weight: 7
---

{{< hint warning >}}
**License: Pro** - Requires a Pro or Enterprise license. See [License Tiers]({{< relref "/docs/configuration#license-tiers" >}}).
{{< /hint >}}

# Overview

The Redfish probe monitors server and storage hardware through the DMTF Redfish API, providing comprehensive health, capacity, and performance metrics. It supports a wide range of hardware platforms including Dell iDRAC, Dell PowerVault ME series, HPE iLO, Lenovo XClarity, and Cisco UCS.

The probe automatically detects the hardware vendor via the Redfish API and adapts its metric collection accordingly. No vendor-specific configuration is required.

**Collected Data:**
- Controller, drive, pool, and volume health status
- Storage capacity (total, allocated, used, free) for pools and volumes
- I/O performance metrics (reads, writes, latency, throughput)
- Hardware event logs (critical, warning, informational entries)
- Drive failure predictions and hotspare status
- Encryption status for volumes and drives

**Supported Hardware:**
- Dell iDRAC (PowerEdge servers)
- Dell PowerVault ME series (ME5024, etc.)
- HPE iLO (ProLiant servers)
- Lenovo XClarity (ThinkSystem servers)
- Cisco UCS (Unified Computing System)
- Any hardware implementing the DMTF Redfish standard

# Quick Start

## Basic Configuration

```yaml
probes:
  - name: "Hardware Server01"
    type: redfish
    params:
      base_url: "https://idrac-server01.company.com"
      username: "monitoring"
      password: "SecurePassword123"
      interval: 300
      tls:
        verify_ssl: false
```

**Important notes:**
- `base_url`: The Redfish API endpoint (iDRAC, iLO, or BMC management address)
- `interval`: 300 seconds (5 minutes) is recommended for hardware monitoring
- `verify_ssl`: Set to `false` for self-signed certificates commonly used on BMC interfaces

## Multiple Servers

Monitor multiple hardware targets with separate probe instances:

```yaml
probes:
  - name: "Dell Storage ME5024"
    type: redfish
    params:
      base_url: "https://dell-me5024.company.com"
      username: "admin"
      password: "StoragePassword"
      interval: 300
      tls:
        verify_ssl: false

  - name: "HPE ProLiant DL380"
    type: redfish
    params:
      base_url: "https://ilo-dl380.company.com"
      username: "monitoring"
      password: "ServerPassword"
      interval: 300
      tls:
        verify_ssl: false
```

# Metrics Collected

## Health Metrics

Monitor the health status of hardware components. Health values use a standard scale: 0=OK, 1=Warning, 2=Critical, 3=Unknown.

### Storage Controllers

| Metric Name | Description | Type |
|------------|-------------|------|
| `hardware.storage.controller.health` | Controller health status | Gauge |
| `hardware.storage.redundancy.health` | Controller redundancy health | Gauge |
| `hardware.storage.redundancy.controllers_active` | Number of active controllers | Gauge |
| `hardware.storage.redundancy.controllers_min` | Minimum controllers required | Gauge |
| `hardware.storage.redundancy.controllers_max` | Maximum controllers supported | Gauge |

### Drives

| Metric Name | Description | Type |
|------------|-------------|------|
| `hardware.storage.drive.health` | Drive health status | Gauge |
| `hardware.storage.drive.failure_predicted` | Failure prediction (1=failure predicted) | Gauge |
| `hardware.storage.drive.hotspare` | Hotspare status (1=active hotspare) | Gauge |

### Storage Pools

| Metric Name | Description | Type |
|------------|-------------|------|
| `hardware.storage.pool.health` | Pool health status | Gauge |

### Volumes

| Metric Name | Description | Type |
|------------|-------------|------|
| `hardware.storage.volume.health` | Volume health status | Gauge |
| `hardware.storage.volume.encrypted` | Encryption status (1=encrypted) | Gauge |

### Events and Logs

| Metric Name | Description | Type |
|------------|-------------|------|
| `hardware.logs.entries.total` | Total log entries | Gauge |
| `hardware.logs.entries.critical` | Critical log entries | Gauge |
| `hardware.logs.entries.warning` | Warning log entries | Gauge |
| `hardware.logs.entries.info` | Informational log entries | Gauge |
| `hardware.logs.entries.last_24h` | Events in the last 24 hours | Gauge |
| `hardware.logs.entries.last_7d` | Events in the last 7 days | Gauge |
| `hardware.eventservice.health` | Event service health status | Gauge |
| `hardware.eventservice.subscriptions` | Number of event subscriptions | Gauge |

## Capacity Metrics

### Storage Pools

| Metric Name | Description | Unit |
|------------|-------------|------|
| `hardware.storage.pool.capacity.total` | Total pool capacity | bytes |
| `hardware.storage.pool.capacity.allocated` | Allocated space in pool | bytes |
| `hardware.storage.pool.capacity.allocated_percent` | Allocated space percentage | % |
| `hardware.storage.pool.capacity.used` | Actually consumed space | bytes |
| `hardware.storage.pool.capacity.used_percent` | Consumed space percentage | % |
| `hardware.storage.pool.capacity.free` | Free space | bytes |
| `hardware.storage.pool.capacity.free_percent` | Free space percentage | % |
| `hardware.storage.pool.capacity.volumes` | Space allocated to volumes | bytes |
| `hardware.storage.pool.capacity.snapshots` | Space allocated to snapshots | bytes |
| `hardware.storage.pool.capacity.committed` | Total committed space | bytes |
| `hardware.storage.pool.capacity.overcommit` | Over-allocated space (thin provisioning) | bytes |

### Volumes

| Metric Name | Description | Unit |
|------------|-------------|------|
| `hardware.storage.volume.capacity.total` | Total volume capacity | bytes |
| `hardware.storage.volume.capacity.allocated` | Allocated space | bytes |
| `hardware.storage.volume.capacity.allocated_percent` | Allocated space percentage | % |
| `hardware.storage.volume.capacity.used` | Actually used space | bytes |
| `hardware.storage.volume.capacity.used_percent` | Used space percentage | % |
| `hardware.storage.volume.capacity.free` | Free space | bytes |
| `hardware.storage.volume.capacity.free_percent` | Free space percentage | % |

### Drives

| Metric Name | Description | Unit |
|------------|-------------|------|
| `hardware.storage.drive.capacity.total` | Total drive capacity | bytes |

## Performance Metrics

### Volume I/O

| Metric Name | Description | Unit |
|------------|-------------|------|
| `hardware.storage.volume.io.total_ops` | Total I/O operations | count |
| `hardware.storage.volume.io.reads` | Read operations | count |
| `hardware.storage.volume.io.writes` | Write operations | count |
| `hardware.storage.volume.io.total_bytes` | Total data transferred | bytes |
| `hardware.storage.volume.io.read.bytes` | Data read | bytes |
| `hardware.storage.volume.io.write.bytes` | Data written | bytes |
| `hardware.storage.volume.io.read.latency` | Read latency | ms |
| `hardware.storage.volume.io.write.latency` | Write latency | ms |

### Pool I/O

| Metric Name | Description | Unit |
|------------|-------------|------|
| `hardware.storage.pool.io.reads` | Read operations | count |
| `hardware.storage.pool.io.writes` | Write operations | count |
| `hardware.storage.pool.io.read.bytes` | Data read | bytes |
| `hardware.storage.pool.io.write.bytes` | Data written | bytes |

## Operations Metrics

| Metric Name | Description | Type |
|------------|-------------|------|
| `hardware.storage.drive.has_operations` | Operations in progress (1=yes) | Gauge |
| `hardware.storage.drive.operation.progress` | Operation progress | % |

# Tags

All metrics include contextual tags for filtering and grouping.

## Controller Tags

| Tag | Description | Example |
|-----|-------------|---------|
| `controller_id` | Controller identifier | `A` |
| `controller_name` | Controller name | `Controller A` |
| `controller` | Controller letter | `A`, `B` |
| `controller_type` | Controller type | `storage` |
| `host` | Host system name | `me5024-prod` |
| `manufacturer` | Controller manufacturer | `Dell` |
| `model` | Controller model | `PERC H740P` |
| `serial_number` | Controller serial number | `ABC123` |

## Drive Tags

| Tag | Description | Example |
|-----|-------------|---------|
| `drive_id` | Drive identifier | `Disk.Bay.0:Enclosure.Internal.0-1` |
| `drive_name` | Drive name | `Disk 0` |
| `model` | Drive model | `ST1200MM0009` |
| `drive_manufacturer` | Drive manufacturer | `Seagate` |
| `serial_number` | Drive serial number | `WFK12345` |
| `media_type` | Media type | `SSD`, `HDD` |
| `protocol` | Communication protocol | `SAS`, `SATA` |
| `hotspare_type` | Hotspare type | `Global`, `Dedicated` |
| `encryption_ability` | Encryption capability | `SelfEncryptingDrive` |
| `encryption_status` | Encryption status | `Unlocked` |
| `service_label` | Service label | `Bay 0` |
| `location_type` | Location type | `Slot` |
| `location_ordinal` | Location ordinal value | `0` |
| `operation_name` | Current operation name | `Rebuild` |

## Pool Tags

| Tag | Description | Example |
|-----|-------------|---------|
| `pool_id` | Pool identifier | `A` |
| `pool_name` | Pool name | `Pool A` |
| `description` | Pool description | `Virtual storage pool` |
| `supported_raid_types` | Supported RAID types | `RAID1, RAID5, RAID6` |
| `max_block_size_bytes` | Maximum block size | `512` |
| `thin_provisioned` | Thin provisioning indicator | `true` |

## Volume Tags

| Tag | Description | Example |
|-----|-------------|---------|
| `volume_id` | Volume identifier | `VD1` |
| `volume_name` | Volume name | `Production-Vol1` |
| `pool_id` | Associated pool identifier | `A` |
| `raid_type` | RAID type | `RAID5` |
| `write_cache_policy` | Write cache policy | `WriteBack` |
| `block_size_bytes` | Block size | `512` |
| `access_capabilities` | Access capabilities | `Read, Write` |
| `encryption_type` | Encryption type | `NativeDriveEncryption` |

## Event and Log Tags

| Tag | Description | Example |
|-----|-------------|---------|
| `host` | Host system name | `me5024-prod` |
| `manager_id` | Manager identifier | `BMC` |
| `manager_name` | Manager name | `iDRAC` |
| `model` | Manager model | `iDRAC9` |
| `log_service_id` | Log service identifier | `Sel` |
| `log_service_name` | Log service name | `System Event Log` |

# Recommended Alerting

## Essential Health Alerts

- Monitor `hardware.storage.controller.health` for controller failures
- Monitor `hardware.storage.redundancy.health` for redundancy issues
- Monitor `hardware.storage.drive.failure_predicted` for drives with predicted failures
- Monitor `hardware.storage.drive.has_operations` for ongoing maintenance operations
- Monitor `hardware.logs.entries.critical` for critical system events

## Capacity Alerts

- Monitor `hardware.storage.pool.capacity.free_percent` for available space
- Monitor `hardware.storage.volume.capacity.used_percent` for volume utilization

## Performance Alerts

- Monitor `hardware.storage.volume.io.total_ops` for general I/O activity
- Monitor `hardware.storage.volume.io.read.latency` and `hardware.storage.volume.io.write.latency` for performance issues

## Event Monitoring

- Monitor `hardware.logs.entries.critical` and `hardware.logs.entries.warning` for system issues
- Use `hardware.logs.entries.last_24h` to track recent system activity
- Compare trends between `hardware.logs.entries.last_24h` and `hardware.logs.entries.last_7d` to identify event spikes
- Use `hardware.eventservice.health` to verify the event service is operating correctly

# Troubleshooting

## Connection Issues

**Symptom:** Cannot connect to the Redfish endpoint

**Diagnosis:**
1. Verify the BMC/iDRAC/iLO management interface is reachable from the agent host:
   ```bash
   curl -k https://idrac-server01.company.com/redfish/v1/
   ```
2. Verify the management interface is powered on and network-connected
3. Check for firewall rules blocking HTTPS (port 443) between the agent and the BMC

## TLS/SSL Errors

**Symptom:** TLS handshake failures or certificate errors

**Resolution:**
BMC management interfaces typically use self-signed certificates. Set `verify_ssl: false` in the probe configuration:

```yaml
params:
  tls:
    verify_ssl: false
```

If your environment uses properly signed certificates, ensure the CA chain is trusted by the system running the agent.

## Authentication Failures

**Symptom:** 401 Unauthorized or login failures

**Diagnosis:**
1. Verify credentials by logging in to the BMC web interface manually
2. Check that the account is not locked out due to failed login attempts
3. Verify the account has sufficient privileges for Redfish API access (read-only access is sufficient)
4. Some BMCs limit concurrent sessions -- ensure the session limit is not reached

## Dell ME Capacity Shows Zero

**Symptom:** Pool or volume capacity metrics return 0

**Explanation:** Dell PowerVault ME series systems may return `CapacityBytes=0` in the standard Redfish response. The agent automatically detects this and uses `Capacity.Data.AllocatedBytes` as the effective capacity. Ensure you are running a recent version of the agent for this workaround to be active.

## Debug Logging

Enable debug logging for the Redfish probe:

```bash
# Runtime log level change
curl -X POST http://localhost:8080/api/{key}/debug/logs \
  -H "Content-Type: application/json" \
  -d '{"module_levels": [{"module": "probe.redfish", "level": "debug"}]}'

# Or start agent with verbose logging
./senhub-agent run --authentication-key KEY --verbose --debug-modules probe.redfish
```

## License Requirements

The Redfish probe requires a **Pro** or **Enterprise** license.

| Tier | Redfish Probe |
|------|--------------|
| Free | Not available |
| Pro | Included |
| Enterprise | Included |
