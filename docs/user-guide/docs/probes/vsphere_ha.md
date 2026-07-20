<img src="https://cdn.simpleicons.org/vmware" alt="" class="probe-page-logo probe-page-logo-si">

!!! warning
    **License: Pro** - Requires a Pro or Enterprise license.

# Overview

The vSphere HA probe monitors VMware vSphere high-availability infrastructure from a single vCenter: **vSAN cluster health** (via the vSAN health API) and, optionally, **NSX-T overlay status** (via the NSX manager REST API). One probe instance monitors one vCenter; add more instances for additional vCenters.

**Collected data:**

- vCenter reachability (probe liveness)
- Per-cluster vSAN overall health, disk-group count, healthy/degraded object counts, and pending resync bytes
- NSX manager connectivity
- NSX transport-node counts (total and up), logical-switch count
- Per-edge-cluster NSX health

All metrics are emitted under the `senhub.vsphere_ha.*` namespace. vSAN metrics carry a `cluster` attribute (one series per cluster); NSX edge-cluster health carries an `edge_cluster_id` attribute (one series per edge cluster). Both act as filters in the Web UI Sensor Builder. NSX-T monitoring is only performed when `nsx_endpoint` and `nsx_username` are configured; a vCenter-only setup collects vSAN metrics alone.

# Quick Start

## Basic Configuration (vSAN only)

```yaml
# probes.d/40-vsphere-ha.yaml — each file under probes.d/ is a YAML array of probes
- name: vsphere-ha-prod
  type: vsphere_ha
  params:
    vcenter_url: "https://vcenter.company.com"
    username: "monitoring@vsphere.local"
    password: "${secret:vsphere-ha-prod.password}"   # OS secret store; inline plaintext is auto-sealed on install
    interval: 300
    insecure_skip_verify: false
```

`vcenter_url` may be given with or without a scheme; `https://` is assumed when none is provided. The `${secret:...}` reference resolves the password from the OS-native secret store (see [Configuration](../configuration.md)).

## With NSX-T Monitoring

Add the NSX manager endpoint and credentials to also collect NSX overlay health:

```yaml
# probes.d/40-vsphere-ha.yaml
- name: vsphere-ha-prod
  type: vsphere_ha
  params:
    vcenter_url: "https://vcenter.company.com"
    username: "monitoring@vsphere.local"
    password: "${secret:vsphere-ha-prod.password}"
    interval: 300
    insecure_skip_verify: true   # self-signed vCenter certificate
    nsx_endpoint: "https://nsx-manager.company.com"
    nsx_username: "monitoring"
    nsx_password: "${secret:vsphere-ha-prod.nsx_password}"
```

When `nsx_endpoint` or `nsx_username` is omitted, the probe collects vSAN metrics only. A vSAN collection failure marks the probe down (`up = 0`); an NSX failure is non-fatal and leaves the vSAN metrics intact.

# Configuration Parameters

| Parameter | Type | Required | Default | Description |
|-----------|------|----------|---------|-------------|
| `vcenter_url` | string | Yes | - | vCenter management address (`https://` assumed if no scheme) |
| `username` | string | Yes | - | vCenter user with read access (vSAN health view) |
| `password` | string | Yes | - | vCenter user password — reference a stored secret via `${secret:<name>.password}`, `${env:VAR}` or `${file:/path}`. Inline plaintext is auto-sealed into the OS secret store on install. |
| `insecure_skip_verify` | boolean | No | `false` | Skip TLS certificate validation for vCenter and NSX (set `true` for self-signed certificates) |
| `nsx_endpoint` | string | No | - | NSX manager base URL. Enables NSX-T collection when set together with `nsx_username` |
| `nsx_username` | string | No | - | NSX manager user with read access to the REST API |
| `nsx_password` | string | No | - | NSX manager user password — reference a stored secret via `${secret:<name>.nsx_password}` |
| `interval` | integer | No | `300` | Collection interval in seconds |
| `timeout` | integer | No | `30` | Per-request deadline (seconds) for vCenter and NSX calls |

# Metrics Collected

vSAN metrics carry a `cluster` attribute identifying the vSAN cluster; the objects metric splits healthy vs degraded via an OTel attribute rather than separate metric names.

## Overview

| Metric | Unit | Description |
|--------|------|-------------|
| `senhub.vsphere_ha.up` | `1` | `1` when the vCenter session was live and vSAN answered this cycle, else `0` |

## vSAN

| Metric | Unit | Description |
|--------|------|-------------|
| `senhub.vsphere_ha.vsan.health` | `1` | Overall cluster health (green=2, yellow=1, red/other=0) |
| `senhub.vsphere_ha.vsan.disk_groups` | `{group}` | Host physical-disk health summaries (disk-group proxy) |
| `senhub.vsphere_ha.vsan.objects` | `{object}` | vSAN object count, split by `senhub.vsphere_ha.vsan.object.state` (`healthy` / `degraded`) |
| `senhub.vsphere_ha.vsan.resync` | `By` | Total bytes pending vSAN resync on the cluster |

## NSX-T

| Metric | Unit | Description |
|--------|------|-------------|
| `senhub.vsphere_ha.nsx.manager.health` | `1` | NSX manager connectivity (1 = CONNECTED, else 0) |
| `senhub.vsphere_ha.nsx.transport_nodes.total` | `{node}` | Total number of transport nodes |
| `senhub.vsphere_ha.nsx.transport_nodes.up` | `{node}` | Transport nodes deployed (NODE_READY) and not in maintenance |
| `senhub.vsphere_ha.nsx.logical_switches` | `{switch}` | Number of logical switches (segments) |
| `senhub.vsphere_ha.nsx.edge_cluster.health` | `1` | Per-edge-cluster health (1 = all members UP, else 0), attribute `nsx.edge_cluster.id` |

## Filtering (Web UI Sensor Builder)

The PRTG/Web UI Sensor Builder exposes filters for this probe:

- **Metric Type** (category) — overview, vSAN, NSX-T
- **vSAN Cluster** — pick a specific cluster
- **NSX Edge Cluster** — pick a specific edge cluster

# Requirements

- **vCenter** reachable from the agent host (HTTPS, default port 443) with vSAN enabled on the monitored clusters.
- A **vCenter user** with read access to the vSAN cluster health view (a read-only monitoring role is sufficient; no administrative rights are required).
- For NSX-T monitoring: an **NSX manager** reachable over HTTPS and an NSX user with read access to the manager REST API (`/api/v1/cluster/status`, `/transport-nodes/status`, `/logical-switches`, `/edge-clusters`).

# Outputs

vSphere HA metrics are available through every configured output — OTLP, Prometheus, and the pull formats (PRTG, Nagios, Web UI). For PRTG and Nagios, query the probe by its configured `name`:

```bash
curl "http://localhost:8080/api/{agentkey}/prtg/metrics/vsphere-ha-prod"
curl "http://localhost:8080/api/{agentkey}/nagios/metrics/vsphere-ha-prod"
```
