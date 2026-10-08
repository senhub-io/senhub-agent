<img src="../../assets/probe-logos/redfish.svg" alt="" class="probe-page-logo probe-page-logo-si">

!!! warning
    **License: Pro** - Requires a Pro or Enterprise license.

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
# probes.d/10-redfish.yaml — each file under probes.d/ is a YAML array of probes
- name: "hardware-server01"
  type: redfish
  params:
    endpoint: "https://idrac-server01.company.com"
    username: "monitoring"
    password: ${secret:hardware-server01.password}   # OS secret store; inline plaintext is auto-sealed on install
    interval: 300
    verify_ssl: false
```

**Important notes:**
- `endpoint`: The Redfish API endpoint (iDRAC, iLO, or BMC management address). The probe refuses to start without it
- `interval`: 300 seconds is a good starting point for hardware monitoring. Do
  not go above it if you read this probe through PRTG — see
  [Interval and the PRTG TTL](#interval-and-the-prtg-ttl).
- `verify_ssl`: Set to `false` for self-signed certificates commonly used on BMC interfaces

## Choosing what to collect (`collections`)

The probe walks several independent subsystems of the BMC. By default it
collects six of them:

```yaml
collections: ["system", "thermal", "power", "processor", "memory", "storage"]
```

You do not need this key at all to get those six. Add it only to collect
something outside the default set, or to deliberately collect less.

!!! warning "`collections` replaces the defaults, it does not filter them"

    The moment this key is present, the six defaults are **discarded** and
    only what you list is collected. It is a replacement, not a filter.
    This is deliberate: it is the only way to collect less, which is what
    you want against a slow BMC.

    So this configuration does **not** mean "everything except storage":

    ```yaml
    collections: ["system", "power"]     # thermal, processor, memory
                                         # and storage are ALL off
    ```

    It means the probe collects system and power, and nothing else. The
    sensor still reports OK, with far fewer channels than before, every
    one of them healthy. The agent now says so when the probe starts, at
    Info level:

    ```text
    redfish: 'collections' replaces the default set; these default
    subsystems are NOT collected  collections=[system power]
    disabled_defaults=[thermal processor memory storage]
    ```

    If you are turning one subsystem off, write out every subsystem you
    still want:

    ```yaml
    collections: ["system", "thermal", "power", "processor", "memory"]
    ```

    A configuration that lists all six defaults behaves exactly as if the
    key were absent.

The list is checked when the probe loads. An unknown name, a value that
is not a string, or an empty list stops the probe with an error that
names the accepted values; names are case-insensitive and a repeated name
is ignored. The `Redfish probe initialized` line logged at start carries
the final `collections` list, and a collection the detected BMC cannot
serve (for example `drives` on a generic BMC) is logged as a warning
instead of yielding nothing quietly.

### Accepted values

| Value | Collects | In the defaults |
|---|---|---|
| `system` | Overall system health, power state | Yes |
| `thermal` | Temperature sensors, fans | Yes |
| `power` | PSU health, power supplies, consumption | Yes |
| `processor` | CPU health, speed, temperature, utilisation | Yes |
| `memory` | DIMM health, capacity, speed, ECC errors | Yes |
| `storage` | Controllers, drives, volumes, pools | Yes |
| `drives` | Physical drives on their own; Dell, HPE, Cisco, Lenovo and storage-system collectors only (a generic BMC logs a warning) | **No** |
| `network` | Accepted, but no collector serves it today (logged as a warning at start); use `networkadapter` | **No** |
| `networkadapter` | Network adapter detail | **No** |

The last three are **never collected unless you list them** — a default
configuration does not include them. Listing them means also re-listing
the defaults you want, per the warning above.

## Interval and the PRTG TTL

The PRTG format serves what is in the agent's cache and drops any value
older than **5 minutes**. That bound is fixed — it is not the
`cache.retention_minutes` setting, and changing that setting does not
move it.

An `interval` above 300 seconds therefore produces PRTG channels that
come and go: the sensor reads them just after a collection, then finds
nothing on the next scrape. BMCs are slow, so raising the interval is a
natural reflex — it is the wrong lever here. If you need to poll a BMC
less often than every 5 minutes, read it through Nagios or push over
OTLP, both of which serve the cache without that age limit.

This applies to the PRTG format only. Nagios and the OTLP push are not
affected.

## Multiple Servers

Monitor multiple hardware targets with separate probe instances:

```yaml
# probes.d/10-redfish.yaml
- name: "dell-storage-me5024"
  type: redfish
  params:
    endpoint: "https://dell-me5024.company.com"
    username: "admin"
    password: ${secret:dell-storage-me5024.password}   # OS secret store; inline plaintext is auto-sealed on install
    interval: 300
    verify_ssl: false

