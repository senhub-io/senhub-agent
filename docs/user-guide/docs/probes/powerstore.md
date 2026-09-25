<img src="../../assets/probe-logos/powerstore.svg" alt="" class="probe-page-logo probe-page-logo-si">

!!! warning
    **License: Pro** - Requires a Pro or Enterprise license.

# Overview

The PowerStore probe monitors Dell PowerStore storage arrays through the PowerStore REST API, providing cluster health, hardware faults, capacity, performance, and active-alert metrics. One probe instance monitors one array (cluster); add more instances for additional arrays.

**Collected data:**

- Cluster reachability and configuration state
- Hardware component health (healthy vs faulted counts) and per-drive state
- Physical and logical capacity, data-reduction and efficiency ratios — array-wide and per-appliance
- Performance: IOPS, bandwidth, latency, average I/O size, CPU workload — array-wide, per-appliance and per-node
- Per-volume state and capacity; volume counts (total, not-ready)
- Replication sessions (count and per-session state)
- Active alerts by severity

All metrics are emitted under the `senhub.powerstore.*` namespace. Cluster-level
aggregates are complemented by **per-resource series** (per volume, appliance,
node, drive and replication session), each carrying a resource attribute that
also acts as a filter in the Sensor URLs tab of the console.

# Quick Start

## Basic Configuration

```yaml
# probes.d/20-powerstore.yaml — each file under probes.d/ is a YAML array of probes
- name: powerstore-prod
  type: powerstore
  params:
    endpoint: "https://powerstore.company.com"
    username: "monitoring"
    password: "${secret:powerstore-prod.password}"   # OS secret store; inline plaintext is auto-sealed on install
    interval: 300
    verify_ssl: true
```

`endpoint` may be given with or without a scheme; `https://` is assumed when none is provided. The `${secret:...}` reference resolves the password from the OS-native secret store (see [Configuration](../configuration.md)).

## Multiple Arrays

Monitor several arrays with separate probe instances:

```yaml
# probes.d/20-powerstore.yaml
- name: powerstore-dc1
  type: powerstore
  params:
    endpoint: "https://powerstore-dc1.company.com"
    username: "monitoring"
    password: "${secret:powerstore-dc1.password}"
    interval: 300

- name: powerstore-dc2
  type: powerstore
  params:
    endpoint: "https://powerstore-dc2.company.com"
    username: "monitoring"
    password: "${secret:powerstore-dc2.password}"
    interval: 300
    verify_ssl: false   # self-signed management certificate
```

# Configuration Parameters

<!-- schema:params:start -->
<!-- Generated from the probe's schema. Run `make docs-params` after changing it. -->

| Parameter | Must set | Default | Description |
|---|---|---|---|
| `endpoint` | Yes | - | Management API address; https:// is assumed when no scheme is given. Example: `https://powerstore.example.com` |
| `username` | Yes | - | PowerStore user with read access to the REST API |
| `password` | Yes | - | Password of the PowerStore user. A secret: reference it with `${secret:…}`, `${env:…}` or `${file:…}` rather than writing it in the file |
| `verify_ssl` | No | `true` | Validate the array's TLS certificate; false for a self-signed management certificate |
| `interval` | No | `300` | Seconds between collections |
| `volume_perf` | No | - | Per-volume IOPS, bandwidth and latency, off by default |
| `volume_perf.enabled` | No | `false` | Collect per-volume performance, one API call per volume |
| `volume_perf.top_n` | No | `20` | How many volumes are queried per run, busiest by logical usage first |
| `volume_perf.interval` | No | `300` | Seconds between per-volume runs, independent of the main interval |

<!-- schema:params:end -->

### Per-volume performance (opt-in)

Per-volume IOPS/bandwidth/latency is **off by default**: unlike the cluster,
appliance and node rollups (one request each), it costs **one `POST /metrics/generate`
per volume**, so on an array with hundreds or thousands of volumes an unconditional
per-cycle collection is a real request-cost and cache-cardinality concern.

When you enable it, the fan-out stays bounded on two axes:

- **`top_n`** caps how many volumes are queried each run — the busiest by logical
  usage, so the volumes that matter are covered without querying the long tail.
