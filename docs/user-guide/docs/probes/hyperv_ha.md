<img src="https://cdn.simpleicons.org/microsoft" alt="" class="probe-page-logo probe-page-logo-si">

!!! warning
    **License: Pro** - Requires a Pro or Enterprise license.

# Overview

The Hyper-V HA probe monitors **Hyper-V Replica** and **Windows Failover Cluster** health on a Windows host. It reads two WMI namespaces locally — `root\virtualization\v2` for replica relationships and `root\MSCluster` for cluster node and resource-group state — so no credentials or network endpoint are configured. One probe instance monitors the host it runs on.

**Collected data:**

- Probe reachability (the replica WMI namespace answered this cycle)
- Per-VM Hyper-V Replica health, raw replication state, and replication lag
- Failover Cluster node state (per node)
- Failover Cluster resource-group state (per group)

All metrics are emitted under the `senhub.hyperv_ha.*` namespace. Per-VM,
per-node and per-group series each carry a resource attribute (`vm.name`,
`cluster.node`, `cluster.group`) that also acts as a filter in the Web UI Sensor
Builder.

The Failover Clustering feature is **optional**: when it is not installed the
`root\MSCluster` query fails and the probe simply emits no cluster metrics for
that cycle, without erroring out. Replica metrics are unaffected.

# Quick Start

## Basic Configuration

```yaml
# probes.d/40-hyperv-ha.yaml — each file under probes.d/ is a YAML array of probes
- name: hyperv-ha-local
  type: hyperv_ha
  params:
    interval: 60
```

The probe queries WMI on the local host under the agent's service account. There
is no endpoint, username or password to configure — the agent must run on the
Hyper-V host itself.

## Longer Interval

Replica and cluster state change slowly; a longer interval keeps WMI load low:

```yaml
# probes.d/40-hyperv-ha.yaml
- name: hyperv-ha-local
  type: hyperv_ha
  params:
    interval: 300
    timeout: 30
```

# Configuration Parameters

| Parameter | Type | Required | Default | Description |
|-----------|------|----------|---------|-------------|
| `interval` | integer | No | `60` | Collection interval in seconds |
| `timeout` | integer | No | `30` | WMI query timeout in seconds |

# Metrics Collected

Every datapoint carries a `metric_type` tag. Per-resource series additionally
carry the VM, node or group identifier as an OTel attribute so instances stay
distinct in OTLP/Prometheus.

## Overview

| Metric | Unit | Description |
|--------|------|-------------|
| `senhub.hyperv_ha.up` | `1` | `1` when the replica WMI namespace answered this cycle, else `0` |

## Replica

| Metric | Unit | Resource attribute | Description |
|--------|------|--------------------|-------------|
| `senhub.hyperv_ha.replica.health` | `1` | `vm.name` | Replication health (1 = Normal, 0 = Warning/Critical) |
| `senhub.hyperv_ha.replica.state` | `1` | `vm.name` | Raw replication state numeric value |
| `senhub.hyperv_ha.replica.lag` | `s` | `vm.name` | Seconds since the last successful replication |

## Cluster

| Metric | Unit | Resource attribute | Description |
|--------|------|--------------------|-------------|
| `senhub.hyperv_ha.cluster.node.state` | `1` | `cluster.node` | Cluster node state (1 = Up, 0 = Down/Paused/Joining) |
| `senhub.hyperv_ha.cluster.group.state` | `1` | `cluster.group` | Cluster resource-group state (1 = Online, 0 = Offline/Failed/Partial) |

## Filtering (Web UI Sensor Builder)

The PRTG/Web UI Sensor Builder exposes filters for this probe:

- **Replicated VM** — pick a specific replicated virtual machine
- **Cluster Node** — pick a specific Failover Cluster node
- **Cluster Group** — pick a specific Failover Cluster resource group

# Requirements

- The agent must run **on the Hyper-V host** (Windows). WMI is queried locally;
  the probe is refused on non-Windows platforms.
- The **Hyper-V role** must be installed for replica metrics
  (`root\virtualization\v2`).
- The **Failover Clustering** feature is optional — cluster metrics appear only
  when it is installed (`root\MSCluster`); otherwise they are silently skipped.
- The agent service account needs **WMI read access** to those namespaces.

# Outputs

Hyper-V HA metrics are available through every configured output — OTLP, Prometheus, and the pull formats (PRTG, Nagios, Web UI). For PRTG and Nagios, query the probe by its configured `name`:

```bash
curl "http://localhost:8080/api/{agentkey}/prtg/metrics/hyperv-ha-local"
curl "http://localhost:8080/api/{agentkey}/nagios/metrics/hyperv-ha-local"
```
