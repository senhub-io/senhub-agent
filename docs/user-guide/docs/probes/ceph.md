<img src="../../assets/probe-logos/ceph.svg" alt="" class="probe-page-logo probe-page-logo-si">

!!! info
    **License: Free** — part of the universal collection tier.

# Ceph

The `ceph` probe monitors a Ceph cluster via the Ceph REST Management API v1,
reporting cluster health, OSD counts, monitor quorum, capacity and per-pool
I/O statistics.

## Quick start

```yaml
# probes.d/10-ceph.yaml — each file under probes.d/ is a YAML array of probes
- name: ceph
  type: ceph
  params:
    endpoint: https://localhost:8443
    username: admin
    password: ${secret:ceph.password}   # OS secret store; inline plaintext is auto-sealed on install
```

## Parameters

<!-- schema:params:start -->
<!-- Generated from the probe's schema. Run `make docs-params` after changing it. -->
<!-- sha256:200d4e82b2ccb5ecc405d20f4987a7c53cf5b0b99e0afd5de2564dbb3ae0de8e -->

| Parameter | Must set | Default | Description |
|---|---|---|---|
| `endpoint` | In practice | `https://localhost:8443` | Base URL of the Manager dashboard / REST API |
| `username` | Yes | - | Dashboard user |
| `password` | Yes | - | Dashboard user's password. A secret: reference it with `${secret:…}`, `${env:…}` or `${file:…}` rather than writing it in the file |
| `verify_tls` | No | `true` | Verify the dashboard certificate; false accepts a self-signed one |
| `interval` | No | `60` | Seconds between collections |
| `instance_name` | No | - | Stable identity override for this cluster |

<!-- schema:params:end -->

## Metrics

| Metric | Unit | Description |
|---|---|---|
| `senhub.ceph.up` | 1 | 1 when the Ceph REST API is reachable and authentication succeeds |
| `ceph.health.status` | 1 | Cluster health: 2 = HEALTH_OK, 1 = HEALTH_WARN, 0 = HEALTH_ERR |
| `ceph.cluster.capacity` | By | Raw cluster storage capacity |
| `ceph.cluster.used` | By | Raw storage space in use |
| `ceph.osd.total` | {osd} | Total number of OSDs in the cluster |
| `ceph.osd.up` | {osd} | OSDs currently in the `up` state |
| `ceph.osd.in` | {osd} | OSDs currently in the `in` state (participating in data placement) |
| `ceph.monitor.quorum_count` | {monitor} | Number of monitors participating in the quorum |
| `ceph.pool.rd_ops` | {read} | Read operations per pool (tagged with `pool`) |
| `ceph.pool.wr_ops` | {write} | Write operations per pool |

## Operational notes

- The Ceph Manager Dashboard must be enabled: `ceph mgr module enable dashboard`.
- The dashboard ships with a self-signed TLS certificate, which the probe rejects by default. Set `verify_tls: false` to accept it in a lab; for production, configure a proper certificate.
- The API requires Ceph Nautilus (14+) or newer for the `/api/` v1 interface.

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
| `senhub.ceph.up` | `senhub.ceph.up` | Ceph {instance} Reachability | # | 1 when the Ceph REST API is reachable and authentication succeeds |
| `ceph.health.status` | `ceph.health.status` | Ceph {instance} Health Status | # | Cluster health: 2=HEALTH_OK, 1=HEALTH_WARN, 0=HEALTH_ERR |
| `ceph.cluster.capacity` | `ceph.cluster.capacity` | Ceph {instance} Raw Capacity | B | Total raw cluster capacity in bytes |
| `ceph.cluster.used` | `ceph.cluster.used` | Ceph {instance} Used Capacity | B | Total bytes currently used across the cluster |
| `ceph.osd.total` | `ceph.osd.total` | Ceph {instance} OSD Total | # | Total number of OSDs configured in the cluster |
| `ceph.osd.in` | `ceph.osd.in` | Ceph {instance} OSD In | # | Number of OSDs that are in (participating in the cluster) |
| `ceph.osd.up` | `ceph.osd.up` | Ceph {instance} OSD Up | # | Number of OSDs that are up (running) |
| `ceph.monitor.count` | `ceph.monitor.count` | Ceph {instance} Monitor Count | # | Total number of monitor daemons |
| `ceph.monitor.quorum_count` | `ceph.monitor.quorum_count` | Ceph {instance} Monitor Quorum | # | Number of monitors currently in quorum |
| `ceph.pool.objects` | `ceph.pool.objects` | Ceph {instance} Pool {pool} Objects | # | Number of objects stored in the pool |
| `ceph.pool.used` | `ceph.pool.used` | Ceph {instance} Pool {pool} Used | B | Bytes stored in the pool |
| `ceph.pool.rd_ops` | `ceph.pool.rd_ops` | Ceph {instance} Pool {pool} Read Ops | # | Cumulative read operations on the pool |
| `ceph.pool.wr_ops` | `ceph.pool.wr_ops` | Ceph {instance} Pool {pool} Write Ops | # | Cumulative write operations on the pool |

<!-- schema:metrics:end -->