- **`volume_perf.interval`** throttles the fan-out to its own (typically longer)
  cadence, decoupled from the main probe `interval`.

With the defaults (`top_n: 20`, `interval: 300`) the added load is at most 20
requests every 5 minutes and 20 × 9 = 180 extra cache series — well within the
agent's series cap. Raise `top_n` deliberately after checking your array's volume
count.

```yaml
probes:
  - type: powerstore
    endpoint: "https://10.0.199.11"
    username: "supervision"
    password: "${secret:powerstore.password}"
    interval: 300
    volume_perf:
      enabled: true
      top_n: 25
      interval: 600
```

# Metrics Collected

All metrics carry a `cluster` attribute identifying the array. Metric families that split by direction or state (IOPS, bandwidth, latency, hardware) use an OTel attribute rather than separate metric names.

## Cluster

| Metric | Unit | Description |
|--------|------|-------------|
| `senhub.powerstore.up` | `1` | `1` when the management API answered this cycle, else `0` |
| `senhub.powerstore.cluster.state` | `1` | Cluster configuration state (Configured=2, Unconfigured=1, other=0) |

## Hardware

| Metric | Unit | Description |
|--------|------|-------------|
| `senhub.powerstore.hardware.components` | `{component}` | Hardware component count, split by `senhub.powerstore.hardware.state` (`healthy` / `faulted`) |

## Capacity

| Metric | Unit | Description |
|--------|------|-------------|
| `senhub.powerstore.capacity.physical` | `By` | Physical capacity (used / total, by attribute) |
| `senhub.powerstore.capacity.used_ratio` | `1` | Physical used ratio |
| `senhub.powerstore.capacity.logical` | `By` | Logical capacity (used / provisioned, by attribute) |
| `senhub.powerstore.data_reduction_ratio` | `1` | Data-reduction ratio |
| `senhub.powerstore.efficiency_ratio` | `1` | Overall efficiency ratio |

## Performance

| Metric | Unit | Description |
|--------|------|-------------|
| `senhub.powerstore.iops` | `{operation}/s` | I/O operations per second (read / write / total, by attribute) |
| `senhub.powerstore.bandwidth` | `By/s` | Throughput (read / write / total, by attribute) |
| `senhub.powerstore.latency` | `ms` | I/O latency in milliseconds (read / write / total, by attribute); exported as seconds over OTel |
| `senhub.powerstore.io_size` | `By` | Average I/O size |
| `senhub.powerstore.cpu.utilization` | `1` | CPU workload utilization — exported as a `0..1` ratio; the PRTG/Nagios pull views show it as a percentage |

!!! note "Ratios in the pull views"
    `capacity.used_ratio` and `cpu.utilization` are exported to OTLP/Prometheus as
    `0..1` ratios (OTel unit `1`). The PRTG and Nagios views display them as
    percentages (e.g. `42 %`, not `0.42`).

## Replication

| Metric | Unit | Description |
|--------|------|-------------|
| `senhub.powerstore.replication.sessions` | `{session}` | Number of replication sessions |

## Volumes

| Metric | Unit | Description |
|--------|------|-------------|
| `senhub.powerstore.volumes` | `{volume}` | Total number of volumes |
| `senhub.powerstore.volumes.not_ready` | `{volume}` | Volumes not in a ready state |

## Alerts

| Metric | Unit | Description |
|--------|------|-------------|
| `senhub.powerstore.alerts.active` | `{alert}` | Active alerts, split by `severity` |

## Per-resource series

In addition to the cluster-level aggregates above, the probe emits one series per
resource. Each carries a resource attribute (mapped from the `volume`,
`appliance`, `node`, `drive` or `session` tag) so instances stay distinct in
OTLP/Prometheus and become filterable in the Web UI.

