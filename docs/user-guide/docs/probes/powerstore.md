<img src="https://api.iconify.design/mdi/nas.svg?color=%23666" alt="" class="probe-page-logo probe-page-logo-mdi">

!!! warning
    **License: Pro** - Requires a Pro or Enterprise license.

# Overview

The PowerStore probe monitors Dell PowerStore storage arrays through the PowerStore REST API, providing cluster health, hardware faults, capacity, performance, and active-alert metrics. One probe instance monitors one array (cluster); add more instances for additional arrays.

**Collected data:**

- Cluster reachability and configuration state
- Hardware component health (healthy vs faulted counts)
- Physical and logical capacity, data-reduction and efficiency ratios
- Performance: IOPS, bandwidth, latency, average I/O size, CPU workload
- Replication sessions
- Volume counts (total, not-ready)
- Active alerts by severity

All metrics are emitted under the `senhub.powerstore.*` namespace.

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

| Parameter | Type | Required | Default | Description |
|-----------|------|----------|---------|-------------|
| `endpoint` | string | Yes | - | PowerStore management API address (`https://` assumed if no scheme) |
| `username` | string | Yes | - | PowerStore user with read access to the REST API |
| `password` | string | Yes | - | User password — reference a stored secret via `${secret:<name>.password}`, `${env:VAR}` or `${file:/path}`. Inline plaintext is auto-sealed into the OS secret store on install. |
| `interval` | integer | No | `300` | Collection interval in seconds |
| `verify_ssl` | boolean | No | `true` | Validate the array's TLS certificate (set `false` for self-signed management certificates) |

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
| `senhub.powerstore.latency` | `s` | I/O latency (read / write / total, by attribute) |
| `senhub.powerstore.io_size` | `By` | Average I/O size |
| `senhub.powerstore.cpu.utilization` | `1` | CPU workload utilization ratio |

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

# Requirements

- **PowerStore REST API** reachable from the agent host (HTTPS, default port 443).
- A **PowerStore user** with read access to the REST API (a monitoring/operator role is sufficient; no administrative rights are required).
- Network path from the agent to the array's management endpoint.

# Outputs

PowerStore metrics are available through every configured output — OTLP, Prometheus, and the pull formats (PRTG, Nagios, Web UI). For PRTG and Nagios, query the probe by name:

```bash
curl http://localhost:8080/api/{agentkey}/prtg/metrics -d '{"probe": "powerstore-prod"}'
curl "http://localhost:8080/api/{agentkey}/nagios/metrics?probe=powerstore-prod"
```