- name: "hpe-proliant-dl380"
  type: redfish
  params:
    endpoint: "https://ilo-dl380.company.com"
    username: "monitoring"
    password: ${secret:hpe-proliant-dl380.password}   # OS secret store; inline plaintext is auto-sealed on install
    interval: 300
    verify_ssl: false
```

# Configuration Parameters

<!-- schema:params:start -->
<!-- Generated from the probe's schema. Run `make docs-params` after changing it. -->
<!-- sha256:a265d48a598e505a23cc4610a41815b23a79a739e8e513aa03916bff9068cc7d -->

| Parameter | Must set | Default | Description |
|---|---|---|---|
| `endpoint` | Yes | - | BMC management address. Example: `https://idrac-server01.example.com` |
| `username` | Yes | - | BMC user with read access to the Redfish API |
| `password` | Yes | - | Password of the BMC user. A secret: reference it with `${secret:…}`, `${env:…}` or `${file:…}` rather than writing it in the file |
| `verify_ssl` | No | `true` | Validate the BMC's TLS certificate; false for the self-signed certificate most BMCs ship with |
| `interval` | No | `300` | Seconds between collections |
| `collections` | No | - | Subsystems to collect; replaces the default set (system, thermal, power, processor, memory, storage) rather than filtering it; unknown names are rejected. One of `system`, `thermal`, `power`, `processor`, `memory`, `storage`, `drives`, `network`, `networkadapter` |

<!-- schema:params:end -->