| Metric | Unit | Resource attribute | Description |
|--------|------|--------------------|-------------|
| `senhub.powerstore.volume.state` | `1` | `volume.name` | Volume operational state (Ready=1, else 0) |
| `senhub.powerstore.volume.logical_used` | `By` | `volume.name` | Logical data written before data reduction |
| `senhub.powerstore.volume.size` | `By` | `volume.name` | Provisioned (thin) volume size |
| `senhub.powerstore.volume.iops` | `{operation}/s` | `volume.name` | Per-volume IOPS (read / write / total) — **opt-in**, see below |
| `senhub.powerstore.volume.bandwidth` | `By/s` | `volume.name` | Per-volume throughput — **opt-in** |
| `senhub.powerstore.volume.latency` | `ms` | `volume.name` | Per-volume latency (milliseconds) — **opt-in** |
| `senhub.powerstore.drive.state` | `1` | `drive.name` | Drive lifecycle state (Healthy=1, else 0) |
| `senhub.powerstore.appliance.state` | `1` | `appliance.name` | Appliance health (1 = no faulted component, else 0) |
| `senhub.powerstore.appliance.capacity.physical` | `By` | `appliance.name` | Physical capacity (used / total, by attribute) |
| `senhub.powerstore.appliance.capacity.logical` | `By` | `appliance.name` | Logical used capacity |
| `senhub.powerstore.appliance.iops` | `{operation}/s` | `appliance.name` | Appliance IOPS (read / write / total, by attribute) |
| `senhub.powerstore.appliance.bandwidth` | `By/s` | `appliance.name` | Appliance throughput |
| `senhub.powerstore.appliance.latency` | `ms` | `appliance.name` | Appliance latency (milliseconds) |
| `senhub.powerstore.appliance.cpu.utilization` | `1` | `appliance.name` | Appliance CPU workload (ratio; `%` in pull views) |
| `senhub.powerstore.node.cpu.utilization` | `1` | `node.name` | Node CPU workload (ratio; `%` in pull views) |
| `senhub.powerstore.node.iops` | `{operation}/s` | `node.name` | Node total IOPS |
| `senhub.powerstore.replication.state` | `1` | `replication.session_id` | Replication session state (OK=2, transitional=1, error=0) |

!!! note "Per-volume performance"
    Per-volume **capacity and state** are collected for every volume. Per-volume
    **performance** (IOPS/latency per volume) is not collected by default — it
    costs one REST request per volume per cycle, which does not scale on arrays
    with many volumes. Appliance- and node-level performance cover the array
    without that per-volume cost.

## Filtering (Sensor URLs tab of the console)

The Sensor URLs tab of the console exposes filters for this probe:

- **Metric Type** (category) — cluster, hardware, volumes, alerts, capacity, performance, replication
- **Alert Severity** — Critical / Major / Minor / Info
- **Volume**, **Appliance**, **Node**, **Drive**, **Replication Session** — pick a specific resource

`Cluster State`, `Array Reachable`, `Drive`/`Appliance`/`Volume State` and
`Replication State` render as text (e.g. `CONFIGURED`, `UP`, `Healthy`) via PRTG
value lookups rather than raw numbers.

# Requirements

- **PowerStore REST API** reachable from the agent host (HTTPS, default port 443).
- A **PowerStore user** with read access to the REST API (a monitoring/operator role is sufficient; no administrative rights are required).
- Network path from the agent to the array's management endpoint.

# Outputs

PowerStore metrics are available through every configured output — OTLP, Prometheus, and the pull formats (PRTG, Nagios, Web UI). For PRTG and Nagios, query the probe by its configured `name`:

```bash
curl "http://localhost:8080/api/{agentkey}/prtg/metrics/powerstore-prod"
curl "http://localhost:8080/api/{agentkey}/nagios/metrics/powerstore-prod"
```

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
| `senhub.powerstore.up` | `powerstore_up` | Array Reachable | # | 1 when the PowerStore management API answered this cycle, else 0 |
| `senhub.powerstore.cluster.state` | `powerstore_cluster_state` | Cluster State | # | Cluster configuration state (Configured=2, Unconfigured=1, other=0) |
| `senhub.powerstore.hardware.components` | `powerstore_hardware_healthy` | Hardware Healthy | # | Number of hardware components in the Healthy lifecycle state |
| `senhub.powerstore.hardware.components` | `powerstore_hardware_faulted` | Hardware Faulted | # | Number of hardware components in a fault lifecycle state (not Healthy/Empty/Initializing) |
| `senhub.powerstore.drive.state` | `powerstore_drive_state` | Drive {drive} State | # | Drive lifecycle state (Healthy=1, otherwise 0) |
| `senhub.powerstore.appliance.state` | `powerstore_appliance_state` | Appliance {appliance} State | # | Appliance health (1 = no faulted component, 0 = a faulted component) |
| `senhub.powerstore.capacity.physical` | `powerstore_capacity_physical_used` | Physical Used | Bytes | Physical space used on the array |
| `senhub.powerstore.capacity.physical` | `powerstore_capacity_physical_total` | Physical Total | Bytes | Total physical capacity of the array |
| `senhub.powerstore.capacity.used_ratio` | `powerstore_capacity_used_ratio` | Physical Used Ratio | % | Percentage of physical capacity used (0-100; exported as a 0..1 ratio) |
| `senhub.powerstore.capacity.logical` | `powerstore_capacity_logical_used` | Logical Used | Bytes | Logical (thin-provisioned) space used before data reduction |
| `senhub.powerstore.data_reduction_ratio` | `powerstore_data_reduction_ratio` | Data Reduction Ratio | # | Data reduction ratio (logical:physical), e.g. 1.49 = 1.49:1 |
| `senhub.powerstore.efficiency_ratio` | `powerstore_efficiency_ratio` | Overall Efficiency Ratio | # | Overall storage efficiency ratio (thin + dedup + compression + snapshots) |
| `senhub.powerstore.capacity.logical` | `powerstore_capacity_logical_provisioned` | Logical Provisioned | Bytes | Logical capacity provisioned to hosts (thin) |
| `senhub.powerstore.appliance.capacity.physical` | `powerstore_appliance_physical_used` | {appliance} Physical Used | Bytes | Physical space used on the appliance |
| `senhub.powerstore.appliance.capacity.physical` | `powerstore_appliance_physical_total` | {appliance} Physical Total | Bytes | Total physical capacity of the appliance |
| `senhub.powerstore.appliance.capacity.logical` | `powerstore_appliance_logical_used` | {appliance} Logical Used | Bytes | Logical (thin) space used on the appliance before data reduction |
| `senhub.powerstore.iops` | `powerstore_iops_read` | Read IOPS | # | Average read IOPS over the interval |
| `senhub.powerstore.iops` | `powerstore_iops_write` | Write IOPS | # | Average write IOPS over the interval |
| `senhub.powerstore.iops` | `powerstore_iops_total` | Total IOPS | # | Average total IOPS over the interval |
| `senhub.powerstore.bandwidth` | `powerstore_bandwidth_read` | Read Bandwidth | Bytes/s | Average read bandwidth over the interval |
| `senhub.powerstore.bandwidth` | `powerstore_bandwidth_write` | Write Bandwidth | Bytes/s | Average write bandwidth over the interval |
| `senhub.powerstore.bandwidth` | `powerstore_bandwidth_total` | Total Bandwidth | Bytes/s | Average total bandwidth over the interval |
| `senhub.powerstore.latency` | `powerstore_latency_read` | Read Latency | ms | Average read latency over the interval (milliseconds) |
| `senhub.powerstore.latency` | `powerstore_latency_write` | Write Latency | ms | Average write latency over the interval (milliseconds) |
| `senhub.powerstore.latency` | `powerstore_latency_total` | Latency | ms | Average overall latency over the interval (milliseconds) |
| `senhub.powerstore.io_size` | `powerstore_io_size` | Average IO Size | Bytes | Average IO size over the interval |
| `senhub.powerstore.cpu.utilization` | `powerstore_cpu_utilization` | CPU Workload Utilization | % | Array IO-workload CPU utilization, appliance-level (percent; exported as a 0..1 ratio) |
| `senhub.powerstore.appliance.iops` | `powerstore_appliance_iops_read` | {appliance} Read IOPS | # | Average read IOPS on the appliance over the interval |
| `senhub.powerstore.appliance.iops` | `powerstore_appliance_iops_write` | {appliance} Write IOPS | # | Average write IOPS on the appliance over the interval |
| `senhub.powerstore.appliance.iops` | `powerstore_appliance_iops_total` | {appliance} Total IOPS | # | Average total IOPS on the appliance over the interval |
| `senhub.powerstore.appliance.bandwidth` | `powerstore_appliance_bandwidth_total` | {appliance} Total Bandwidth | Bytes/s | Average total bandwidth on the appliance over the interval |
| `senhub.powerstore.appliance.latency` | `powerstore_appliance_latency_total` | {appliance} Latency | ms | Average overall latency on the appliance over the interval (milliseconds) |
| `senhub.powerstore.appliance.cpu.utilization` | `powerstore_appliance_cpu_utilization` | {appliance} CPU Utilization | % | Appliance IO-workload CPU utilization (percent; exported as a 0..1 ratio) |
| `senhub.powerstore.node.cpu.utilization` | `powerstore_node_cpu_utilization` | Node {node} CPU Utilization | % | Node IO-workload CPU utilization (percent; exported as a 0..1 ratio) |
| `senhub.powerstore.node.iops` | `powerstore_node_iops_total` | Node {node} Total IOPS | # | Average total IOPS on the node over the interval |
| `senhub.powerstore.replication.sessions` | `powerstore_replication_sessions` | Replication Sessions | # | Number of replication sessions (0 when replication is not configured) |
| `senhub.powerstore.replication.state` | `powerstore_replication_state` | Replication {session} State | # | Replication session state (OK=2, transitional=1, error=0) |
| `senhub.powerstore.volumes` | `powerstore_volumes_total` | Volumes Total | # | Total number of provisioned volumes |
| `senhub.powerstore.volumes.not_ready` | `powerstore_volumes_not_ready` | Volumes Not Ready | # | Number of volumes not in the Ready state |
| `senhub.powerstore.volume.state` | `powerstore_volume_state` | {volume} State | # | Volume operational state (Ready=1, otherwise 0) |
| `senhub.powerstore.volume.logical_used` | `powerstore_volume_logical_used` | {volume} Logical Used | Bytes | Logical data written to the volume before data reduction |
| `senhub.powerstore.volume.size` | `powerstore_volume_size` | {volume} Provisioned Size | Bytes | Provisioned (thin) size of the volume |
| `senhub.powerstore.volume.iops` | `powerstore_volume_iops_read` | {volume} Read IOPS | # | Average read IOPS for the volume over the interval |
| `senhub.powerstore.volume.iops` | `powerstore_volume_iops_write` | {volume} Write IOPS | # | Average write IOPS for the volume over the interval |
| `senhub.powerstore.volume.iops` | `powerstore_volume_iops_total` | {volume} Total IOPS | # | Average total IOPS for the volume over the interval |
| `senhub.powerstore.volume.bandwidth` | `powerstore_volume_bandwidth_read` | {volume} Read Bandwidth | Bytes/s | Average read bandwidth for the volume over the interval |
| `senhub.powerstore.volume.bandwidth` | `powerstore_volume_bandwidth_write` | {volume} Write Bandwidth | Bytes/s | Average write bandwidth for the volume over the interval |
| `senhub.powerstore.volume.bandwidth` | `powerstore_volume_bandwidth_total` | {volume} Total Bandwidth | Bytes/s | Average total bandwidth for the volume over the interval |
| `senhub.powerstore.volume.latency` | `powerstore_volume_latency_read` | {volume} Read Latency | ms | Average read latency for the volume over the interval (milliseconds) |
| `senhub.powerstore.volume.latency` | `powerstore_volume_latency_write` | {volume} Write Latency | ms | Average write latency for the volume over the interval (milliseconds) |
| `senhub.powerstore.volume.latency` | `powerstore_volume_latency_total` | {volume} Latency | ms | Average overall latency for the volume over the interval (milliseconds) |
| `senhub.powerstore.alerts.active` | `powerstore_alerts_active` | Active Alerts ({severity}) | # | Number of active (uncleared) alerts by severity |

<!-- schema:metrics:end -->