The cadence set by `interval` interacts with the PRTG sensor TTL: see [Interval and the PRTG TTL](#interval-and-the-prtg-ttl).

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

!!! note "Names keep the BMC's own text in OTLP, Prometheus and Zabbix"

    `hw.name` and the other name attributes carry the value exactly as the
    BMC reports it, for example `Lab drive 1 (failure predicted)`. The PRTG
    channel names and the URL filters are built from a cleaned copy with
    `, ; ( ) [ ] { } < > | \ " ' ` # & ? =` removed (`Lab drive 1 failure
    predicted`), which is what they always used; they are unchanged.

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

## Missing channels in PRTG or Nagios

**Symptom:** the sensor works and every channel reads OK, but there are
far fewer channels than you expect — no temperatures, no fans, no memory,
no drives.

Nothing is broken; the probe was told to collect less. Check, in order:

1. **`collections` in the probe configuration.** If the key is present,
   only the subsystems it lists are collected — the defaults are gone.
   This is the usual cause. See
   [Choosing what to collect](#choosing-what-to-collect-collections).
2. **The start-up log.** The agent logs which default subsystems the
   list turned off, and warns about a collection the BMC cannot serve:

   ```bash
   journalctl -u senhub-agent | grep -E "collections.*replaces|not supported by this vendor"
   ```

   An unrecognised value no longer gets this far: the probe refuses to
   start and the error names the accepted values.

3. **`interval` above 300 seconds**, if you read the probe through
   PRTG — channels then disappear between scrapes. See
   [Interval and the PRTG TTL](#interval-and-the-prtg-ttl).
4. **What the BMC actually exposes.** A limited or unlicensed BMC may not
   serve a subsystem at all; enable debug logging below to see which
   Redfish endpoints answered.

## Debug Logging

Enable debug logging for the Redfish probe:

```bash
# Runtime log level change
curl -X POST http://localhost:8080/api/{key}/debug/logs \
  -H "Content-Type: application/json" \
  -d '{"module_levels": [{"module": "probe.redfish", "level": "debug"}]}'

# Or start agent with verbose logging
senhub-agent run --filter probe.redfish
```

## License Requirements

The Redfish probe requires a **Pro** or **Enterprise** license.

| Tier | Redfish Probe |
|------|--------------|
| Free | Not available |
| Pro | Included |
| Enterprise | Included |

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
| `hw.status` | `hardware.power.health` | {psu_name} Health | # | Power supply unit health status |
| `hw.physical_disk.size` | `hardware.storage.drive.capacity.total` | {drive_name} Total Capacity | Bytes | Total drive capacity in bytes |
| `hw.status` | `hardware.storage.drive.health` | {drive_name} Health | # | Drive health status |
| `hw.status` | `hardware.storage.drive.failure_predicted` | {drive_name} Failure Predicted | # | Drive failure prediction status |
| `senhub.hardware.physical_disk.has_active_operations` | `hardware.storage.drive.has_operations` | {drive_name} Has Operations | # | Indicates if drive has active operations |
| `senhub.hardware.physical_disk.operation.progress_ratio` | `hardware.storage.drive.operation.progress` | {drive_name} Operation Progress | % | Drive operation progress percentage |
| `senhub.hardware.physical_disk.link_speed` | `hardware.storage.drive.speed_gbs` | {drive_name} Negotiated Speed | Gbps | Drive negotiated speed in Gbps |
| `senhub.hardware.physical_disk.location_indicator_active` | `hardware.storage.drive.location_indicator_active` | {drive_name} Location Indicator Active | # | Drive location indicator active status |
| `senhub.hardware.physical_disk.block_size` | `hardware.storage.drive.block_size_bytes` | {drive_name} Block Size | Bytes | Drive block size in bytes |
| `hw.logical_disk.limit` | `hardware.storage.volume.capacity.total` | Volume {volume_name} Total Capacity | Bytes | Total volume capacity in bytes |
| `hw.logical_disk.usage` | `hardware.storage.volume.capacity.allocated` | Volume {volume_name} Allocated | Bytes | Allocated volume capacity in bytes |
| `hw.logical_disk.usage` | `hardware.storage.volume.capacity.free` | Volume {volume_name} Free | Bytes | Free volume capacity in bytes |
| `hw.logical_disk.usage` | `hardware.storage.volume.capacity.used` | Volume {volume_name} Used | Bytes | Used (consumed) volume capacity in bytes |
| `hw.logical_disk.utilization` | `hardware.storage.volume.capacity.allocated_percent` | Volume {volume_name} Allocated Percent | % | Allocated volume capacity percentage |
| `hw.logical_disk.utilization` | `hardware.storage.volume.capacity.free_percent` | Volume {volume_name} Free Percent | % | Free volume capacity percentage |
| `hw.logical_disk.utilization` | `hardware.storage.volume.capacity.used_percent` | Volume {volume_name} Used Percent | % | Used (consumed) volume capacity percentage |
| `hw.status` | `hardware.storage.volume.health` | Volume {volume_name} Health | # | Volume health status |
| `senhub.hardware.logical_disk.encrypted` | `hardware.storage.volume.encrypted` | Volume {volume_name} Encrypted | # | Volume encryption status (0=no, 1=yes) |
| `senhub.hardware.logical_disk.io.operations` | `hardware.storage.volume.io.reads` | Volume {volume_name} IO Reads | # | Number of read operations on volume |
| `senhub.hardware.logical_disk.io.operations` | `hardware.storage.volume.io.writes` | Volume {volume_name} IO Writes | # | Number of write operations on volume |
| - | `hardware.storage.volume.io.total_ops` | Volume {volume_name} IO Total Ops | # | Total number of I/O operations on volume |
| `senhub.hardware.logical_disk.io` | `hardware.storage.volume.io.read.bytes` | Volume {volume_name} IO Read Bytes | Bytes | Total bytes read from volume |
| `senhub.hardware.logical_disk.io` | `hardware.storage.volume.io.write.bytes` | Volume {volume_name} IO Write Bytes | Bytes | Total bytes written to volume |
| - | `hardware.storage.volume.io.total_bytes` | Volume {volume_name} IO Total Bytes | Bytes | Total bytes transferred (read + write) on volume |
| `senhub.hardware.storage.pool.usage` | `hardware.storage.pool.capacity.allocated` | Pool {pool_name} Allocated | Bytes | Allocated pool capacity in bytes |
| `senhub.hardware.storage.pool.usage` | `hardware.storage.pool.capacity.total` | Pool {pool_name} Total | Bytes | Total pool capacity in bytes |
| `senhub.hardware.storage.pool.usage` | `hardware.storage.pool.capacity.used` | Pool {pool_name} Used | Bytes | Used pool capacity in bytes |
| `senhub.hardware.storage.pool.utilization` | `hardware.storage.pool.capacity.free_percent` | Pool {pool_name} Free Percent | % | Free pool capacity percentage |
| `senhub.hardware.storage.pool.utilization` | `hardware.storage.pool.capacity.allocated_percent` | Pool {pool_name} Allocated Percent | % | Allocated pool capacity percentage |
| `senhub.hardware.storage.pool.utilization` | `hardware.storage.pool.capacity.used_percent` | Pool {pool_name} Used Percent | % | Used pool capacity percentage |
| `senhub.hardware.storage.pool.status` | `hardware.storage.pool.health` | Pool {pool_name} Health | # | Pool health status |
| `senhub.hardware.storage.pool.io.operations` | `hardware.storage.pool.io.reads` | Pool {pool_name} IO Reads | # | Number of read operations on pool |
| `senhub.hardware.storage.pool.io.operations` | `hardware.storage.pool.io.writes` | Pool {pool_name} IO Writes | # | Number of write operations on pool |
| `senhub.hardware.storage.pool.io` | `hardware.storage.pool.io.read.bytes` | Pool {pool_name} IO Read Bytes | Bytes | Total bytes read from pool |
| `senhub.hardware.storage.pool.io` | `hardware.storage.pool.io.write.bytes` | Pool {pool_name} IO Write Bytes | Bytes | Total bytes written to pool |
| `hw.status` | `hardware.system.health` | System Health | # | Overall system health status |
| `senhub.hardware.system.power_state` | `hardware.system.power.state` | System Power State | # | System power state |
| `hw.status` | `hardware.controller.health` | Controller {controller_id} Health | # | Storage controller health status |
| `senhub.hardware.eventservice.status` | `hardware.eventservice.health` | Event Service Health | # | System event service health status |
| `senhub.hardware.redundancy.status` | `hardware.redundancy.health` | {host} Redundancy Health | # | Redundancy system health status |
| `senhub.hardware.redundancy.controllers.count` | `hardware.redundancy.controllers.active` | {host} Redundancy Controllers Active | # | Number of active redundant controllers |
| `senhub.hardware.redundancy.controllers.count` | `hardware.redundancy.controllers.min` | {host} Redundancy Controllers Min | # | Minimum number of controllers for redundancy |
| `senhub.hardware.redundancy.controllers.count` | `hardware.redundancy.controllers.max` | {host} Redundancy Controllers Max | # | Maximum number of controllers supported |
| `senhub.hardware.redundancy.status` | `hardware.storage.redundancy.health` | {redundancy_group} Health | # | Storage redundancy health status |
| `senhub.hardware.redundancy.controllers.count` | `hardware.storage.redundancy.controllers_active` | {redundancy_group} Controllers Active | # | Number of active redundant storage controllers |
| `senhub.hardware.redundancy.controllers.count` | `hardware.storage.redundancy.controllers_min` | {redundancy_group} Controllers Min | # | Minimum number of storage controllers for redundancy |
| `senhub.hardware.redundancy.controllers.count` | `hardware.storage.redundancy.controllers_max` | {redundancy_group} Controllers Max | # | Maximum number of storage controllers supported |
| `hw.status` | `hardware.cpu.health` | CPU Health | # | Processor health status |
| `senhub.hardware.cpu.cores` | `hardware.cpu.cores` | CPU Cores | # | Physical cores of the processor |
| `senhub.hardware.cpu.threads` | `hardware.cpu.threads` | CPU Threads | # | Hardware threads of the processor |
| `senhub.hardware.cpu.speed` | `hardware.cpu.max_speed` | CPU Max Speed | MHz | Maximum processor speed |
| `senhub.hardware.cpu.speed` | `hardware.cpu.current_speed` | CPU Current Speed | MHz | Current processor speed |
| `senhub.hardware.cpu.speed` | `hardware.cpu.average_frequency` | CPU Average Frequency | MHz | Average processor frequency |
| `hw.temperature` | `hardware.cpu.temperature` | CPU Temperature | °C | Processor temperature |
| `senhub.hardware.cpu.throttling_temperature` | `hardware.cpu.throttling_temperature` | CPU Throttling Temperature | °C | Temperature at which the processor throttles |
| `senhub.hardware.cpu.thermal_margin` | `hardware.cpu.thermal_margin` | CPU Thermal Margin | °C | Margin between the processor temperature and its throttling temperature |
| `hw.power` | `hardware.cpu.power_consumption` | CPU Power Consumption | W | Power drawn by the processor |
| `senhub.hardware.cpu.power_limit` | `hardware.cpu.power_limit` | CPU Power Limit | W | Power cap applied to the processor |
| `senhub.hardware.cpu.utilization` | `hardware.cpu.utilization` | CPU Utilization | % | Processor utilization reported by the BMC |
| `senhub.hardware.cpu.utilization` | `hardware.cpu.user_percent` | CPU User Percent | % | Processor time spent in user mode |
| `senhub.hardware.cpu.utilization` | `hardware.cpu.kernel_percent` | CPU Kernel Percent | % | Processor time spent in kernel mode |
| `senhub.hardware.cpu.utilization` | `hardware.cpu.io_wait_percent` | CPU IO Wait Percent | % | Processor time spent waiting for I/O |
| `senhub.hardware.cpu.utilization` | `hardware.cpu.utilization.dell` | CPU Utilization Dell | % | Processor utilization from the Dell OEM metrics |
| `senhub.hardware.cpu.utilization` | `hardware.cpu.utilization.hpe` | CPU Utilization Hpe | % | Processor utilization from the HPE OEM metrics |
| `senhub.hardware.cpu.cache.usage` | `hardware.cpu.cache.occupancy` | CPU Cache Occupancy | Bytes | Bytes held in the processor cache |
| `senhub.hardware.cpu.cache.hit_ratio` | `hardware.cpu.cache.hit_ratio` | CPU Cache Hit Ratio | ratio | Processor cache hit ratio |
| `hw.status` | `hardware.memory.health` | Memory Health | # | Memory module health status |
| `hw.memory.size` | `hardware.memory.capacity` | Memory Capacity | MiB | Memory module capacity |
| `senhub.hardware.memory.logical_size` | `hardware.memory.logical_size` | Memory Logical Size | MiB | Memory module logical size |
| `senhub.hardware.memory.cache_size` | `hardware.memory.cache_size` | Memory Cache Size | MiB | Cache size of the memory module |
| `senhub.hardware.memory.speed` | `hardware.memory.speed` | Memory Speed | MHz | Operating speed of the memory module |
| `senhub.hardware.memory.speed` | `hardware.memory.configured_speed` | Memory Configured Speed | MHz | Configured speed of the memory module |
| `senhub.hardware.memory.width` | `hardware.memory.bus_width` | Memory Bus Width | bits | Total bus width of the memory module |
| `senhub.hardware.memory.width` | `hardware.memory.data_width` | Memory Data Width | bits | Data width of the memory module |
| `senhub.hardware.memory.ranks` | `hardware.memory.rank_count` | Memory Rank Count | # | Ranks of the memory module |
| `senhub.hardware.memory.max_tdp` | `hardware.memory.max_tdp` | Memory Max Tdp | mW | Maximum thermal design power of the memory module |
| `hw.power` | `hardware.memory.power_consumption` | Memory Power Consumption | W | Power drawn by the memory module |
| `hw.temperature` | `hardware.memory.temperature` | Memory Temperature | °C | Memory module temperature |
| `senhub.hardware.memory.thermal_margin` | `hardware.memory.thermal_margin` | Memory Thermal Margin | °C | Margin between the memory module temperature and its limit |
| `senhub.hardware.memory.bandwidth.utilization` | `hardware.memory.bandwidth_utilization` | Memory Bandwidth Utilization | % | Memory bandwidth utilization |
| `senhub.hardware.memory.block_size` | `hardware.memory.block_size` | Memory Block Size | Bytes | Block size of the memory module |
| `senhub.hardware.memory.errors` | `hardware.memory.correctable_ecc_errors` | Memory Correctable Ecc Errors | # | Correctable ECC errors since the module was installed |
| `senhub.hardware.memory.errors` | `hardware.memory.uncorrectable_ecc_errors` | Memory Uncorrectable Ecc Errors | # | Uncorrectable ECC errors since the module was installed |
| `senhub.hardware.memory.throttled_cycles` | `hardware.memory.throttled_cycles` | Memory Throttled Cycles | # | Cycles the memory module spent throttled |
| `senhub.hardware.memory.alarm` | `hardware.memory.alarm.temperature` | Memory Alarm Temperature | # | Memory temperature alarm raised |
| `senhub.hardware.memory.alarm` | `hardware.memory.alarm.spares` | Memory Alarm Spares | # | Memory spares alarm raised |
| `senhub.hardware.memory.alarm` | `hardware.memory.alarm.correctable_ecc` | Memory Alarm Correctable Ecc | # | Correctable ECC alarm raised |
| `senhub.hardware.memory.alarm` | `hardware.memory.alarm.uncorrectable` | Memory Alarm Uncorrectable | # | Uncorrectable ECC alarm raised |
| `senhub.hardware.memory.period.blocks` | `hardware.memory.current_period.blocks_read` | Memory Current Period Blocks Read | # | Blocks read in the current period |
| `senhub.hardware.memory.period.blocks` | `hardware.memory.current_period.blocks_written` | Memory Current Period Blocks Written | # | Blocks written in the current period |
| `senhub.hardware.memory.blocks` | `hardware.memory.lifetime.blocks_read` | Memory Lifetime Blocks Read | # | Blocks read over the module lifetime |
| `senhub.hardware.memory.blocks` | `hardware.memory.lifetime.blocks_written` | Memory Lifetime Blocks Written | # | Blocks written over the module lifetime |
| `senhub.hardware.memory.spares` | `hardware.memory.dell.remaining_spares` | Memory Dell Remaining Spares | # | Dell OEM: spare memory still available |
| `senhub.hardware.memory.spares` | `hardware.memory.dell.used_spares` | Memory Dell Used Spares | # | Dell OEM: spare memory already used |
| `hw.voltage` | `hardware.memory.hpe.current_voltage` | Memory Hpe Current Voltage | mV | HPE OEM: current operating voltage of the memory module |
| `hw.voltage` | `hardware.memory.hpe.min_voltage` | Memory Hpe Min Voltage | mV | HPE OEM: min operating voltage of the memory module |
| `hw.voltage` | `hardware.memory.hpe.max_voltage` | Memory Hpe Max Voltage | mV | HPE OEM: max operating voltage of the memory module |
| `hw.status` | `hardware.network.health` | Network Health | # | Network adapter health status |
| `hw.network.up` | `hardware.network.link_up` | Network Link Up | # | Network adapter link state (1 up, 0 down) |
| `hw.network.bandwidth.limit` | `hardware.network.speed_mbps` | Network Speed Mbps | Mbps | Negotiated link speed of the network adapter |
| `hw.status` | `network.adapter.health` | Network Adapter Health | # | Network adapter health status |
| `hw.status` | `network.port.health` | Network Port Health | # | Network port health status |
| `hw.network.up` | `network.port.link_up` | Network Port Link Up | # | Network port link state (1 up, 0 down) |
| `hw.network.bandwidth.limit` | `network.port.speed_gbps` | Network Port Speed Gbps | Gbps | Negotiated link speed of the network port |
| `hw.power` | `hardware.power.usage` | Usage | W | Power delivered by the power supply |
| `senhub.hardware.power_supply.limit` | `hardware.power.limit` | Limit | W | Rated output of the power supply |
| `hw.voltage` | `hardware.power.input_voltage` | Input Voltage | Volts | Line input voltage of the power supply |
| `hw.power` | `hardware.power.consumption` | Consumption | W | Power drawn by the chassis |
| `senhub.hardware.enclosure.power.capacity` | `hardware.power.capacity` | Capacity | W | Power the chassis can draw |
| `hw.temperature` | `thermal.temperature` | Temperature | °C | Temperature sensor reading |
| `hw.fan.speed_ratio` | `thermal.fan_speed_percent` | Fan Speed Percent | % | Fan speed as a share of its maximum |
| `senhub.hardware.system.cpu.count` | `hardware.system.cpu.count` | System CPU Count | # | Processors installed in the system |
| `senhub.hardware.system.cpu.status` | `hardware.system.cpu.health` | System CPU Health | # | Health of the system's processors as a whole |
| `senhub.hardware.system.memory.size` | `hardware.system.memory.size` | System Memory Size | GiB | Total system memory |
| `senhub.hardware.system.memory.status` | `hardware.system.memory.health` | System Memory Health | # | Health of the system's memory as a whole |
| `senhub.hardware.firmware.info` | `system.idrac_firmware` | System Idrac Firmware | # | Presence marker (always 1) carrying the idrac firmware version as an attribute |
| `senhub.hardware.firmware.info` | `system.lifecycle_controller` | System Lifecycle Controller | # | Presence marker (always 1) carrying the lifecycle_controller firmware version as an attribute |
| `senhub.hardware.firmware.info` | `system.ilo_firmware` | System Ilo Firmware | # | Presence marker (always 1) carrying the ilo firmware version as an attribute |
| `senhub.hardware.firmware.info` | `system.cimc_firmware` | System Cimc Firmware | # | Presence marker (always 1) carrying the cimc firmware version as an attribute |
| `senhub.hardware.firmware.info` | `system.xcc_firmware` | System Xcc Firmware | # | Presence marker (always 1) carrying the xcc firmware version as an attribute |
| `senhub.hardware.eventservice.subscriptions` | `hardware.eventservice.subscriptions` | Eventservice Subscriptions | # | Event subscriptions registered on the BMC |
| `senhub.hardware.log.entries` | `hardware.logs.entries.total` | Logs Entries Total | # | Entries in the BMC event log |
| `senhub.hardware.log.entries` | `hardware.logs.entries.critical` | Logs Entries Critical | # | Critical entries in the BMC event log |
| `senhub.hardware.log.entries` | `hardware.logs.entries.warning` | Logs Entries Warning | # | Warning entries in the BMC event log |
| `senhub.hardware.log.entries` | `hardware.logs.entries.info` | Logs Entries Info | # | Informational entries in the BMC event log |
| `senhub.hardware.log.entries` | `hardware.logs.entries.last_24h` | Logs Entries Last 24h | # | Entries logged in the last 24 hours |
| `senhub.hardware.log.entries` | `hardware.logs.entries.last_7d` | Logs Entries Last 7d | # | Entries logged in the last 7 days |
| `senhub.hardware.storage.status` | `hardware.storage.health` | Health | # | Health of the storage subsystem |
| `hw.status` | `hardware.storage.controller.health` | Controller Health | # | Storage controller health status |
| `hw.status` | `storage.controller.health` | Controller Health | # | Storage controller health status |
| `senhub.hardware.disk_controller.link_speed` | `hardware.storage.controller.speed_gbps` | Controller Speed Gbps | Gbps | Link speed of the storage controller |
| `hw.status` | `storage.drive.health` | Drive Health | # | Drive health status |
| `hw.physical_disk.size` | `hardware.storage.drive.capacity_bytes` | Drive Capacity Bytes | Bytes | Total drive capacity |
| `hw.physical_disk.size` | `storage.drive.capacity_gb` | Drive Capacity Gb | GB | Total drive capacity |
| `senhub.hardware.physical_disk.hotspare` | `hardware.storage.drive.hotspare` | Drive Hotspare | # | Drive configured as a hot spare (1) or not (0) |
| `senhub.hardware.physical_disk.media_life_remaining` | `storage.drive.media_life_percent` | Drive Media Life Percent | % | Predicted media life left on the drive |
| `senhub.hardware.physical_disk.rotation_speed` | `storage.drive.rotation_rpm` | Drive Rotation Rpm | RPM | Rotation speed of the drive |
| `hw.status` | `storage.volume.health` | Volume Health | # | Volume health status |
| `hw.logical_disk.limit` | `storage.volume.capacity_gb` | Volume Capacity Gb | GB | Total volume capacity |
| `senhub.hardware.logical_disk.reserved` | `hardware.storage.volume.capacity.reserved` | Volume Capacity Reserved | Bytes | Capacity reserved on the volume |
| - | `hardware.storage.volume.io.read.latency` | Volume IO Read Latency | # | Volume read latency |
| - | `hardware.storage.volume.io.write.latency` | Volume IO Write Latency | # | Volume write latency |
| `senhub.hardware.storage.pool.usage` | `hardware.storage.pool.capacity.free` | Pool Capacity Free | Bytes | Free pool capacity |
| `senhub.hardware.storage.pool.usage` | `hardware.storage.pool.capacity.committed` | Pool Capacity Committed | Bytes | Capacity committed on the pool |
| `senhub.hardware.storage.pool.usage` | `hardware.storage.pool.capacity.overcommit` | Pool Capacity Overcommit | Bytes | Capacity over-committed on the pool |
| `senhub.hardware.storage.pool.usage` | `hardware.storage.pool.capacity.snapshots` | Pool Capacity Snapshots | Bytes | Pool capacity taken by snapshots |
| `senhub.hardware.storage.pool.usage` | `hardware.storage.pool.capacity.volumes` | Pool Capacity Volumes | Bytes | Pool capacity taken by volumes |

<!-- schema:metrics:end -->
